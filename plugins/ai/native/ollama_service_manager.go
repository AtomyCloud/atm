//go:build linux

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const ollamaServiceName = "atm-ollama.service"
const ollamaServiceProperties = "Id,Names,LoadState,ActiveState,SubState,Job,FragmentPath,SourcePath,DropInPaths,NeedDaemonReload,UnitFileState,Type,Restart,ExecStart,WorkingDirectory,EnvironmentFiles,Description"

// Test-only injection, never selected by an environment variable or CLI flag.
type ollamaServiceTools struct {
	uid     func() int
	resolve func(string) (string, error)
	run     func(context.Context, string, []string, []string) (string, error)
	bus     func(int) error
}

func (b *backend) serviceTools() *ollamaServiceTools {
	if b.ollamaService != nil {
		return b.ollamaService
	}
	return &ollamaServiceTools{os.Geteuid, ollamaServiceCommand, ollamaServiceRun, ollamaServiceBus}
}

func ollamaServiceCommand(name string) (string, error) {
	if name != "systemctl" && name != "env" {
		return "", fmt.Errorf("unsupported service tool")
	}
	for _, dir := range []string{"/usr/bin", "/bin"} {
		p := filepath.Join(dir, name)
		if _, err := os.Lstat(p); os.IsNotExist(err) {
			continue
		}
		p, err := filepath.EvalSymlinks(p)
		if err != nil {
			return "", err
		}
		if err = trustedRootFile(p); err != nil {
			return "", err
		}
		i, err := os.Lstat(p)
		if err != nil || i.Mode().Perm()&0111 == 0 {
			return "", fmt.Errorf("nonexecutable service tool")
		}
		return p, nil
	}
	return "", fmt.Errorf("trusted %s unavailable", name)
}

func ollamaServiceBus(uid int) error {
	if uid <= 0 {
		return fmt.Errorf("Ollama user service refuses root")
	}
	runtime := "/run/user/" + strconv.Itoa(uid)
	if err := dirs(runtime, false, true); err != nil {
		return err
	}
	i, err := os.Lstat(filepath.Join(runtime, "bus"))
	if err != nil || i.Mode()&os.ModeSocket == 0 || owner(i) != uint32(uid) {
		return fmt.Errorf("fixed owned user bus socket unavailable")
	}
	return nil
}

func ollamaServiceEnvironment(home string, uid int) []string {
	runtime := "/run/user/" + strconv.Itoa(uid)
	return []string{"PATH=/usr/bin:/bin", "HOME=" + home, "LANG=C", "LC_ALL=C", "XDG_CONFIG_HOME=" + filepath.Join(home, ".config"), "XDG_RUNTIME_DIR=" + runtime, "DBUS_SESSION_BUS_ADDRESS=unix:path=" + runtime + "/bus", "SYSTEMD_PAGER=cat", "SYSTEMD_COLORS=0"}
}

func ollamaServiceRun(ctx context.Context, path string, args, env []string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env, cmd.Dir, cmd.WaitDelay = env, "/", time.Second
	var out, stderr systemOutput
	cmd.Stdout, cmd.Stderr = &out, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		return "", fmt.Errorf("user manager command: %w; %s", err, stderr.String())
	}
	if out.overflow || stderr.overflow {
		return "", fmt.Errorf("user manager output exceeded bound")
	}
	return out.String(), nil
}

func (b *backend) serviceCommand(ctx context.Context, home, action string) (string, error) {
	t := b.serviceTools()
	if t.uid() <= 0 {
		return "", fmt.Errorf("Ollama user service refuses root")
	}
	if err := t.bus(t.uid()); err != nil {
		return "", err
	}
	p, err := t.resolve("systemctl")
	if err != nil {
		return "", err
	}
	args := []string{"--user", "--no-pager", "--no-ask-password"}
	switch action {
	case "show":
		args = append(args, "show", "--all", "--property="+ollamaServiceProperties, ollamaServiceName)
	case "paths":
		args = append(args, "show", "--property=UnitPath")
	case "daemon-reload":
		args = append(args, action)
	case "start", "stop":
		args = append(args, action, ollamaServiceName)
	default:
		return "", fmt.Errorf("unsupported manager action")
	}
	return t.run(ctx, p, args, ollamaServiceEnvironment(home, t.uid()))
}

type ollamaManagerState map[string]string

