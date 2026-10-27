package updates

import (
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"

	"github.com/AtomyCloud/atm/internal/catalog"
)

type inventory map[string]catalog.Fingerprint

// Refuse links in every existing ancestor, not merely the last path component.
func safeAncestors(name string) error {
	for p := filepath.Clean(name); ; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("linked path refused: %s", p)
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	return nil
}
func readOptional(name string) ([]byte, error) {
	if err := safeAncestors(name); err != nil {
		return nil, err
	}
	info, err := os.Lstat(name)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 4<<20 {
		return nil, fmt.Errorf("unsafe state file")
	}
	f, err := os.Open(name)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 4<<20 {
		return nil, fmt.Errorf("unsafe state file")
	}
	b, err := io.ReadAll(io.LimitReader(f, 4<<20+1))
	if len(b) > 4<<20 {
		return nil, fmt.Errorf("oversized state file")
	}
	return b, err
}
func scan(name string) (inventory, error) {
	if err := safeAncestors(name); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	out := inventory{}
	var total int64
	err = fs.WalkDir(root.FS(), ".", func(n string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if n == "." {
			return nil
		}
		if len(out) >= 10000 {
			return fmt.Errorf("local component exceeds file limit")
		}
		if n != ".atm-origin.json" && !catalog.ValidPayloadPath(n) {
			return fmt.Errorf("unsafe local path: %s", n)
		}
		info, err := root.Lstat(n)
		if err != nil {
			return err
		}
		if info.IsDir() {
			out[n] = catalog.Fingerprint{Mode: "dir"}
			return nil
		}
		if !info.Mode().IsRegular() || info.Size() > 32<<20 {
			return fmt.Errorf("linked, special or oversized file: %s", n)
		}
		total += info.Size()
		if total > 512<<20 {
			return fmt.Errorf("local component exceeds backup limit")
		}
		f, err := root.Open(n)
		if err != nil {
			return err
		}
		defer f.Close()
		h := sha1.New()
		fmt.Fprintf(h, "blob %d\x00", info.Size())
		count, err := io.Copy(h, io.LimitReader(f, info.Size()+1))
		if err != nil {
			return err
		}
		if count != info.Size() {
			return fmt.Errorf("file changed during inspection")
		}
		mode := "100644"
		if info.Mode().Perm()&0111 != 0 && runtime.GOOS != "windows" {
			mode = "100755"
		}
		out[n] = catalog.Fingerprint{SHA: fmt.Sprintf("%x", h.Sum(nil)), Mode: mode}
		return nil
	})
	return out, err
}
func matches(actual inventory, owned map[string]catalog.Fingerprint) bool {
	for n, want := range owned {
		got, ok := actual[n]
		if !ok || got.SHA != want.SHA || (runtime.GOOS != "windows" && got.Mode != want.Mode) {
			return false
		}
	}
	return true
}
func collisions(actual inventory, base, target map[string]catalog.Fingerprint) error {
	for n := range target {
		if _, owned := base[n]; !owned {
			if _, ok := actual[n]; ok {
				return fmt.Errorf("new upstream file collides with local entry: %s", n)
			}
		}
		for local, f := range actual {
			if strings.EqualFold(local, n) && local != n {
				return fmt.Errorf("case collision: %s", n)
			}
			if strings.HasPrefix(strings.ToLower(n), strings.ToLower(local)+"/") && f.Mode != "dir" {
				return fmt.Errorf("file/directory collision: %s", n)
			}
			if strings.HasPrefix(strings.ToLower(local), strings.ToLower(n)+"/") {
				return fmt.Errorf("directory/file collision: %s", n)
			}
			// Case-only directory changes are unsafe on case-insensitive filesystems.
			for a := path.Dir(n); a != "."; a = path.Dir(a) {
				if strings.EqualFold(local, a) && local != a {
					return fmt.Errorf("directory case collision: %s", n)
				}
			}
		}
	}
	return nil
}
func copyTree(ctxCheck func() error, source, dest string, expected inventory) error {
	if err := safeAncestors(source); err != nil {
		return err
	}
	if err := safeAncestors(dest); err != nil {
		return err
	}
	if err := os.Mkdir(dest, 0700); err != nil {
		return err
	}
	from, err := os.OpenRoot(source)
	if err != nil {
		return err
	}
	defer from.Close()
	to, err := os.OpenRoot(dest)
	if err != nil {
		return err
	}
	defer to.Close()
	for n, f := range expected {
		if err := ctxCheck(); err != nil {
			return err
		}
		if f.Mode == "dir" {
			if err := to.MkdirAll(n, 0700); err != nil {
				return err
			}
			continue
		}
		if err := copyFile(from, to, n, f); err != nil {
			return err
		}
	}
	actual, err := scan(dest)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(expected, actual) {
		return fmt.Errorf("copy readback mismatch")
	}
	return nil
}
func copyFile(from, to *os.Root, n string, f catalog.Fingerprint) error {
	if err := to.MkdirAll(path.Dir(n), 0700); err != nil {
		return err
	}
	info, err := from.Lstat(n)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > 32<<20 {
		return fmt.Errorf("unsafe copy source")
	}
	src, err := from.Open(n)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := to.OpenFile(n, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = io.Copy(dst, io.LimitReader(src, 32<<20+1))
	if err == nil {
		mode := os.FileMode(0644)
		if f.Mode == "100755" {
			mode = 0755
		}
		err = dst.Chmod(mode)
	}
	if err == nil {
		err = dst.Sync()
	}
	return errors.Join(err, dst.Close())
}
func writeJSON(name string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if len(b)+1 > 4<<20 {
		return fmt.Errorf("state record exceeds recovery read limit")
	}
	return atomicBytes(name, append(b, '\n'))
}
func atomicBytes(name string, b []byte) error {
	if err := safeAncestors(name); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(name), ".atm-write-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), name)
}
func noReplace(source, destination string) error {
	if err := safeAncestors(source); err != nil {
		return err
	}
	if err := safeAncestors(destination); err != nil {
		return err
	}
	a, err := os.Open(filepath.Dir(source))
	if err != nil {
		return err
	}
	defer a.Close()
	b, err := os.Open(filepath.Dir(destination))
	if err != nil {
		return err
	}
	defer b.Close()
	return catalog.PublishDirectory(a, b, filepath.Base(source), filepath.Base(destination))
}
func refuseGit(name string) error {
	for p := name; ; p = filepath.Dir(p) {
		marker := filepath.Join(p, ".git")
		info, err := os.Lstat(marker)
		if err == nil {
			// Match legacy: an empty ancestor .git directory is not a repository.
			// Sandboxes may expose such placeholders at /tmp and other ancestors.
			entries, readErr := os.ReadDir(marker)
			if p == name || !info.IsDir() || readErr != nil || len(entries) != 0 {
				return fmt.Errorf("selective updates refuse Git checkouts/worktrees")
			}
		}
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	return nil
}
