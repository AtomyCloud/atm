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
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"golang.org/x/sys/unix"
)

type ollamaEntry struct {
	Mode   uint32 `json:"mode"`
	Size   int64  `json:"size,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
	Link   string `json:"link,omitempty"`
}

type ollamaReceipt struct {
	Schema   int
	Plugin   string
	Artifact artifact
	ROCm     *artifact `json:",omitempty"`
	Files    map[string]ollamaEntry
}

func ollamaReadReceipt(dir string) (ollamaReceipt, error) {
	var rec ollamaReceipt
	f, err := regular(filepath.Join(dir, "receipt.json"), 0600, ollamaMaxReceipt)
	if err != nil {
		return rec, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, ollamaMaxReceipt+1))
	if err != nil {
		return rec, err
	}
	if int64(len(data)) > ollamaMaxReceipt {
		return rec, fmt.Errorf("Ollama receipt bound exceeded")
	}
	err = decode(data, &rec, true)
	if err != nil {
		return rec, err
	}
	// encoding/json accepts case-insensitive struct fields. Receipts use exact
	// names so an alias cannot silently override a second artifact or its digest.
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(data, &fields); err != nil {
		return rec, err
	}
	for key, value := range fields {
		switch key {
		case "Schema", "Plugin", "Files":
		case "Artifact", "ROCm":
			var artifactFields map[string]json.RawMessage
			if err = json.Unmarshal(value, &artifactFields); err != nil {
				return rec, err
			}
			for name := range artifactFields {
				switch name {
				case "Provider", "Version", "Arch", "URL", "SHA256", "Format", "Entry", "Size":
				default:
					return rec, fmt.Errorf("noncanonical Ollama artifact field")
				}
			}
		default:
			return rec, fmt.Errorf("noncanonical Ollama receipt field")
		}
	}
	return rec, err
}

func ollamaWriteReceipt(dir string, rec ollamaReceipt) error {
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if int64(len(data))+1 > ollamaMaxReceipt {
		return fmt.Errorf("Ollama receipt bound exceeded")
	}
	f, err := os.OpenFile(filepath.Join(dir, "receipt.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	if err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err != nil {
		return err
	}
	return ce
}

func ollamaEntryAt(ctx context.Context, r *os.Root, name string) (ollamaEntry, os.FileInfo, error) {
	var e ollamaEntry
	if err := ctx.Err(); err != nil {
		return e, nil, err
	}
	i, err := r.Lstat(name)
	if err != nil {
		return e, nil, err
	}
	if owner(i) != uint32(os.Geteuid()) || i.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return e, nil, fmt.Errorf("unowned or unsafe Ollama entry")
	}
	e.Mode = uint32(i.Mode())
	switch {
	case i.IsDir():
		if i.Mode().Perm() != 0700 {
			return e, nil, fmt.Errorf("Ollama directory mode changed")
		}
	case i.Mode()&os.ModeSymlink != 0:
		if !singleLink(i) {
			return e, nil, fmt.Errorf("hardlinked Ollama symlink refused")
		}
		e.Link, err = r.Readlink(name)
		if err != nil {
			return e, nil, err
		}
		if !ollamaLinkText(e.Link) {
			return e, nil, fmt.Errorf("unsafe Ollama link")
		}
	case i.Mode().IsRegular():
		if !singleLink(i) || (i.Mode().Perm() != 0600 && i.Mode().Perm() != 0755) || i.Size() < 0 || i.Size() > ollamaMaxExpanded {
			return e, nil, fmt.Errorf("unsafe Ollama file")
		}
		f, err := r.OpenFile(name, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
		if err != nil {
			return e, nil, err
		}
		opened, err := f.Stat()
		if err != nil || !os.SameFile(i, opened) {
			f.Close()
			return e, nil, fmt.Errorf("Ollama file changed while opening")
		}
		h := sha256.New()
		n, err := io.Copy(h, io.LimitReader(contextReader{ctx, f}, i.Size()+1))
		end, statErr := f.Stat()
		ce := f.Close()
		if err != nil {
			return e, nil, err
		}
		if statErr != nil || n != i.Size() || end.Size() != i.Size() || end.Mode() != i.Mode() || owner(end) != owner(i) || !singleLink(end) || !end.ModTime().Equal(i.ModTime()) {
			return e, nil, fmt.Errorf("Ollama file changed while hashing")
		}
		if ce != nil {
			return e, nil, ce
		}
		e.Size, e.SHA256 = n, hex.EncodeToString(h.Sum(nil))
	default:
		return e, nil, fmt.Errorf("special Ollama file refused")
	}
	after, err := r.Lstat(name)
	if err != nil || !os.SameFile(i, after) || i.Mode() != after.Mode() || owner(i) != owner(after) {
		return e, nil, fmt.Errorf("Ollama entry changed during verification")
	}
	if e.Link != "" {
		link, err := r.Readlink(name)
		if err != nil || link != e.Link {
			return e, nil, fmt.Errorf("Ollama link changed during verification")
		}
	}
	return e, i, nil
}

func ollamaInventory(ctx context.Context, tree string) (map[string]ollamaEntry, error) {
	if err := dirs(tree, false, true); err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(tree)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	files := map[string]ollamaEntry{}
	var total int64
	var walk func(string) error
	walk = func(name string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := r.Lstat(name)
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() && info.Size() > ollamaMaxExpanded-total {
			return fmt.Errorf("Ollama inventory byte limit")
		}
		e, original, err := ollamaEntryAt(ctx, r, name)
		if err != nil {
			return err
		}
		if name != "." {
			if !ollamaPath(name) || len(files) >= ollamaMaxEntries {
				return fmt.Errorf("unsafe or excessive Ollama inventory")
			}
			if e.Size > ollamaMaxExpanded-total {
				return fmt.Errorf("Ollama inventory byte limit")
			}
			total += e.Size
			files[name] = e
		}
		if !os.FileMode(e.Mode).IsDir() {
			return nil
		}
		f, err := r.OpenFile(name, os.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
		if err != nil {
			return err
		}
		opened, err := f.Stat()
		if err != nil || !os.SameFile(original, opened) {
			f.Close()
			return fmt.Errorf("Ollama directory changed while opening")
		}
		es, err := f.ReadDir(ollamaMaxEntries + 1)
		f.Close()
		if err != nil && err != io.EOF {
			return err
		}
		if len(es) > ollamaMaxEntries {
			return fmt.Errorf("Ollama directory entry bound exceeded")
		}
		for _, child := range es {
			if err := walk(path.Join(name, child.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	if err = walk("."); err != nil {
		return nil, err
	}
	if files["bin/ollama"].Mode != 0755 || files["bin/ollama"].Size == 0 || !os.FileMode(files["lib/ollama"].Mode).IsDir() {
		return nil, fmt.Errorf("incomplete Ollama tree")
	}
	if err = ollamaCheckLinks(files); err != nil {
		return nil, err
	}
	return files, ctx.Err()
}

func (b *backend) ollamaVerify(ctx context.Context, v, dir string) (ollamaReceipt, error) {
	var rec ollamaReceipt
	if !versionRE.MatchString(v) {
		return rec, fmt.Errorf("invalid Ollama version")
	}
	es, err := entries(dir, 2)
	if err != nil {
		return rec, err
	}
	if len(es) != 2 {
		return rec, fmt.Errorf("unknown or missing Ollama version entry")
	}
	for _, e := range es {
		if e.Name() != "tree" && e.Name() != "receipt.json" {
			return rec, fmt.Errorf("unknown Ollama version entry preserved")
		}
	}
	rec, err = ollamaReadReceipt(dir)
	if err != nil {
		return rec, err
	}
	if rec.Plugin != "ai" || rec.Artifact.Provider != "ollama" || rec.Artifact.Version != v || rec.Artifact.Arch != b.arch || rec.Artifact.validate() != nil || len(rec.Files) == 0 || len(rec.Files) > ollamaMaxEntries {
		return rec, fmt.Errorf("invalid Ollama bundle receipt")
	}
	// Schema 1 remains the base-only format. Schema 2 requires both artifacts.
	if (rec.Schema == 1 && rec.ROCm != nil) || (rec.Schema == 2 && rec.ROCm == nil) || (rec.Schema != 1 && rec.Schema != 2) {
		return rec, fmt.Errorf("invalid Ollama receipt variant; base and ROCm artifacts cannot be mixed or omitted")
	}
	if rec.ROCm != nil {
		if err = ollamaValidateROCm(*rec.ROCm, v, b.arch); err != nil {
			return rec, err
		}
	}
	actual, err := ollamaInventory(ctx, filepath.Join(dir, "tree"))
	if err != nil {
		return rec, err
	}
	if !reflect.DeepEqual(actual, rec.Files) {
		return rec, fmt.Errorf("Ollama bundle changed; preserving complete installation")
	}
	if err = validateELF(filepath.Join(dir, "tree", "bin", "ollama"), b.arch, false); err != nil {
		return rec, err
	}
	return rec, ctx.Err()
}

func (b *backend) ollamaVersion(v string) string {
	return filepath.Join(b.root("ollama"), "versions", v)
}

func (b *backend) ollamaCurrent() (string, error) {
	p := filepath.Join(b.root("ollama"), "current")
	i, err := os.Lstat(p)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if i.Mode()&os.ModeSymlink == 0 || owner(i) != uint32(os.Geteuid()) || !singleLink(i) {
		return "", fmt.Errorf("foreign Ollama current entry preserved")
	}
	target, err := os.Readlink(p)
	if err != nil {
		return "", err
	}
	v := strings.TrimSuffix(strings.TrimPrefix(target, "versions/"), "/tree")
	if !versionRE.MatchString(v) || target != "versions/"+v+"/tree" {
		return "", fmt.Errorf("foreign Ollama current link preserved")
	}
	return v, nil
}

func (b *backend) ollamaInspect(ctx context.Context) ([]string, string, error) {
	root := b.root("ollama")
	if err := dirs(root, false, true); os.IsNotExist(err) {
		return nil, "", nil
	} else if err != nil {
		return nil, "", err
	}
	var own ownership
	if err := readJSON(filepath.Join(root, "owner.json"), &own); err != nil {
		return nil, "", err
	}
	if own != (ownership{1, "ai", "ollama"}) {
		return nil, "", fmt.Errorf("foreign Ollama store; no adoption")
	}
	es, err := entries(root, 5)
	if err != nil {
		return nil, "", err
	}
	for _, e := range es {
		if e.Name() != "owner.json" && e.Name() != "versions" && e.Name() != "current" && e.Name() != "bin" && e.Name() != "path.json" {
			return nil, "", fmt.Errorf("unknown Ollama store entry preserved")
		}
	}
	es, err = entries(filepath.Join(root, "versions"), 1024)
	if err != nil {
		return nil, "", err
	}
	var vs []string
	for _, e := range es {
		if !e.IsDir() || !versionRE.MatchString(e.Name()) {
			return nil, "", fmt.Errorf("unknown Ollama version preserved")
		}
		if _, err := b.ollamaVerify(ctx, e.Name(), b.ollamaVersion(e.Name())); err != nil {
			return nil, "", err
		}
		vs = append(vs, e.Name())
	}
	sort.Strings(vs)
	current, err := b.ollamaCurrent()
	if err != nil {
		return nil, "", err
	}
	if current != "" {
		found := false
		for _, v := range vs {
			found = found || v == current
		}
		if !found {
			return nil, "", fmt.Errorf("Ollama active version missing")
		}
	}
	if _, err := b.inspectIntegration("ollama", current); err != nil {
		return nil, "", err
	}
	return vs, current, ctx.Err()
}
