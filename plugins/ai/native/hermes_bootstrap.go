//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/AtomyCloud/atm/internal/install"
)

const hermesAgent = "hermes-agent"

// Only the inspected PM contract is enabled. A newer release needs a contract
// review, not optimistic execution of a changed upstream installation protocol.
const hermesPMVersion = "0.22.5"
const hermesPMCommit = "bf0203a794a07aac3ff764972ab9087baa4e4f4e"

type hermesTools struct {
	run func(context.Context, string, []string, string, []string) (string, error)
	uid func() int
}

type hermesPin struct {
	URL, SHA256 string
}

type hermesBootstrapPins struct {
	UV                                                     hermesPin
	UVVersion, PythonVersion, PythonBuild, UVEntry, Target string
}

func readHermesPins(source, arch string) (hermesBootstrapPins, error) {
	var result hermesBootstrapPins
	f, err := regular(filepath.Join(source, "pm", "lock.json"), 0644, 256<<10)
	if err != nil {
		return result, err
	}
	data, err := io.ReadAll(io.LimitReader(f, (256<<10)+1))
	f.Close()
	if err != nil || len(data) > 256<<10 {
		return result, fmt.Errorf("cannot read bounded Hermes PM lock")
	}
	var lock struct{ Packages map[string]json.RawMessage }
	if err := decode(data, &lock, false); err != nil {
		return result, err
	}
	target, machine := "linux-x64", "x86_64"
	if arch == "arm64" {
		target, machine = "linux-arm64", "aarch64"
	} else if arch != "amd64" {
		return result, fmt.Errorf("unsupported Hermes architecture")
	}
	var uv struct {
		Version   string
		Artifacts map[string]hermesPin
	}
	var python struct{ Version string }
	if err := decode(lock.Packages["uv"], &uv, false); err != nil {
		return result, err
	}
	if err := decode(lock.Packages["python"], &python, false); err != nil {
		return result, err
	}
	if !versionRE.MatchString(uv.Version) || !regexp.MustCompile(`^3\.14\.[0-9]+\+[0-9]{8}$`).MatchString(python.Version) {
		return result, fmt.Errorf("unsupported Hermes bootstrap versions")
	}
	entry := "uv-" + machine + "-unknown-linux-gnu"
	pin := uv.Artifacts[target]
	if !validSHA(pin.SHA256) || pin.URL != "https://github.com/astral-sh/uv/releases/download/"+uv.Version+"/"+entry+".tar.gz" {
		return result, fmt.Errorf("unsupported Hermes uv origin or digest")
	}
	parts := strings.Split(python.Version, "+")
	return hermesBootstrapPins{pin, uv.Version, parts[0], parts[1], entry + "/uv", target}, nil
}

