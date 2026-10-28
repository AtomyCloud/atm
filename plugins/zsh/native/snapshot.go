//go:build linux

package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/AtomyCloud/atm/internal/protocol"
)

const maxData = 64 << 20
const maxFile = 16 << 20
const maxSnapshot = 100 << 20
const maxEntries = 20000

type entry struct {
	Root string `json:"root"`
	Path string `json:"path"`
	Dir  bool   `json:"dir"`
	Mode uint32 `json:"mode"`
	Hash string `json:"sha256,omitempty"`
	Data []byte `json:"data,omitempty"`
}
type snapshot struct {
	Format  string  `json:"format"`
	Entries []entry `json:"entries"`
}

const snapshotFormat = "atm-zsh-native-v1"

func safeRelative(p string) bool {
	if p == "." {
		return true
	}
	if p == "" || !utf8.ValidString(p) || len(p) > 1000 || filepath.IsAbs(p) || filepath.ToSlash(filepath.Clean(p)) != p || strings.Contains(p, "\\") || strings.HasPrefix(p, "../") || p == ".." {
		return false
	}
	for _, r := range p {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}
func scan(ctx context.Context, roots map[string]string) ([]entry, error) {
	out := []entry{}
	total := 0
	keys := make([]string, 0, len(roots))
	for k := range roots {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		base := roots[key]
		if e := dirs(filepath.Dir(base), false, false); e != nil {
			if os.IsNotExist(e) {
				continue
			}
			return nil, e
		}
		if _, e := os.Lstat(base); os.IsNotExist(e) {
			continue
		} else if e != nil {
			return nil, e
		}
		e := filepath.WalkDir(base, func(p string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if e := ctx.Err(); e != nil {
				return e
			}
			i, e := d.Info()
			if e != nil {
				return e
			}
			st, ok := i.Sys().(*syscall.Stat_t)
			if !owned(i) || i.Mode()&(os.ModeSymlink|os.ModeSetuid|os.ModeSetgid) != 0 || i.Mode().Perm()&0022 != 0 || (!i.IsDir() && !i.Mode().IsRegular()) || (!i.IsDir() && (!ok || st.Nlink != 1)) {
				return fmt.Errorf("linked, writable, foreign or special data refused: %s", p)
			}
			rel, e := filepath.Rel(base, p)
			if e != nil || !safeRelative(rel) {
				return fmt.Errorf("unsafe snapshot path")
			}
			en := entry{Root: key, Path: rel, Dir: i.IsDir(), Mode: uint32(i.Mode().Perm())}
			if !en.Dir {
				en.Data, e = readFile(p, maxFile, false)
				if e != nil {
					return e
				}
				en.Hash = digest(en.Data)
				total += len(en.Data)
			}
			out = append(out, en)
			if len(out) > maxEntries || total > maxData {
				return fmt.Errorf("snapshot resource limit exceeded")
			}
			return nil
		})
		if e != nil {
			return nil, e
		}
	}
	return out, nil
}
func sameEntries(a, b []entry) bool {
	av, _ := json.Marshal(a)
	bv, _ := json.Marshal(b)
	return bytes.Equal(av, bv)
}
func (b *backend) capture(ctx context.Context) ([]entry, error) {
	es, e := scan(ctx, b.roots())
	if e != nil {
		return nil, e
	}
	if e = validateSnapshot(snapshot{snapshotFormat, es}, b.roots()); e != nil {
		return nil, e
	}
	return es, nil
}
func (b *backend) snapshot(ctx context.Context, o *operation) (string, error) {
	before, e := b.capture(ctx)
	if e != nil {
		return "", e
	}
	if e = dirs(b.backups, true, true); e != nil {
		return "", e
	}
	f, e := os.CreateTemp(b.backups, ".zsh-snapshot-*")
	if e != nil {
		return "", e
	}
	defer os.Remove(f.Name())
	gz := gzip.NewWriter(f)
	e = json.NewEncoder(gz).Encode(snapshot{snapshotFormat, before})
	if ce := gz.Close(); e == nil {
		e = ce
	}
	if e == nil {
		e = f.Sync()
	}
	if ce := f.Close(); e == nil {
		e = ce
	}
	if e != nil {
		return "", e
	}
	after, e := b.capture(ctx)
	if e != nil {
		return "", e
	}
	if !sameEntries(before, after) {
		return "", fmt.Errorf("configuration changed during snapshot; close all Zsh sessions")
	}
	if e = ctx.Err(); e != nil {
		return "", e
	}
	dest := filepath.Join(b.backups, "zsh-native-"+time.Now().UTC().Format("20060102T150405Z")+"-"+filepath.Base(f.Name())[14:]+".json.gz")
	if e = b.rename(f.Name(), dest); e != nil {
		return "", e
	}
	o.Snapshot = dest
	if e = o.phase("snapshot-published"); e != nil {
		return "", e
	}
	b.log("Private unencrypted snapshot: " + dest)
	return dest, nil
}
func (b *backend) backup(ctx context.Context) error {
	if _, e := b.capture(ctx); e != nil {
		return e
	}
	o, e := b.begin("backup")
	if e != nil {
		return e
	}
	if _, e = b.snapshot(ctx, o); e != nil {
		return e
	}
	return o.finish()
}
func validateSnapshot(s snapshot, roots map[string]string) error {
	if s.Format != snapshotFormat {
		return fmt.Errorf("incompatible snapshot: native atm-zsh-native-v1 required; legacy atm-zsh-backup-v1 is not imported")
	}
	if len(s.Entries) > maxEntries {
		return fmt.Errorf("too many snapshot entries")
	}
	seen := map[string]entry{}
	total := 0
	for _, en := range s.Entries {
		if _, ok := roots[en.Root]; !ok || !safeRelative(en.Path) || en.Mode > 0777 || en.Mode&0022 != 0 {
			return fmt.Errorf("unsafe snapshot entry")
		}
		tree := en.Root == "omz" || en.Root == "custom"
		if (!tree && (en.Path != "." || en.Dir)) || (tree && en.Path == "." && !en.Dir) {
			return fmt.Errorf("invalid snapshot root type")
		}
		k := en.Root + "/" + en.Path
		if _, ok := seen[k]; ok {
			return fmt.Errorf("duplicate snapshot entry")
		}
		seen[k] = en
		if en.Dir {
			if len(en.Data) != 0 || en.Hash != "" {
				return fmt.Errorf("directory with data")
			}
		} else {
			if len(en.Data) > maxFile || !validHex(en.Hash, 32) || digest(en.Data) != en.Hash {
				return fmt.Errorf("snapshot content mismatch")
			}
			total += len(en.Data)
		}
	}
	if total > maxData {
		return fmt.Errorf("snapshot size limit")
	}
	for _, en := range s.Entries {
		if en.Path == "." {
			continue
		}
		p := filepath.Dir(en.Path)
		parent, ok := seen[en.Root+"/"+p]
		if !ok || !parent.Dir {
			return fmt.Errorf("missing directory or file ancestor")
		}
	}
	return nil
}
func (b *backend) readSnapshot(ctx context.Context, path, hash string) (snapshot, error) {
	var s snapshot
	if e := absolutePath(path); e != nil {
		return s, e
	}
	if filepath.Dir(path) != b.backups {
		return s, fmt.Errorf("select archive directly inside configured backup directory")
	}
	if e := dirs(b.backups, false, true); e != nil {
		return s, e
	}
	if !strings.HasPrefix(filepath.Base(path), "zsh-native-") || !strings.HasSuffix(path, ".json.gz") {
		return s, fmt.Errorf("legacy/incompatible archive refused; native .json.gz required")
	}
	v, e := readFile(path, maxSnapshot, true)
	if e != nil {
		return s, e
	}
	if digest(v) != hash {
		return s, fmt.Errorf("archive changed since review")
	}
	raw := bytes.NewReader(v)
	gz, e := gzip.NewReader(raw)
	if e != nil {
		return s, e
	}
	gz.Multistream(false)
	data, e := io.ReadAll(io.LimitReader(contextReader{ctx, gz}, maxSnapshot+1))
	ce := gz.Close()
	if e != nil {
		return s, e
	}
	if ce != nil {
		return s, ce
	}
	if len(data) > maxSnapshot || raw.Len() != 0 {
		return s, fmt.Errorf("oversized or trailing archive stream")
	}
	if e = uniqueJSON(data); e != nil {
		return s, e
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if e = dec.Decode(&s); e != nil {
		return s, e
	}
	if e = dec.Decode(new(any)); e != io.EOF {
		return s, fmt.Errorf("trailing snapshot JSON")
	}
	if e = validateSnapshot(s, b.roots()); e != nil {
		return s, e
	}
	return s, nil
}
func (b *backend) backupMenu(ctx context.Context) ([]protocol.Choice, error) {
	if e := dirs(b.backups, false, true); os.IsNotExist(e) {
		return nil, nil
	} else if e != nil {
		return nil, e
	}
	f, e := os.Open(b.backups)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	names, e := f.Readdirnames(201)
	if e != nil && e != io.EOF {
		return nil, e
	}
	if len(names) > 200 {
		return nil, fmt.Errorf("backup listing exceeds 200 entries; select archive explicitly")
	}
	sort.Strings(names)
	cs := []protocol.Choice{}
	for _, name := range names {
		if strings.HasPrefix(name, "zsh-") && strings.HasSuffix(name, ".tar.gz") {
			b.log("Legacy archive retained; native import unsupported: " + name)
			continue
		}
		if !strings.HasPrefix(name, "zsh-native-") || !strings.HasSuffix(name, ".json.gz") {
			continue
		}
		if e = ctx.Err(); e != nil {
			return nil, e
		}
		p := filepath.Join(b.backups, name)
		v, e := readFile(p, maxSnapshot, true)
		if e != nil {
			return nil, e
		}
		if len(cs) >= 40 {
			return nil, fmt.Errorf("more than 40 native backups; select archive explicitly")
		}
		cs = append(cs, choice(fmt.Sprintf("restore-%d", len(cs)), name, "menu", "restore", p, "--sha256", digest(v), "--accept-restore", "--accept-snapshot"))
	}
	b.log("Restore replaces only roots present in snapshot, retains displaced data and invalidates native installation ownership. Close all Zsh sessions before confirming.")
	return cs, nil
}
func stageEntries(ctx context.Context, path string, entries []entry) error {
	sorted := append([]entry(nil), entries...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })
	for _, en := range sorted {
		if e := ctx.Err(); e != nil {
			return e
		}
		p := path
		if en.Path != "." {
			p = filepath.Join(path, en.Path)
		}
		if en.Dir {
			if e := os.Mkdir(p, 0700); e != nil {
				return e
			}
		} else {
			if e := writeNew(p, en.Data, 0600); e != nil {
				return e
			}
		}
	}
	// Set exact validated modes after populating children, independent of umask.
	for i := len(sorted) - 1; i >= 0; i-- {
		en := sorted[i]
		p := path
		if en.Path != "." {
			p = filepath.Join(path, en.Path)
		}
		if e := os.Chmod(p, os.FileMode(en.Mode)); e != nil {
			return e
		}
	}
	return nil
}
func (b *backend) replace(ctx context.Context, o *operation, staged map[string]string) error {
	keys := make([]string, 0, len(staged))
	for target := range staged {
		keys = append(keys, target)
	}
	sort.Strings(keys)
	for _, target := range keys {
		if e := ctx.Err(); e != nil {
			return e
		}
		if e := dirs(filepath.Dir(target), false, false); e != nil {
			return e
		}
		rec, e := os.MkdirTemp(filepath.Dir(target), ".atm-zsh-recovery-*")
		if e != nil {
			return e
		}
		move := moveRecord{Target: target, Staged: staged[target], Recovery: filepath.Join(rec, "previous")}
		o.Moves = append(o.Moves, move)
		if e = o.phase("replacement-prepared"); e != nil {
			return e
		}
		had := false
		if _, e = os.Lstat(target); e == nil {
			if e = b.rename(target, move.Recovery); e != nil {
				return e
			}
			had = true
		} else if !os.IsNotExist(e) {
			return e
		}
		if move.Staged != "" {
			if e = b.rename(move.Staged, target); e != nil {
				if had {
					if re := b.rename(move.Recovery, target); re != nil {
						return fmt.Errorf("replacement failed (%v), rollback failed (%v); retain journal/recovery", e, re)
					}
				}
				return e
			}
		}
		if e = o.phase("replacement-published"); e != nil {
			return e
		}
		b.log("Retained recovery: " + rec)
	}
	return nil
}
func (b *backend) restore(ctx context.Context, path, hash string) error {
	s, e := b.readSnapshot(ctx, path, hash)
	if e != nil {
		return e
	}
	before, e := b.capture(ctx)
	if e != nil {
		return e
	}
	o, e := b.begin("restore")
	if e != nil {
		return e
	}
	if _, e = b.snapshot(ctx, o); e != nil {
		return e
	}
	byRoot := map[string][]entry{}
	for _, en := range s.Entries {
		byRoot[en.Root] = append(byRoot[en.Root], en)
	}
	staged := map[string]string{}
	for key, entries := range byRoot {
		target := b.roots()[key]
		if e = dirs(filepath.Dir(target), false, false); e != nil {
			return e
		}
		temp, e := o.stage(filepath.Dir(target))
		if e != nil {
			return e
		}
		p := filepath.Join(temp, "payload")
		if e = stageEntries(ctx, p, entries); e != nil {
			return e
		}
		staged[target] = p
	}
	now, e := b.capture(ctx)
	if e != nil {
		return e
	}
	if !sameEntries(before, now) {
		return fmt.Errorf("data changed before restore; preserving staged snapshot")
	}
	if e = b.invalidateReceipt(o); e != nil {
		return e
	}
	if e = b.replace(ctx, o, staged); e != nil {
		return fmt.Errorf("restore incomplete: earlier roots may already be replaced; originals, staging and prior receipt retained; inspect journal/recovery paths in %s: %w", filepath.Join(o.dir, "journal.json"), e)
	}
	// Preserve invalidated authority separately, never leave it at receipt.json.
	if _, e = os.Lstat(filepath.Join(o.dir, "invalidated-receipt.json")); e == nil {
		p, e := os.MkdirTemp(b.state, "restored-receipt-*")
		if e != nil {
			return e
		}
		if e = b.rename(filepath.Join(o.dir, "invalidated-receipt.json"), filepath.Join(p, "receipt.json")); e != nil {
			return e
		}
	}
	return o.finish()
}

// Reject duplicate object keys at every level before decoding security metadata.
func uniqueJSON(v []byte) error {
	if !utf8.Valid(v) {
		return fmt.Errorf("invalid JSON encoding")
	}
	d := json.NewDecoder(bytes.NewReader(v))
	d.UseNumber()
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 24 {
			return fmt.Errorf("JSON nesting exceeds bound")
		}
		token, e := d.Token()
		if e != nil {
			return e
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				key, e := d.Token()
				if e != nil {
					return e
				}
				s, ok := key.(string)
				if !ok || seen[s] {
					return fmt.Errorf("duplicate/invalid JSON key")
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
	if e := walk(0); e != nil {
		return e
	}
	if _, e := d.Token(); e != io.EOF {
		return fmt.Errorf("trailing JSON data")
	}
	return nil
}
