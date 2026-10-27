package catalog

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/AtomyCloud/atm/internal/data"
	"github.com/AtomyCloud/atm/internal/plugin"
	"mvdan.cc/sh/v3/syntax"
)

const DownloadTimeout = 10 * time.Minute
const maxDownloadFile = 32 << 20
const maxDownloadTotal = 128 << 20

type DownloadInfo struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Commit string `json:"commit"`
	Source string `json:"source"`
	Files  int    `json:"files"`
	Bytes  int64  `json:"bytes"`
}

// Private contents cannot be constructed from user-supplied JSON or a revision flag.
type DownloadPlan struct {
	info     DownloadInfo
	files    map[string]treeEntry
	manifest plugin.Manifest
}

func (p *DownloadPlan) Info() DownloadInfo { return p.info }

type DownloadResult struct {
	DownloadInfo
	Directory       string   `json:"directory"`
	RestartRequired bool     `json:"restart_required"`
	Warnings        []string `json:"warnings,omitempty"`
}

func portablePath(name string) bool {
	if !safePath(name) || len(name) > 1024 || strings.ContainsAny(name, ":<>\"|?*") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if len(part) > 255 || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") || strings.HasPrefix(strings.ToLower(part), ".atm-") {
			return false
		}
		stem, _, _ := strings.Cut(strings.ToUpper(part), ".")
		switch stem {
		case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
			return false
		}
		if len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && stem[3] >= '1' && stem[3] <= '9' {
			return false
		}
		if strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT") {
			if strings.ContainsAny(stem, "\u00b9\u00b2\u00b3") {
				return false
			}
		}
	}
	return true
}

// CheckDestination is read-only, including when the configured plugin root is absent.
func CheckDestination(root, id string) error {
	if !plugin.IDPattern.MatchString(id) || !portablePath(id) {
		return fmt.Errorf("invalid plugin ID")
	}
	info, err := os.Lstat(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("plugin root must be a real directory")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if strings.EqualFold(e.Name(), id) {
			return fmt.Errorf("existing local plugin will not be overwritten: %s", id)
		}
	}
	return nil
}

func (c *Client) Prepare(ctx context.Context, id string, pinned *Snapshot) (*DownloadPlan, error) {
	if !plugin.IDPattern.MatchString(id) || !portablePath(id) {
		return nil, fmt.Errorf("invalid plugin ID")
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	var snapshot Snapshot
	var err error
	if pinned == nil {
		snapshot, err = c.snapshot(ctx)
	} else {
		snapshot = *pinned
	}
	if err != nil {
		return nil, err
	}
	if !shaPattern.MatchString(snapshot.revision) || snapshot.files == nil {
		return nil, fmt.Errorf("catalog snapshot must come from the official search")
	}
	files := snapshot.files[id]
	if len(files) == 0 || len(files) > 1000 {
		return nil, fmt.Errorf("plugin missing or exceeds 1000 files")
	}
	p := &DownloadPlan{info: DownloadInfo{ID: id, Source: Source, Commit: snapshot.revision, Files: len(files)}, files: map[string]treeEntry{}}
	seen := map[string]bool{}
	spellings := map[string]string{}
	for name, e := range files {
		if !portablePath(name) || !validBlob(e, maxDownloadFile) {
			return nil, fmt.Errorf("unsafe or oversized plugin file: %s", name)
		}
		key := strings.ToLower(name)
		if seen[key] {
			return nil, fmt.Errorf("case-colliding plugin files")
		}
		seen[key] = true
		for part := name; part != "."; part = path.Dir(part) {
			folded := strings.ToLower(part)
			if previous, ok := spellings[folded]; ok && previous != part {
				return nil, fmt.Errorf("case-colliding plugin directories")
			}
			spellings[folded] = part
		}
		p.info.Bytes += *e.Size
		if p.info.Bytes > maxDownloadTotal {
			return nil, fmt.Errorf("plugin exceeds 128 MiB")
		}
		p.files[name] = e
	}
	for name := range seen {
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if seen[parent] {
				return nil, fmt.Errorf("plugin file/directory collision")
			}
		}
	}
	e, hasJSON := files["plugin.json"]
	if !hasJSON {
		e = files["plugin.metadata"]
	}
	b, err := c.blob(ctx, e)
	if err != nil {
		return nil, err
	}
	p.manifest, err = parseManifest(b, id, !hasJSON)
	if err != nil {
		return nil, fmt.Errorf("invalid plugin manifest for %s", id)
	}
	p.info.Name = p.manifest.Name
	return p, nil
}

