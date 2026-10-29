//go:build linux

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

type ollamaServiceIdentity struct{ Device, Inode uint64 }
type ollamaServiceOwner struct {
	Schema                       int
	Plugin, Provider, Home, Root string
	Directories                  map[string]ollamaServiceIdentity
}
type ollamaServiceReceipt struct {
	Schema                                         int
	Home, Root, Version, BundleSHA256, OwnerSHA256 string
	UnitPath, DataHome, Models, Env, Description   string
	Argv                                           []string
}
type ollamaServiceState struct {
	Owner   *ollamaServiceOwner
	Receipt *ollamaServiceReceipt
	Files   map[string]pathState
}

func (b *backend) ollamaServiceRoot() string {
	return filepath.Join(filepath.Dir(b.root("ollama")), "service-v1")
}
func ollamaServiceAnchor(home string) string {
	return filepath.Join(home, ".atm-ai-ollama-service.json")
}
func ollamaServiceUnitPath(home string) string {
	return filepath.Join(home, ".config", "systemd", "user", ollamaServiceName)
}
func ollamaServiceHash(v any) string {
	data, _ := json.Marshal(v)
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func ollamaServiceHome() (string, error) {
	home, err := integrationHome()
	if err != nil {
		return "", err
	}
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" && x != filepath.Join(home, ".config") {
		return "", fmt.Errorf("custom XDG_CONFIG_HOME unsupported; default ~/.config/systemd/user required")
	}
	if x := os.Getenv("XDG_DATA_HOME"); x != "" && x != filepath.Join(home, ".local", "share") {
		return "", fmt.Errorf("custom XDG_DATA_HOME unsupported")
	}
	if os.Getenv("SYSTEMD_UNIT_PATH") != "" {
		return "", fmt.Errorf("custom SYSTEMD_UNIT_PATH unsupported")
	}
	return home, nil
}

func ollamaDirectoryIdentity(path string) (ollamaServiceIdentity, error) {
	if err := dirs(path, false, true); err != nil {
		return ollamaServiceIdentity{}, err
	}
	i, err := os.Lstat(path)
	if err != nil {
		return ollamaServiceIdentity{}, err
	}
	s, ok := i.Sys().(*syscall.Stat_t)
	if !ok {
		return ollamaServiceIdentity{}, fmt.Errorf("directory identity unavailable")
	}
	return ollamaServiceIdentity{uint64(s.Dev), s.Ino}, nil
}

func (b *backend) serviceOwner(home string) (ollamaServiceOwner, error) {
	r := ollamaServiceOwner{Schema: 1, Plugin: "ai", Provider: "ollama", Home: home, Root: b.ollamaServiceRoot(), Directories: map[string]ollamaServiceIdentity{}}
	for _, rel := range []string{".", "data", "data/home", "data/models"} {
		i, err := ollamaDirectoryIdentity(filepath.Join(r.Root, rel))
		if err != nil {
			return r, err
		}
		r.Directories[rel] = i
	}
	return r, nil
}

func (b *backend) serviceReceipt(home, version, digest, env string, own ollamaServiceOwner) ollamaServiceReceipt {
	r := ollamaServiceReceipt{Schema: 1, Home: home, Root: b.ollamaServiceRoot(), Version: version, BundleSHA256: digest, OwnerSHA256: ollamaServiceHash(own), UnitPath: ollamaServiceUnitPath(home), DataHome: filepath.Join(b.ollamaServiceRoot(), "data/home"), Models: filepath.Join(b.ollamaServiceRoot(), "data/models"), Env: env}
	r.Argv = []string{env, "-i", "HOME=" + r.DataHome, "PATH=/usr/bin:/bin", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "OLLAMA_HOST=127.0.0.1:11434", "OLLAMA_MODELS=" + r.Models, filepath.Join(b.ollamaVersion(version), "tree/bin/ollama"), "serve"}
	r.Description = "ATM portable Ollama " + ollamaServiceHash(r)
	return r
}

