package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

const marker = ".atm-rea-native-root"
const magic = "atm-rea-native-v1\n"
const receiptName = ".atm-rea-native.json"
const dependencyFile = ".atm-runtime.json"
const maxFile = int64(512 << 20)
const maxTotal = int64(2 << 30)

type entry struct {
	Mode   uint32 `json:"mode"`
	Size   int64  `json:"size,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
	Link   string `json:"link,omitempty"`
}
type receipt struct {
	Schema   int              `json:"schema"`
	Plugin   string           `json:"plugin"`
	Artifact artifact         `json:"artifact"`
	Files    map[string]entry `json:"files"`
}
type destination struct {
	Schema int    `json:"schema"`
	Plugin string `json:"plugin"`
	Base   string `json:"base"`
}

func cleanPath(p string) error {
	if !filepath.IsAbs(p) || filepath.Clean(p) != p || p == "/" {
		return fmt.Errorf("clean absolute dedicated paths required")
	}
	for _, r := range p {
		if r < 32 || r == 127 || r == ':' {
			return fmt.Errorf("control characters/PATH separators refused")
		}
	}
	return nil
}
func within(p, root string) bool { return p == root || strings.HasPrefix(p, root+"/") }
func realDirs(p string, create bool) error {
	if !filepath.IsAbs(p) {
		return fmt.Errorf("absolute directory required")
	}
	cur := "/"
	for _, part := range strings.Split(filepath.Clean(p), "/") {
		if part == "" {
			continue
		}
		cur = filepath.Join(cur, part)
		i, e := os.Lstat(cur)
		if os.IsNotExist(e) && create {
			if e = os.Mkdir(cur, 0700); e != nil {
				return e
			}
			i, e = os.Lstat(cur)
		}
		if e != nil {
			return e
		}
		if !i.IsDir() || i.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsafe directory %s", cur)
		}
	}
	return nil
}
func (b *backend) validateBase() error {
	if err := cleanPath(b.base); err != nil {
		return err
	}
	for _, p := range []string{b.home, b.req.AppsDir, "/usr", "/usr/local", "/etc", "/var", "/opt", "/tmp", "/home"} {
		if b.base == p {
			return fmt.Errorf("dedicated REA base required")
		}
	}
	for _, p := range []string{b.req.Home, filepath.Join(b.home, ".codex"), filepath.Join(b.home, ".claude"), filepath.Join(b.home, ".agents")} {
		if within(b.base, p) || within(p, b.root) {
			return fmt.Errorf("REA base overlaps protected application/agent paths")
		}
	}
	if within(b.state, b.base) || within(b.base, filepath.Dir(b.state)) {
		return fmt.Errorf("state and application paths overlap")
	}
	return nil
}
func readRegular(path string, limit int64) ([]byte, error) {
	if err := realDirs(filepath.Dir(path), false); err != nil {
		return nil, err
	}
	i, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !i.Mode().IsRegular() || i.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 || i.Size() > limit {
		return nil, fmt.Errorf("unsafe/oversized regular file %s", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("file grew beyond limit")
	}
	return data, err
}

// JSON receipts must not hide a second value or duplicate an ownership field.
func decodeJSON(data []byte, out any, strict bool) error {
	d := json.NewDecoder(bytes.NewReader(data))
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 64 {
			return fmt.Errorf("JSON nesting limit")
		}
		t, e := d.Token()
		if e != nil {
			return e
		}
		delim, ok := t.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				k, e := d.Token()
				if e != nil {
					return e
				}
				s, ok := k.(string)
				if !ok || seen[s] {
					return fmt.Errorf("duplicate JSON key")
				}
				seen[s] = true
				if e = walk(depth + 1); e != nil {
					return e
				}
			}
		case '[':
			for d.More() {
				if e = walk(depth + 1); e != nil {
					return e
				}
			}
		default:
			return fmt.Errorf("invalid JSON delimiter")
		}
		_, e = d.Token()
		return e
	}
	if err := walk(0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("trailing JSON data")
	}
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return fmt.Errorf("null JSON object")
	}
	d = json.NewDecoder(bytes.NewReader(data))
	if strict {
		d.DisallowUnknownFields()
	}
	return d.Decode(out)
}
func privateJSON(path string, out any) error {
	data, err := readRegular(path, 32<<20)
	if err != nil {
		return err
	}
	i, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if i.Mode().Perm() != 0600 {
		return fmt.Errorf("private receipt mode required")
	}
	return decodeJSON(data, out, true)
}
func (b *backend) remembered() error {
	var d destination
	err := privateJSON(b.state, &d)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if d.Schema != 1 || d.Plugin != "rea" || cleanPath(d.Base) != nil {
		return fmt.Errorf("invalid native destination receipt")
	}
	b.setBase(d.Base)
	return b.validateBase()
}
func (b *backend) current() (string, error) {
	if err := realDirs(b.root, false); os.IsNotExist(err) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	target, err := os.Readlink(filepath.Join(b.root, "current"))
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("foreign current entry preserved")
	}
	v := strings.TrimPrefix(target, "versions/")
	if target != "versions/"+v || !stable.MatchString(v) {
		return "", fmt.Errorf("foreign current link preserved")
	}
	return v, nil
}
func (b *backend) versions() ([]string, error) {
	p := filepath.Join(b.root, "versions")
	if err := realDirs(p, false); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	es, err := f.ReadDir(1025)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if len(es) > 1024 {
		return nil, fmt.Errorf("too many versions")
	}
	var out []string
	for _, e := range es {
		if !e.IsDir() || !stable.MatchString(e.Name()) {
			return nil, fmt.Errorf("unknown version entry preserved: %s", e.Name())
		}
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out, nil
}
func (b *backend) preflight(ctx context.Context) error {
	if err := b.validateBase(); err != nil {
		return err
	}
	if err := realDirs(filepath.Dir(b.state), false); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := realDirs(b.root, false); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	data, err := readRegular(filepath.Join(b.root, marker), 128)
	if os.IsNotExist(err) {
		f, e := os.Open(b.root)
		if e != nil {
			return e
		}
		defer f.Close()
		names, e := f.Readdirnames(1)
		if len(names) != 0 || e != io.EOF {
			return fmt.Errorf("nonempty unowned native root preserved")
		}
		return nil
	}
	if err != nil {
		return err
	}
	if string(data) != magic {
		return fmt.Errorf("foreign native root marker")
	}
	vs, err := b.versions()
	if err != nil {
		return err
	}
	for _, v := range vs {
		if err = b.verifyOwned(ctx, v); err != nil {
			return err
		}
	}
	v, err := b.current()
	if err != nil {
		return err
	}
	if v != "" {
		if err = b.verifyOwned(ctx, v); err != nil {
			return err
		}
	}
	if err = realDirs(filepath.Join(b.root, "bin"), false); err != nil && !os.IsNotExist(err) {
		return err
	}
	target, err := os.Readlink(filepath.Join(b.root, "bin/rea"))
	if err == nil && target != "../current/bin/rea" {
		return fmt.Errorf("foreign REA entry preserved")
	}
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("foreign REA entry preserved: %w", err)
	}
	if _, err = os.Lstat(filepath.Join(b.root, ".atm-native-lock")); err == nil {
		return fmt.Errorf("REA is locked/interrupted; inspect .atm-native-lock manually")
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
func (b *backend) locked(fn func() error) error {
	if err := realDirs(filepath.Dir(b.state), true); err != nil {
		return err
	}
	lock := b.state + ".lock"
	if err := os.Mkdir(lock, 0700); err != nil {
		return fmt.Errorf("REA destination locked/interrupted; inspect %s: %w", lock, err)
	}
	defer os.Remove(lock)
	// Another request may have completed between discovery and lock acquisition.
	var remembered destination
	err := privateJSON(b.state, &remembered)
	if err == nil {
		if remembered.Schema != 1 || remembered.Plugin != "rea" || remembered.Base != b.base {
			return fmt.Errorf("destination changed before lock acquisition; refresh status")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err = b.preflight(context.Background()); err != nil {
		return err
	}
	return fn()
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if e := r.ctx.Err(); e != nil {
		return 0, e
	}
	return r.r.Read(p)
}
func hashFile(ctx context.Context, path string) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	n, e := io.Copy(h, io.LimitReader(contextReader{ctx, f}, maxFile+1))
	if n > maxFile {
		return "", fmt.Errorf("file too large")
	}
	return hex.EncodeToString(h.Sum(nil)), e
}
func inventory(ctx context.Context, dir string) (map[string]entry, error) {
	if err := realDirs(dir, false); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	files := map[string]entry{}
	var total int64
	err = fs.WalkDir(root.FS(), ".", func(name string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		if name == "." || name == receiptName {
			return nil
		}
		if !fs.ValidPath(name) || strings.ContainsAny(name, "\x00\r\n\\") || len(files) >= 100000 {
			return fmt.Errorf("unsafe/excessive inventory")
		}
		i, e := d.Info()
		if e != nil {
			return e
		}
		if i.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
			return fmt.Errorf("special permission bits")
		}
		item := entry{Mode: uint32(i.Mode())}
		switch {
		case i.IsDir():
		case i.Mode()&os.ModeSymlink != 0:
			item.Link, e = root.Readlink(name)
			if e != nil {
				return e
			}
			if filepath.IsAbs(item.Link) {
				return fmt.Errorf("absolute package link")
			}
			resolved, e := filepath.EvalSymlinks(filepath.Join(dir, name))
			if e != nil || !within(resolved, dir) {
				return fmt.Errorf("escaping/broken package link")
			}
		case i.Mode().IsRegular():
			item.Size = i.Size()
			if item.Size > maxFile || item.Size > maxTotal-total {
				return fmt.Errorf("oversized package")
			}
			total += item.Size
			f, e := root.Open(name)
			if e != nil {
				return e
			}
			h := sha256.New()
			n, e := io.Copy(h, io.LimitReader(contextReader{ctx, f}, item.Size+1))
			f.Close()
			if e != nil {
				return e
			}
			if n != item.Size {
				return fmt.Errorf("package changed while reading")
			}
			item.SHA256 = hex.EncodeToString(h.Sum(nil))
		default:
			return fmt.Errorf("special package entry")
		}
		files[name] = item
		return nil
	})
	return files, err
}
func (b *backend) readReceipt(v string) (receipt, error) {
	var r receipt
	if !stable.MatchString(v) {
		return r, fmt.Errorf("invalid version")
	}
	err := privateJSON(filepath.Join(b.root, "versions", v, receiptName), &r)
	if err != nil {
		return r, err
	}
	if r.Schema != 1 || r.Plugin != "rea" || r.Artifact.Version != v || len(r.Files) == 0 || r.Artifact.validate() != nil {
		return r, fmt.Errorf("invalid REA ownership receipt")
	}
	return r, nil
}
func (b *backend) verifyOwned(ctx context.Context, v string) error {
	r, err := b.readReceipt(v)
	if err != nil {
		return err
	}
	dir := filepath.Join(b.root, "versions", v)
	actual, err := inventory(ctx, dir)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(r.Files, actual) {
		return fmt.Errorf("REA %s contains changed/unknown files; preserving it", v)
	}
	return validatePackage(dir, v)
}