func checkAbsent(root *os.Root, id string) error {
	_, err := root.Lstat(id)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("existing local plugin will not be overwritten: %s", id)
}

func writeFile(root *os.Root, name string, b []byte, mode os.FileMode) error {
	if err := root.MkdirAll(path.Dir(name), 0700); err != nil {
		return err
	}
	f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if err == nil {
		err = f.Chmod(mode)
	}
	if err == nil {
		err = f.Sync()
	}
	return errors.Join(err, f.Close())
}

func checkShell(name string, b []byte) error {
	switch path.Ext(name) {
	case ".sh", ".conf", ".metadata", ".lang":
		if !utf8.Valid(b) {
			return fmt.Errorf("non-UTF-8 shell file: %s", name)
		}
		if _, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(bytes.NewReader(b), name); err != nil {
			return fmt.Errorf("Bash syntax validation failed: %s", name)
		}
	}
	return nil
}

func validatePayload(root *os.Root, p *DownloadPlan) error {
	if p.manifest.Executor == "legacy-bash" {
		b, err := root.ReadFile("plugin.metadata")
		if err != nil {
			return fmt.Errorf("legacy plugin requires plugin.metadata")
		}
		m, err := parseManifest(b, p.info.ID, true)
		if err != nil || m.Version != p.manifest.Version {
			return fmt.Errorf("legacy metadata does not match manifest")
		}
		values, err := data.ParseAssignments(b, "plugin.metadata")
		if err != nil {
			return err
		}
		entry := values["ATM_PLUGIN_ENTRYPOINT"]
		if entry == "" {
			entry = "plugin.sh"
		}
		if !portablePath(entry) || !strings.HasSuffix(entry, ".sh") {
			return fmt.Errorf("invalid legacy entrypoint")
		}
		for _, name := range []string{entry, "plugin.conf", "lang/en-us.lang"} {
			if _, ok := p.files[name]; !ok {
				return fmt.Errorf("required plugin file missing: %s", name)
			}
		}
	} else {
		for target, name := range p.manifest.Executables {
			e, ok := p.files[name]
			if !ok || (!strings.HasPrefix(target, "windows/") && e.Mode != "100755") {
				return fmt.Errorf("native executable missing or not executable: %s", target)
			}
			if err := plugin.CheckBinary(filepath.Join(root.Name(), filepath.FromSlash(name)), target); err != nil {
				return err
			}
		}
	}
	return nil
}

func verifyStaged(root *os.Root, name string, e treeEntry) error {
	f, err := root.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != *e.Size {
		return fmt.Errorf("staged file size/type mismatch: %s", name)
	}
	if runtime.GOOS != "windows" {
		mode := os.FileMode(0644)
		if e.Mode == "100755" {
			mode = 0755
		}
		if info.Mode().Perm() != mode {
			return fmt.Errorf("staged file mode mismatch: %s", name)
		}
	}
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", *e.Size)
	n, err := io.Copy(h, io.LimitReader(f, *e.Size+1))
	if err != nil {
		return err
	}
	if n != *e.Size || fmt.Sprintf("%x", h.Sum(nil)) != e.SHA {
		return fmt.Errorf("staged file hash mismatch: %s", name)
	}
	return nil
}