func (b *backend) hermesBootstrapUV(ctx context.Context, versionDir string, pins hermesBootstrapPins) (string, error) {
	m := install.Manager{Root: filepath.Join(versionDir, "bootstrap"), PluginID: "ai", Client: b.client,
		Limits: install.Limits{DownloadBytes: 128 << 20, ExpandedBytes: 512 << 20, FileBytes: 256 << 20, Entries: 1000}}
	r, err := m.Install(ctx, install.Spec{Version: pins.UVVersion, URL: pins.UV.URL, SHA256: pins.UV.SHA256, Format: "tar.gz",
		Origins: []string{"https://github.com", "https://release-assets.githubusercontent.com", "https://objects.githubusercontent.com", "https://github-releases.githubusercontent.com"}})
	if err != nil {
		return "", err
	}
	path := filepath.Join(r.Path, pins.UVEntry)
	if err := hermesOwnedExecutable(versionDir, path); err != nil {
		return "", err
	}
	// PM owns/replaces this discovery slot. Preserve a separate verified bootstrap
	// executable because PM may replace its own uv while preparing its runtime.
	slot := filepath.Join(versionDir, "home", ".hermes", "tools", "uv-"+pins.UVVersion+"-"+pins.Target)
	if err := dirs(slot, true, true); err != nil {
		return "", err
	}
	f, err := regular(path, 0700, 256<<20)
	if err != nil {
		return "", err
	}
	defer f.Close()
	out, err := os.OpenFile(filepath.Join(slot, "uv"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0700)
	if err != nil {
		return "", err
	}
	_, copyErr := io.Copy(out, contextReader{ctx, f})
	if copyErr == nil {
		copyErr = out.Sync()
	}
	closeErr := out.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	return path, nil
}

func (b *backend) hermesVerifyBootstrap(ctx context.Context, versionDir string, pins hermesBootstrapPins, pinned hermesCompletionFile) error {
	m := install.Manager{Root: filepath.Join(versionDir, "bootstrap"), PluginID: "ai", Client: b.client}
	r, err := m.Active(ctx)
	if err != nil {
		return err
	}
	if r.Version != pins.UVVersion {
		return fmt.Errorf("bootstrap uv selection changed")
	}
	var rec struct {
		Schema   int                     `json:"schema"`
		PluginID string                  `json:"plugin_id"`
		Artifact install.Spec            `json:"artifact"`
		Files    map[string]install.File `json:"files"`
	}
	if err := readJSON(filepath.Join(versionDir, "bootstrap", "versions", pins.UVVersion, "receipt.json"), &rec); err != nil {
		return err
	}
	if rec.Artifact.URL != pins.UV.URL || rec.Artifact.SHA256 != pins.UV.SHA256 {
		return fmt.Errorf("bootstrap uv provenance changed")
	}
	path := filepath.Join(r.Path, pins.UVEntry)
	_, now, err := hermesCompletionRead(ctx, versionDir, path, 256<<20, true, false)
	if err != nil {
		return err
	}
	if now != pinned {
		return fmt.Errorf("verified bootstrap uv changed during preparation")
	}
	if err := hermesOwnedExecutable(versionDir, path); err != nil {
		return err
	}
	f, err := regular(path, 0700, 256<<20)
	if err != nil {
		return err
	}
	defer f.Close()
	slot := filepath.Join(versionDir, "home", ".hermes", "tools", "uv-"+pins.UVVersion+"-"+pins.Target, "uv")
	g, err := regular(slot, 0700, 256<<20)
	if err != nil {
		return err
	}
	defer g.Close()
	// Exact comparison avoids relying on a second mutable local receipt for the
	// independent PM discovery slot.
	left, right := make([]byte, 64<<10), make([]byte, 64<<10)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, e := io.ReadFull(f, left)
		m, se := io.ReadFull(g, right)
		if n != m || !bytes.Equal(left[:n], right[:m]) {
			return fmt.Errorf("PM bootstrap uv copy changed")
		}
		if e == io.EOF || e == io.ErrUnexpectedEOF {
			if se != e {
				return fmt.Errorf("PM bootstrap uv size changed")
			}
			return nil
		}
		if e != nil {
			return e
		}
		if se != nil {
			return se
		}
	}
}

func hermesOwnedExecutable(root, path string) error {
	if err := absolutePath(path); err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(resolved, root+string(os.PathSeparator)) {
		return fmt.Errorf("Hermes executable escapes its private installation")
	}
	if err := dirs(filepath.Dir(resolved), false, false); err != nil {
		return err
	}
	i, err := os.Lstat(resolved)
	if err != nil {
		return err
	}
	if !i.Mode().IsRegular() || owner(i) != uint32(os.Geteuid()) || !singleLink(i) || i.Mode().Perm()&0111 == 0 || i.Mode().Perm()&0022 != 0 || i.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return fmt.Errorf("unsafe Hermes managed executable")
	}
	return nil
}

func hermesEnvironment(versionDir string) []string {
	home := filepath.Join(versionDir, "home")
	return []string{
		"HOME=" + home, "PATH=" + filepath.Join(versionDir, "host-bin"), "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "TERM=dumb", "NO_COLOR=1", "CI=1",
		"HERMES_HOME=" + filepath.Join(home, ".hermes"), "XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"XDG_CACHE_HOME=" + filepath.Join(home, ".cache"), "XDG_DATA_HOME=" + filepath.Join(home, ".local", "share"),
		"UV_PYTHON_INSTALL_DIR=" + filepath.Join(versionDir, "python"), "UV_CACHE_DIR=" + filepath.Join(home, ".cache", "uv"),
		"UV_NO_CONFIG=1", "UV_NO_PROGRESS=1", "UV_PYTHON_PREFERENCE=only-managed", "UV_NO_MODIFY_PATH=1",
		"UV_PYTHON_CPYTHON_BUILD=20261021", "HERMES_DISABLE_LAZY_INSTALLS=1",
		"PYTHONNOUSERSITE=1", "PYTHONDONTWRITEBYTECODE=1", "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0",
		"PIP_CONFIG_FILE=/dev/null", "PIP_NO_INPUT=1", "TMPDIR=" + filepath.Join(versionDir, "tmp"),
	}
}