// Metadata remains separate from native-v1. Retained data is reusable only with
// the original immutable directory identities, never by adopting a directory.
func (b *backend) inspectOllamaService(home string) (*ollamaServiceState, error) {
	s := &ollamaServiceState{Files: map[string]pathState{}}
	root := b.ollamaServiceRoot()
	if err := dirs(root, false, true); os.IsNotExist(err) {
		return s, nil
	} else if err != nil {
		return nil, err
	}
	es, err := entries(root, 4)
	if err != nil {
		return nil, err
	}
	for _, e := range es {
		switch e.Name() {
		case "owner.json", "receipt.json", "data":
		case "journal.json":
			return nil, fmt.Errorf("Ollama service interrupted; retained journal requires manual inspection")
		default:
			return nil, fmt.Errorf("unknown service entry preserved")
		}
	}
	var own ollamaServiceOwner
	if err = readJSON(filepath.Join(root, "owner.json"), &own); err != nil {
		return nil, err
	}
	expected, err := b.serviceOwner(home)
	if err != nil || !reflect.DeepEqual(own, expected) {
		return nil, fmt.Errorf("service data/root ownership changed: %v", err)
	}
	dataEntries, err := entries(filepath.Join(root, "data"), 2)
	if err != nil || len(dataEntries) != 2 {
		return nil, fmt.Errorf("unexpected service data layout")
	}
	for _, e := range dataEntries {
		if e.Name() != "home" && e.Name() != "models" {
			return nil, fmt.Errorf("unknown service data entry")
		}
	}
	s.Owner = &own
	for _, name := range []string{"owner.json", "receipt.json"} {
		p := filepath.Join(root, name)
		state, err := pathSnapshot(p, false)
		if err != nil {
			return nil, err
		}
		s.Files[p] = state
	}
	if !s.Files[filepath.Join(root, "receipt.json")].Exists {
		return s, nil
	}
	var r ollamaServiceReceipt
	if err = readJSON(filepath.Join(root, "receipt.json"), &r); err != nil {
		return nil, err
	}
	if !versionRE.MatchString(r.Version) || !validSHA(r.BundleSHA256) || (r.Env != "/usr/bin/env" && r.Env != "/bin/env") {
		return nil, fmt.Errorf("invalid service receipt")
	}
	want := b.serviceReceipt(home, r.Version, r.BundleSHA256, r.Env, own)
	if !reflect.DeepEqual(r, want) {
		return nil, fmt.Errorf("foreign or changed service receipt")
	}
	s.Receipt = &r
	for _, p := range []string{r.UnitPath, ollamaServiceAnchor(home)} {
		state, err := pathSnapshot(p, false)
		if err != nil || !state.Exists || state.Mode != 0600 {
			return nil, fmt.Errorf("service ownership evidence unavailable: %s: %v", p, err)
		}
		s.Files[p] = state
	}
	if string(s.Files[r.UnitPath].Data) != string(ollamaServiceUnit(r)) {
		return nil, fmt.Errorf("owned service unit bytes changed")
	}
	var anchor ollamaServiceReceipt
	if err = readJSON(ollamaServiceAnchor(home), &anchor); err != nil || !reflect.DeepEqual(anchor, r) {
		return nil, fmt.Errorf("HOME service registration changed: %v", err)
	}
	return s, nil
}

func (b *backend) servicePayload(ctx context.Context, r ollamaServiceReceipt) error {
	_, v, err := b.ollamaInspect(ctx)
	if err != nil {
		return err
	}
	if v != r.Version {
		return fmt.Errorf("service pinned selection changed")
	}
	rec, err := b.ollamaVerify(ctx, v, b.ollamaVersion(v))
	if err != nil {
		return err
	}
	if ollamaServiceHash(rec) != r.BundleSHA256 {
		return fmt.Errorf("service pinned bundle receipt changed")
	}
	return ctx.Err()
}

func ollamaServiceUnchanged(s *ollamaServiceState) error {
	for p, old := range s.Files {
		now, err := pathSnapshot(p, false)
		if err != nil || !pathUnchanged(now, old) {
			return fmt.Errorf("service evidence changed: %s: %v", p, err)
		}
	}
	return nil
}