// Download publishes code only, never installs its application or loads the plugin.
// Cancellation before publication discards staging; after publication success wins.
func (c *Client) Download(ctx context.Context, p *DownloadPlan, directory string) (result DownloadResult, err error) {
	if p == nil || !shaPattern.MatchString(p.info.Commit) || len(p.files) == 0 {
		return result, fmt.Errorf("unverified download plan")
	}
	directory, err = filepath.Abs(directory)
	if err != nil {
		return result, err
	}
	ctx, cancel := context.WithTimeout(ctx, DownloadTimeout)
	defer cancel()
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if err = CheckDestination(directory, p.info.ID); err != nil {
		return result, err
	}
	if err = os.MkdirAll(directory, 0700); err != nil {
		return result, err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return result, err
	}
	defer root.Close()
	if err = checkAbsent(root, p.info.ID); err != nil {
		return result, err
	}
	lock := ".atm-download-lock-" + p.info.ID
	if err = root.Mkdir(lock, 0700); err != nil {
		return result, fmt.Errorf("plugin download lock unavailable; inspect unfinished downloads: %w", err)
	}
	defer func() {
		if cleanup := root.RemoveAll(lock); cleanup != nil {
			if result.ID != "" {
				result.Warnings = append(result.Warnings, "Published successfully; staging lock cleanup failed: "+lock)
			} else {
				err = errors.Join(err, cleanup)
			}
		}
	}()
	if err = root.Mkdir(lock+"/payload", 0700); err != nil {
		return result, err
	}
	stage, err := root.OpenRoot(lock + "/payload")
	if err != nil {
		return result, err
	}
	defer stage.Close()
	names := make([]string, 0, len(p.files))
	for name := range p.files {
		names = append(names, name)
	}
	sort.Strings(names)
	type identity struct {
		SHA  string `json:"sha"`
		Mode string `json:"mode"`
	}
	receiptFiles := map[string]identity{}
	for _, name := range names {
		if err = ctx.Err(); err != nil {
			return result, err
		}
		e := p.files[name]
		b, readErr := c.blobBytes(ctx, e, maxDownloadFile)
		if readErr != nil {
			return result, readErr
		}
		if err = checkShell(name, b); err != nil {
			return result, err
		}
		mode := os.FileMode(0644)
		if e.Mode == "100755" {
			mode = 0755
		}
		if err = writeFile(stage, name, b, mode); err != nil {
			return result, err
		}
		receiptFiles[name] = identity{SHA: e.SHA, Mode: e.Mode}
	}
	for _, name := range names {
		if err = ctx.Err(); err != nil {
			return result, err
		}
		if err = verifyStaged(stage, name, p.files[name]); err != nil {
			return result, err
		}
	}
	if err = validatePayload(stage, p); err != nil {
		return result, err
	}
	receipt := struct {
		Schema int                 `json:"schema"`
		Source string              `json:"source"`
		Branch string              `json:"branch"`
		Commit string              `json:"commit"`
		Plugin string              `json:"plugin"`
		Files  map[string]identity `json:"files"`
	}{1, Source, "main", p.info.Commit, p.info.ID, receiptFiles}
	b, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return result, err
	}
	if err = writeFile(stage, ".atm-origin.json", append(b, '\n'), 0600); err != nil {
		return result, err
	}
	if err = stage.Close(); err != nil {
		return result, err
	}
	from, err := root.Open(lock)
	if err != nil {
		return result, err
	}
	defer from.Close()
	to, err := root.Open(".")
	if err != nil {
		return result, err
	}
	defer to.Close()
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if err = publishNoReplace(from, to, "payload", p.info.ID); err != nil {
		return result, fmt.Errorf("plugin publication refused: %w", err)
	}
	return DownloadResult{DownloadInfo: p.info, Directory: filepath.Join(directory, p.info.ID), RestartRequired: true}, nil
}