func parseOllamaManager(output string) (ollamaManagerState, error) {
	want := strings.Split(ollamaServiceProperties, ",")
	m := ollamaManagerState{}
	for _, line := range strings.Split(strings.TrimSuffix(output, "\n"), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("malformed manager state")
		}
		if _, exists := m[k]; exists {
			return nil, fmt.Errorf("duplicate manager state")
		}
		known := false
		for _, key := range want {
			known = known || key == k
		}
		if !known {
			return nil, fmt.Errorf("unknown manager field")
		}
		m[k] = v
	}
	// systemctl's empty structure-array printers omit these two properties even
	// with --all. All identity/state fields remain mandatory. owned() still
	// requires the exact nonempty ExecStart for a registered unit.
	for _, key := range []string{"EnvironmentFiles", "ExecStart"} {
		if _, ok := m[key]; !ok {
			m[key] = ""
		}
	}
	if len(m) != len(want) {
		return nil, fmt.Errorf("missing manager state")
	}
	if m["Id"] != ollamaServiceName || m["Names"] != ollamaServiceName || m["DropInPaths"] != "" || m["SourcePath"] != "" || m["Job"] != "" || m["NeedDaemonReload"] != "no" || m["EnvironmentFiles"] != "" {
		return nil, fmt.Errorf("foreign, pending, stale or overridden user unit")
	}
	switch m["ActiveState"] + "/" + m["SubState"] {
	case "inactive/dead", "active/running", "failed/failed":
	default:
		return nil, fmt.Errorf("unknown or transitioning manager state")
	}
	return m, nil
}

func (b *backend) serviceManager(ctx context.Context, home string) (ollamaManagerState, error) {
	s, err := b.serviceCommand(ctx, home, "show")
	if err != nil {
		return nil, err
	}
	return parseOllamaManager(s)
}

func (m ollamaManagerState) absent() error {
	if m["LoadState"] != "not-found" || m["FragmentPath"] != "" || m["ActiveState"] != "inactive" || m["SubState"] != "dead" || m["UnitFileState"] != "" || m["ExecStart"] != "" {
		return fmt.Errorf("user unit is not demonstrably absent")
	}
	return nil
}

func (m ollamaManagerState) owned(r ollamaServiceReceipt) error {
	if m["LoadState"] != "loaded" || m["FragmentPath"] != r.UnitPath || m["UnitFileState"] != "static" || m["Type"] != "exec" || m["Restart"] != "no" || m["WorkingDirectory"] != r.DataHome || m["Description"] != r.Description {
		return fmt.Errorf("manager unit identity differs from owned unit")
	}
	// systemctl formats ExecStart as a structure, followed by runtime-only fields.
	// Refuse unfamiliar representations rather than guessing at manager identity.
	prefix := "{ path=" + r.Env + " ; argv[]=" + strings.Join(r.Argv, " ") + " ; ignore_errors=no ; "
	if !strings.HasPrefix(m["ExecStart"], prefix) || !strings.HasSuffix(m["ExecStart"], " }") || strings.Contains(strings.TrimPrefix(m["ExecStart"], prefix), " ; argv[]=") {
		return fmt.Errorf("manager ExecStart differs or is unsupported")
	}
	return nil
}

// Quotes systemd words, not shell words. ':' disables ExecStart $ expansion;
// %% is still required for the independent specifier-expansion pass.
func ollamaSystemdWord(s string) string {
	s = strings.ReplaceAll(s, "%", "%%")
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return "\"" + s + "\""
}

func ollamaServiceUnit(r ollamaServiceReceipt) []byte {
	if absolutePath(r.DataHome) != nil {
		return nil
	}
	words := make([]string, len(r.Argv))
	for i, s := range r.Argv {
		for _, c := range s {
			if c < 32 || c == 127 {
				return nil
			}
		}
		words[i] = ollamaSystemdWord(s)
	}
	// WorkingDirectory takes a raw path, unlike ExecStart's word parser.
	return []byte("[Unit]\nDescription=" + r.Description + "\n\n[Service]\nType=exec\nRestart=no\nUMask=0077\nWorkingDirectory=" + strings.ReplaceAll(r.DataHome, "%", "%%") + "\nUnsetEnvironment=LD_PRELOAD LD_LIBRARY_PATH LD_AUDIT\nExecStart=:" + strings.Join(words, " ") + "\nTimeoutStartSec=15s\nTimeoutStopSec=15s\n")
}

// systemctl's string-array printer uses shell_maybe_quote(value, 0). Compare
// against that encoding without interpreting it as a shell command.
func ollamaManagerWord(s string) string {
	if !strings.ContainsAny(s, " \t\r\n\"\\`$*?[\u0027()<>|&;!") {
		return s
	}
	s = strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "`", "\\`", "$", "\\$").Replace(s)
	return "\"" + s + "\""
}
