//go:build linux

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type inventory struct {
	Path string `json:"path"`
	Dir  bool   `json:"dir"`
	Mode uint32 `json:"mode"`
	Hash string `json:"sha256,omitempty"`
}
type receipt struct {
	Schema  int         `json:"schema"`
	Plugin  string      `json:"plugin"`
	Target  string      `json:"target"`
	Commit  string      `json:"commit"`
	Archive string      `json:"archive_sha256"`
	Files   []inventory `json:"files"`
	RC      string      `json:"generated_zshrc_sha256,omitempty"`
	RCPath  string      `json:"generated_zshrc_path,omitempty"`
}

func inventories(es []entry) []inventory {
	out := make([]inventory, 0, len(es))
	for _, en := range es {
		out = append(out, inventory{en.Path, en.Dir, en.Mode, en.Hash})
	}
	return out
}
func (b *backend) ownedReceipt(ctx context.Context) (*receipt, error) {
	if e := dirs(b.state, false, true); e != nil {
		return nil, e
	}
	v, e := readFile(filepath.Join(b.state, "receipt.json"), 4<<20, true)
	if e != nil {
		return nil, fmt.Errorf("native ownership required; manual/legacy installation preserved: %w", e)
	}
	if e = uniqueJSON(v); e != nil {
		return nil, e
	}
	var r receipt
	dec := json.NewDecoder(bytes.NewReader(v))
	dec.DisallowUnknownFields()
	if e = dec.Decode(&r); e != nil {
		return nil, e
	}
	if e = dec.Decode(new(any)); e != io.EOF {
		return nil, fmt.Errorf("trailing receipt data")
	}
	if r.Schema != 1 || r.Plugin != "zsh" || r.Target != b.omz || !validHex(r.Commit, 20) || !validHex(r.Archive, 32) || len(r.Files) == 0 || len(r.Files) > maxEntries {
		return nil, fmt.Errorf("foreign native receipt")
	}
	es, e := scan(ctx, map[string]string{"omz": b.omz})
	if e != nil {
		return nil, e
	}
	want, _ := json.Marshal(r.Files)
	actual, _ := json.Marshal(inventories(es))
	if !bytes.Equal(want, actual) {
		return nil, fmt.Errorf("native framework locally modified; preserve edits and review manually")
	}
	if r.RC != "" {
		if r.RCPath != filepath.Join(b.dot, ".zshrc") {
			return nil, fmt.Errorf("generated loader belongs to a different dot directory; preserve original path")
		}
		v, e := readFile(r.RCPath, maxFile, false)
		if e != nil || digest(v) != r.RC {
			return nil, fmt.Errorf("generated .zshrc edited or missing; preserve and review manually")
		}
	}
	return &r, nil
}
func officialEntries(ctx context.Context, v []byte, commit string) ([]entry, error) {
	if len(v) > 32<<20 || !validHex(commit, 20) {
		return nil, fmt.Errorf("invalid official archive")
	}
	raw := bytes.NewReader(v)
	gz, e := gzip.NewReader(raw)
	if e != nil {
		return nil, e
	}
	defer gz.Close()
	gz.Multistream(false)
	lr := &io.LimitedReader{R: contextReader{ctx, gz}, N: 100 << 20}
	tr := tar.NewReader(lr)
	prefix := "ohmyzsh-" + commit
	out := []entry{}
	links := map[string]string{}
	indices := map[string]int{}
	total := 0
	for {
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		if h.Typeflag == tar.TypeXGlobalHeader {
			if len(h.PAXRecords) != 1 || h.PAXRecords["comment"] != commit {
				return nil, fmt.Errorf("unexpected global archive metadata")
			}
			continue
		}
		if len(h.PAXRecords) != 0 || (h.Linkname != "" && h.Typeflag != tar.TypeSymlink) || h.Size < 0 || h.Size > maxFile || h.Mode&07000 != 0 {
			return nil, fmt.Errorf("unsafe archive metadata")
		}
		name := strings.TrimSuffix(h.Name, "/")
		rel := "."
		if name != prefix {
			if !strings.HasPrefix(name, prefix+"/") {
				return nil, fmt.Errorf("archive outside pinned root")
			}
			rel = strings.TrimPrefix(name, prefix+"/")
		}
		if !safeRelative(rel) {
			return nil, fmt.Errorf("unsafe archive path")
		}
		if _, exists := indices[rel]; exists {
			return nil, fmt.Errorf("duplicate archive path")
		}
		indices[rel] = len(out)
		en := entry{Root: "omz", Path: rel, Mode: 0600 | uint32(h.Mode&0111)}
		switch h.Typeflag {
		case tar.TypeSymlink:
			if h.Size != 0 || h.Linkname == "" || len(h.Linkname) > 1000 || filepath.IsAbs(h.Linkname) || strings.Contains(h.Linkname, "\\") {
				return nil, fmt.Errorf("unsafe official alias")
			}
			for _, c := range h.Linkname {
				if c < 32 || c == 127 {
					return nil, fmt.Errorf("unsafe alias text")
				}
			}
			target := filepath.Clean(filepath.Join(filepath.Dir(rel), h.Linkname))
			if !safeRelative(target) {
				return nil, fmt.Errorf("official alias escapes archive")
			}
			links[rel] = target
		case tar.TypeDir:
			if h.Size != 0 {
				return nil, fmt.Errorf("directory has payload")
			}
			en.Dir = true
			en.Mode = 0700
		case tar.TypeReg, tar.TypeRegA:
			en.Data, e = io.ReadAll(io.LimitReader(tr, maxFile+1))
			if e != nil {
				return nil, e
			}
			if int64(len(en.Data)) != h.Size {
				return nil, fmt.Errorf("short archive entry")
			}
			en.Hash = digest(en.Data)
			total += len(en.Data)
		default:
			return nil, fmt.Errorf("links/special entries unsupported in official archive")
		}
		out = append(out, en)
		if len(out) > maxEntries || total > maxData {
			return nil, fmt.Errorf("archive exceeds expansion limit")
		}
	}
	// Official aliases are copied, never created as filesystem links. No chains.
	for name, target := range links {
		i, ok := indices[target]
		_, chain := links[target]
		if !ok || chain || out[i].Dir {
			return nil, fmt.Errorf("official alias target must be a direct regular file")
		}
		j := indices[name]
		out[j].Data = out[i].Data
		out[j].Hash = out[i].Hash
		out[j].Mode = out[i].Mode
		total += len(out[j].Data)
		if total > maxData {
			return nil, fmt.Errorf("materialized aliases exceed bound")
		}
	}
	rest, e := io.ReadAll(io.LimitReader(lr, 1<<20))
	if e != nil {
		return nil, e
	}
	for _, v := range rest {
		if v != 0 {
			return nil, fmt.Errorf("unexpected tar trailer")
		}
	}
	if lr.N <= 0 || raw.Len() != 0 {
		return nil, fmt.Errorf("archive exceeds bounds or has trailing gzip streams")
	}
	// Drain to EOF to verify the gzip checksum even for tar padding.
	tail, e := io.ReadAll(io.LimitReader(gz, 2))
	if e != nil {
		return nil, e
	}
	if len(tail) != 0 {
		return nil, fmt.Errorf("oversized tar padding")
	}
	if e = validateSnapshot(snapshot{snapshotFormat, out}, map[string]string{"omz": "unused"}); e != nil {
		return nil, e
	}
	found := false
	for _, en := range out {
		if en.Path == "oh-my-zsh.sh" && !en.Dir && len(en.Data) > 0 {
			found = true
		}
	}
	if !found {
		return nil, fmt.Errorf("official framework entrypoint missing")
	}
	return out, nil
}
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
func (b *backend) loader() []byte {
	return []byte("# ATM Zsh native loader; updates are managed through ATM.\n" +
		"export ZSH=" + shellQuote(b.omz) + "\nexport ZSH_CUSTOM=" + shellQuote(b.custom) + "\n" +
		"export ZSH_CACHE_DIR=" + shellQuote(filepath.Join(b.state, "cache")) + "\nZSH_COMPDUMP=\"$ZSH_CACHE_DIR/.zcompdump\"\n" +
		"zstyle ':omz:update' mode disabled\nZSH_THEME=\"robbyrussell\"\nplugins=(git)\nsource \"$ZSH/oh-my-zsh.sh\"\n")
}
func (b *backend) apply(ctx context.Context, action string, opt options) error {
	acct, e := b.verifiedAccount()
	if e != nil {
		return e
	}
	if action == "remove" && strings.HasPrefix(filepath.Base(acct.Shell), "zsh") {
		return fmt.Errorf("choose and verify a non-Zsh login shell before removal")
	}
	if action == "remove" {
		if e = b.removalSafe(); e != nil {
			return e
		}
	}
	apt, e := b.resolve("apt-get")
	if e != nil {
		return e
	}
	sudo, e := b.resolve("sudo")
	if e != nil {
		return e
	}
	var old *receipt
	check := func() error {
		if action == "install" {
			if _, e := os.Lstat(b.omz); e == nil {
				return fmt.Errorf("existing manual/legacy/native framework preserved; use update for owned installs")
			} else if !os.IsNotExist(e) {
				return e
			}
			if _, e := os.Lstat(filepath.Join(b.state, "receipt.json")); e == nil {
				return fmt.Errorf("existing receipt requires review")
			} else if !os.IsNotExist(e) {
				return e
			}
			return nil
		}
		var e error
		old, e = b.ownedReceipt(ctx)
		return e
	}
	if e = check(); e != nil {
		return e
	}
	before, e := b.capture(ctx)
	if e != nil {
		return e
	}
	o, e := b.begin(action)
	if e != nil {
		return e
	}
	if e = check(); e != nil {
		return e
	}
	if _, e = b.snapshot(ctx, o); e != nil {
		return e
	}
	staged := map[string]string{}
	var next receipt
	if action != "remove" {
		if e = o.phase("downloading-pinned-official-archive"); e != nil {
			return e
		}
		v, e := b.fetch(ctx, opt.commit)
		if e != nil {
			return e
		}
		es, e := officialEntries(ctx, v, opt.commit)
		if e != nil {
			return e
		}
		if e = dirs(filepath.Dir(b.omz), false, false); e != nil {
			return e
		}
		stage, e := o.stage(filepath.Dir(b.omz))
		if e != nil {
			return e
		}
		payload := filepath.Join(stage, "payload")
		if e = stageEntries(ctx, payload, es); e != nil {
			return e
		}
		staged[b.omz] = payload
		inv, e := scan(ctx, map[string]string{"omz": payload})
		if e != nil {
			return e
		}
		next = receipt{Schema: 1, Plugin: "zsh", Target: b.omz, Commit: opt.commit, Archive: digest(v), Files: inventories(inv)}
		if old != nil {
			next.RC = old.RC
			next.RCPath = old.RCPath
		}
		rc := filepath.Join(b.dot, ".zshrc")
		if _, e = os.Lstat(rc); os.IsNotExist(e) {
			if e = dirs(filepath.Join(b.state, "cache"), true, true); e != nil {
				return e
			}
			tmp, e := o.stage(b.dot)
			if e != nil {
				return e
			}
			p := filepath.Join(tmp, "zshrc")
			data := b.loader()
			if e = writeNew(p, data, 0600); e != nil {
				return e
			}
			staged[rc] = p
			next.RC = digest(data)
			next.RCPath = rc
		} else if e != nil {
			return e
		} else if next.RC == "" {
			b.log("Existing .zshrc preserved: manual activation required. Set the native OMZ path and disable OMZ auto-updates before sourcing it; installation does not imply activation.")
		}
	} else {
		staged[b.omz] = ""
		if old.RC != "" {
			staged[old.RCPath] = ""
		}
	}
	now, e := b.capture(ctx)
	if e != nil {
		return e
	}
	if !sameEntries(before, now) {
		return fmt.Errorf("data changed before package operation")
	}
	if e = check(); e != nil {
		return e
	}
	actual, e := b.verifiedAccount()
	if e != nil {
		return e
	}
	if actual != acct {
		return fmt.Errorf("account changed before package operation")
	}
	if action == "remove" {
		if e = b.removalSafe(); e != nil {
			return e
		}
	}
	for _, args := range aptArguments(action) {
		if e = ctx.Err(); e != nil {
			return e
		}
		if e = o.phase("apt-running"); e != nil {
			return e
		}
		_, e = b.run(ctx, sudo, append([]string{"-n", "--", apt}, args...))
		if e != nil {
			return fmt.Errorf("APT failed; system changes may be partial, inspect retained journal and processes: %w", e)
		}
	}
	now, e = b.capture(ctx)
	if e != nil {
		return e
	}
	if !sameEntries(before, now) {
		return fmt.Errorf("data changed during APT; snapshot/staging retained")
	}
	if e = check(); e != nil {
		return e
	}
	if e = b.invalidateReceipt(o); e != nil {
		return e
	}
	if e = b.replace(ctx, o, staged); e != nil {
		return e
	}
	if action != "remove" {
		v, e := json.Marshal(next)
		if e != nil {
			return e
		}
		if e = writeNew(filepath.Join(b.state, "receipt.json"), v, 0600); e != nil {
			return e
		}
	}
	if _, e = os.Lstat(filepath.Join(o.dir, "invalidated-receipt.json")); e == nil {
		p, e := os.MkdirTemp(b.state, "previous-receipt-*")
		if e != nil {
			return e
		}
		if e = b.rename(filepath.Join(o.dir, "invalidated-receipt.json"), filepath.Join(p, "receipt.json")); e != nil {
			return e
		}
	}
	return o.finish()
}