func (b *backend) hermesRun(ctx context.Context, executable string, args []string, dir string, env []string) (string, error) {
	if b.hermes != nil && b.hermes.run != nil {
		return b.hermes.run(ctx, executable, args, dir, env)
	}
	guard, err := hermesHostCommand("setpriv")
	if err != nil {
		return "", err
	}
	return runHermesCommand(ctx, guard, append([]string{"--no-new-privs", "--", executable}, args...), dir, env)
}

func (b *backend) hermesPreparePython(ctx context.Context, versionDir, uv string, pins hermesBootstrapPins, pinned hermesCompletionFile) (string, error) {
	source := filepath.Join(versionDir, "source")
	env := hermesEnvironment(versionDir)
	if pins.PythonBuild != "20261021" {
		return "", fmt.Errorf("bootstrap Python build not reviewed")
	}
	if _, err := b.hermesRun(ctx, uv, []string{"python", "install", "--no-config", "--no-bin", "--no-registry", pins.PythonVersion}, source, env); err != nil {
		return "", err
	}
	if err := b.hermesVerifyBootstrap(ctx, versionDir, pins, pinned); err != nil {
		return "", err
	}
	output, err := b.hermesRun(ctx, uv, []string{"python", "find", "--no-config", "--managed-python", "--system", "--no-project", pins.PythonVersion}, source, env)
	if err != nil {
		return "", err
	}
	python := strings.TrimSpace(output)
	if strings.ContainsAny(python, "\r\n") {
		return "", fmt.Errorf("ambiguous bootstrap Python path")
	}
	if err := hermesOwnedExecutable(filepath.Join(versionDir, "python"), python); err != nil {
		return "", err
	}
	return python, nil
}

func hermesHostCommand(name string) (string, error) {
	for _, dir := range []string{"/usr/bin", "/bin"} {
		p, err := filepath.EvalSymlinks(filepath.Join(dir, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if err := trustedRootFile(p); err != nil {
			return "", err
		}
		i, err := os.Stat(p)
		if err != nil {
			return "", err
		}
		if i.Mode().Perm()&0111 == 0 || i.Mode()&(os.ModeSetuid|os.ModeSetgid) != 0 {
			return "", fmt.Errorf("unsafe host build tool")
		}
		return p, nil
	}
	return "", fmt.Errorf("required host tool unavailable: %s; install separately", name)
}

func (b *backend) hermesPrivilegePolicy() error {
	uid := os.Geteuid()
	if b.hermes != nil && b.hermes.uid != nil {
		uid = b.hermes.uid()
	}
	if uid == 0 {
		return fmt.Errorf("Hermes dependency preparation refuses root; use an unprivileged account")
	}
	_, err := hermesHostCommand("setpriv")
	return err
}

func hermesHostTools(versionDir string) error {
	root := filepath.Join(versionDir, "host-bin")
	if err := os.Mkdir(root, 0700); err != nil {
		return err
	}
	// No sudo, su, package managers, login tools or caller-controlled PATH entries.
	for _, name := range []string{"sh", "bash", "env", "git", "uname", "cc", "gcc", "g++", "make", "ar", "as", "ld", "ldd", "tar", "unzip", "install", "cp", "mkdir", "rm", "ln", "dirname", "basename", "chmod", "nproc", "sed", "grep", "awk", "tr", "head", "cut", "cat", "sort", "tee", "touch", "xargs", "find", "which", "readlink", "pwd", "id"} {
		p, err := hermesHostCommand(name)
		if err != nil {
			continue
		}
		if err := os.Symlink(p, filepath.Join(root, name)); err != nil {
			return err
		}
	}
	return nil
}