// Check all manager-reported lookup roots, including inherited type/dash
// drop-ins and enablement/alias symlinks. Never delete anything found here.
func (b *backend) servicePaths(ctx context.Context, home string, registered bool) error {
	out, err := b.serviceCommand(ctx, home, "paths")
	if err != nil {
		return err
	}
	runtime := "/run/user/" + strconv.Itoa(b.serviceTools().uid())
	allowed := []string{filepath.Join(home, ".config/systemd/user"), filepath.Join(home, ".config/systemd/user.control"), filepath.Join(home, ".local/share/systemd/user"), runtime + "/systemd/user", runtime + "/systemd/user.control", runtime + "/systemd/transient", runtime + "/systemd/generator", runtime + "/systemd/generator.early", runtime + "/systemd/generator.late", "/etc/systemd/user", "/etc/systemd/user.control", "/etc/xdg/systemd/user", "/run/systemd/user", "/usr/local/lib/systemd/user", "/usr/local/share/systemd/user", "/usr/lib/systemd/user", "/usr/share/systemd/user", "/lib/systemd/user"}
	sort.Slice(allowed, func(i, j int) bool { return len(allowed[i]) > len(allowed[j]) })
	remaining := strings.TrimSuffix(out, "\n")
	if !strings.HasPrefix(remaining, "UnitPath=") {
		return fmt.Errorf("missing manager UnitPath")
	}
	remaining = strings.TrimPrefix(remaining, "UnitPath=")
	seen := map[string]bool{}
	for remaining != "" {
		found := ""
		for _, p := range allowed {
			encoded := ollamaManagerWord(p)
			if remaining == encoded || strings.HasPrefix(remaining, encoded+" ") {
				found = p
				break
			}
		}
		if found == "" || seen[found] {
			return fmt.Errorf("custom or malformed manager UnitPath unsupported")
		}
		seen[found] = true
		remaining = strings.TrimPrefix(strings.TrimPrefix(remaining, ollamaManagerWord(found)), " ")
	}
	if !seen[filepath.Dir(ollamaServiceUnitPath(home))] {
		return fmt.Errorf("manager does not load default user config root")
	}
	for p := range seen {
		if _, err = os.Lstat(p); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return err
		}
		actual := p
		if !strings.HasPrefix(p, home+"/") && !strings.HasPrefix(p, runtime+"/") {
			actual, err = filepath.EvalSymlinks(p)
			if err != nil {
				return err
			}
		}
		if err = dirs(actual, false, false); err != nil {
			return err
		}
		for _, drop := range []string{ollamaServiceName + ".d", "atm-.service.d", "service.d"} {
			if _, e := os.Lstat(filepath.Join(actual, drop)); !os.IsNotExist(e) {
				return fmt.Errorf("service drop-in path exists or is uncertain: %s", filepath.Join(p, drop))
			}
		}
		f, e := os.Open(actual)
		if e != nil {
			return e
		}
		es, e := f.ReadDir(4097)
		f.Close()
		if (e != nil && e != io.EOF) || len(es) > 4096 {
			return fmt.Errorf("cannot inspect bounded manager lookup directory")
		}
		for _, entry := range es {
			candidate := filepath.Join(actual, entry.Name())
			if entry.Name() == ollamaServiceName {
				if !registered || p != filepath.Dir(ollamaServiceUnitPath(home)) || entry.Type()&os.ModeSymlink != 0 {
					return fmt.Errorf("existing foreign service unit preserved")
				}
			}
			if entry.Type()&os.ModeSymlink != 0 {
				target, e := os.Readlink(candidate)
				if e != nil {
					return e
				}
				if filepath.Base(target) == ollamaServiceName {
					return fmt.Errorf("external service alias preserved")
				}
			}
			if entry.IsDir() && (strings.HasSuffix(entry.Name(), ".wants") || strings.HasSuffix(entry.Name(), ".requires")) {
				if _, e := os.Lstat(filepath.Join(candidate, ollamaServiceName)); !os.IsNotExist(e) {
					return fmt.Errorf("external service enablement preserved")
				}
				f, e := os.Open(candidate)
				if e != nil {
					return e
				}
				links, e := f.ReadDir(4097)
				f.Close()
				if (e != nil && e != io.EOF) || len(links) > 4096 {
					return fmt.Errorf("cannot inspect bounded dependency directory")
				}
				for _, link := range links {
					if link.Type()&os.ModeSymlink != 0 {
						target, e := os.Readlink(filepath.Join(candidate, link.Name()))
						if e != nil {
							return e
						}
						if filepath.Base(target) == ollamaServiceName {
							return fmt.Errorf("external dependency alias preserved")
						}
					}
				}
			}
		}
	}
	return ctx.Err()
}
