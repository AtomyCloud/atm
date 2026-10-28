package main

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/AtomyCloud/atm/internal/protocol"
)

const maxArchive int64 = 1 << 30
const maxExpanded int64 = 4 << 30
const maxFile int64 = 512 << 20
const maxEntries = 100000

var profileName = regexp.MustCompile(`^(Default|Profile [0-9]+)$`)
var backupName = regexp.MustCompile(`^brave-(profile|bookmarks)-[A-Za-z0-9._-]{1,100}\.tar\.gz$`)

type archiveEntry struct {
	name string
	info os.FileInfo
}

func safeEntry(n string) bool {
	if n == "" || len(n) > 1024 || path.IsAbs(n) || path.Clean(n) != n || n == "." || n == ".." || strings.HasPrefix(n, "../") || strings.Contains(n, "\\") || strings.Count(n, "/") > 64 {
		return false
	}
	for _, r := range n {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}
func gather(ctx context.Context, r *os.Root, bookmarks bool) ([]archiveEntry, error) {
	var out []archiveEntry
	var total int64
	add := func(n string, i os.FileInfo) error {
		if !safeEntry("Brave-Browser/" + n) {
			return fmt.Errorf("unsafe profile name")
		}
		if !i.IsDir() && !i.Mode().IsRegular() {
			return fmt.Errorf("links/special profile entries refused: %s", n)
		}
		if i.Mode()&(os.ModeSetuid|os.ModeSetgid) != 0 || i.Size() > maxFile {
			return fmt.Errorf("unsafe/oversized profile file")
		}
		if i.Mode().IsRegular() {
			total += i.Size()
		}
		if total > maxExpanded || len(out) >= maxEntries-1 {
			return fmt.Errorf("profile bounds exceeded")
		}
		out = append(out, archiveEntry{n, i})
		return nil
	}
	if bookmarks {
		f, e := r.Open(".")
		if e != nil {
			return nil, e
		}
		entries, e := f.ReadDir(maxEntries + 1)
		f.Close()
		if e != nil && e != io.EOF {
			return nil, e
		}
		if len(entries) > maxEntries {
			return nil, fmt.Errorf("profile entry limit")
		}
		for _, entry := range entries {
			if e = ctx.Err(); e != nil {
				return nil, e
			}
			if !profileName.MatchString(entry.Name()) {
				continue
			}
			i, e := r.Lstat(entry.Name())
			if e != nil {
				return nil, e
			}
			if !i.IsDir() || i.Mode()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("profile must be real directory: %s", entry.Name())
			}
			n := entry.Name() + "/Bookmarks"
			i, e = r.Lstat(n)
			if os.IsNotExist(e) {
				continue
			}
			if e != nil {
				return nil, e
			}
			if !i.Mode().IsRegular() {
				return nil, fmt.Errorf("Bookmarks must be regular, not linked")
			}
			if e = add(n, i); e != nil {
				return nil, e
			}
		}
	} else {
		// Bound each directory read as well as the complete inventory.
		var walk func(string) error
		walk = func(dir string) error {
			if e := ctx.Err(); e != nil {
				return e
			}
			f, e := r.Open(dir)
			if e != nil {
				return e
			}
			entries, e := f.ReadDir(maxEntries - len(out))
			f.Close()
			if e != nil && e != io.EOF {
				return e
			}
			if len(entries) >= maxEntries-len(out) {
				return fmt.Errorf("profile entry limit")
			}
			for _, entry := range entries {
				n := path.Join(dir, entry.Name())
				i, e := r.Lstat(n)
				if e != nil {
					return e
				}
				if e = add(n, i); e != nil {
					return e
				}
				if i.IsDir() {
					if e = walk(n); e != nil {
						return e
					}
				}
			}
			return nil
		}
		err := walk(".")
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out, nil
}

type cappedWriter struct {
	w io.Writer
	n int64
}

func (w *cappedWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.n {
		return 0, fmt.Errorf("compressed archive limit exceeded")
	}
	n, e := w.w.Write(p)
	w.n -= int64(n)
	return n, e
}
func writeArchive(ctx context.Context, f *os.File, r *os.Root, entries []archiveEntry, bookmarks bool, snapshots ...map[string]string) error {
	hashes := map[string]string{}
	if len(snapshots) > 0 {
		hashes = snapshots[0]
	}
	gz := gzip.NewWriter(&cappedWriter{f, maxArchive})
	tw := tar.NewWriter(gz)
	if !bookmarks {
		if e := tw.WriteHeader(&tar.Header{Name: "Brave-Browser/", Typeflag: tar.TypeDir, Mode: 0700}); e != nil {
			return e
		}
	}
	for _, entry := range entries {
		if e := ctx.Err(); e != nil {
			return e
		}
		n := entry.name
		if !bookmarks {
			n = "Brave-Browser/" + n
		}
		h := &tar.Header{Name: n, Mode: 0600, Size: entry.info.Size(), Typeflag: tar.TypeReg, ModTime: entry.info.ModTime()}
		if entry.info.IsDir() {
			h.Typeflag = tar.TypeDir
			h.Mode = 0700
			h.Size = 0
			h.Name += "/"
			if e := tw.WriteHeader(h); e != nil {
				return e
			}
			continue
		}
		src, e := regular(r, entry.name, maxFile)
		if e != nil {
			return e
		}
		i, e := src.Stat()
		if e != nil || !os.SameFile(i, entry.info) || i.Size() != entry.info.Size() || !i.ModTime().Equal(entry.info.ModTime()) {
			src.Close()
			return fmt.Errorf("profile changed during backup")
		}
		hash := sha256.New()
		e = tw.WriteHeader(h)
		if e == nil {
			_, e = io.CopyN(io.MultiWriter(tw, hash), contextReader{ctx, src}, h.Size)
		}
		after, se := src.Stat()
		src.Close()
		if e != nil {
			return e
		}
		if se != nil || after.Size() != i.Size() || !after.ModTime().Equal(i.ModTime()) {
			return fmt.Errorf("profile changed during backup")
		}
		hashes[entry.name] = hex.EncodeToString(hash.Sum(nil))
	}
	if e := tw.Close(); e != nil {
		return e
	}
	return gz.Close()
}
func verifySnapshot(ctx context.Context, r *os.Root, before []archiveEntry, hashes map[string]string, bookmarks bool) error {
	after, e := gather(ctx, r, bookmarks)
	if e != nil {
		return e
	}
	if len(before) != len(after) {
		return fmt.Errorf("profile inventory changed during backup")
	}
	for n, entry := range before {
		now := after[n]
		if now.name != entry.name || !os.SameFile(now.info, entry.info) || now.info.Mode() != entry.info.Mode() || now.info.Size() != entry.info.Size() || !now.info.ModTime().Equal(entry.info.ModTime()) {
			return fmt.Errorf("profile inventory changed during backup")
		}
		if entry.info.IsDir() {
			continue
		}
		f, e := regular(r, entry.name, maxFile)
		if e != nil {
			return e
		}
		h := sha256.New()
		_, e = io.Copy(h, contextReader{ctx, io.LimitReader(f, maxFile+1)})
		f.Close()
		if e != nil {
			return e
		}
		if hex.EncodeToString(h.Sum(nil)) != hashes[entry.name] {
			return fmt.Errorf("profile content changed during backup")
		}
	}
	return nil
}
func (b *backend) backup(ctx context.Context, bookmarks bool) error {
	if e := b.closed(ctx, b.profile); e != nil {
		return e
	}
	r, e := openDir(b.profile)
	if e != nil {
		return e
	}
	defer r.Close()
	rootFile, e := r.Open(".")
	if e != nil {
		return e
	}
	rootInfo, e := rootFile.Stat()
	rootFile.Close()
	if e != nil {
		return e
	}
	entries, e := gather(ctx, r, bookmarks)
	if e != nil {
		return e
	}
	if bookmarks && len(entries) == 0 {
		b.log("No Bookmarks files found; no archive created")
		return nil
	}
	if !bookmarks {
		files := 0
		for _, entry := range entries {
			if entry.info.Mode().IsRegular() {
				files++
			}
		}
		if files == 0 {
			return fmt.Errorf("profile contains no regular files; no restorable archive created")
		}
	}
	j, release, e := openJournal(b.state)
	if e != nil {
		return e
	}
	defer release()
	if e = b.closed(ctx, b.profile); e != nil {
		return e
	}
	if e = realDirs(b.backups, true, false); e != nil {
		return e
	}
	kind := "profile"
	if bookmarks {
		kind = "bookmarks"
	}
	f, e := os.CreateTemp(b.backups, ".brave-"+kind+"-"+time.Now().UTC().Format("20060102-150405")+"-*.tar.gz.part")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	defer f.Close()
	rec := operationRecord{Schema: 1, Plugin: "brave", Action: "backup-" + kind, Status: "running", Phase: "compressing", Staging: f.Name(), Target: b.profile}
	if e = j.save(rec); e != nil {
		return e
	}
	hashes := map[string]string{}
	e = writeArchive(ctx, f, r, entries, bookmarks, hashes)
	if e == nil {
		e = f.Sync()
	}
	if ce := f.Close(); e == nil {
		e = ce
	}
	if e == nil {
		e = b.closed(ctx, b.profile)
	}
	if e == nil {
		e = verifySnapshot(ctx, r, entries, hashes, bookmarks)
	}
	if e == nil {
		now, se := os.Lstat(b.profile)
		if se != nil || !os.SameFile(rootInfo, now) {
			e = fmt.Errorf("profile root changed during backup")
		}
	}
	if e == nil {
		e = ctx.Err()
	}
	final := filepath.Join(b.backups, strings.TrimSuffix(strings.TrimPrefix(filepath.Base(f.Name()), "."), ".part"))
	if e == nil {
		e = renameAbsent(f.Name(), final)
	}
	if e == nil {
		e = syncDir(b.backups)
	}
	rec.Status = "completed"
	rec.Phase = "published"
	if e != nil {
		rec.Status = "failed"
		rec.Phase = "not-published"
	}
	se := j.save(rec)
	if e != nil {
		return e
	}
	if se != nil {
		return se
	}
	b.log("Private unencrypted backup created: " + final)
	return nil
}

func hashFile(ctx context.Context, f *os.File) (string, error) {
	if _, e := f.Seek(0, io.SeekStart); e != nil {
		return "", e
	}
	h := sha256.New()
	n, e := io.Copy(h, contextReader{ctx, io.LimitReader(f, maxArchive+1)})
	if e != nil {
		return "", e
	}
	if n > maxArchive {
		return "", fmt.Errorf("archive exceeds compressed limit")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func (b *backend) backupMenu(ctx context.Context) ([]protocol.Choice, error) {
	choices := []protocol.Choice{menuChoice("backup-profile", "backup-profile"), menuChoice("backup-bookmarks", "backup-bookmarks")}
	r, e := openDir(b.backups)
	if os.IsNotExist(e) {
		b.log("No backups directory")
		return choices, nil
	}
	if e != nil {
		return nil, e
	}
	defer r.Close()
	f, e := r.Open(".")
	if e != nil {
		return nil, e
	}
	entries, e := f.ReadDir(513)
	f.Close()
	if e != nil && e != io.EOF {
		return nil, e
	}
	if len(entries) > 512 {
		return nil, fmt.Errorf("backup listing exceeds 512 entries; archive older files separately")
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() > entries[j].Name() })
	var total int64
	count := 0
	for _, entry := range entries {
		name := entry.Name()
		if !backupName.MatchString(name) {
			continue
		}
		count++
		if count > 48 {
			return nil, fmt.Errorf("backup review exceeds 48 archives; archive older files separately")
		}
		f, e := regular(r, name, maxArchive)
		if e != nil {
			return nil, e
		}
		i, e := f.Stat()
		if e != nil {
			f.Close()
			return nil, e
		}
		total += i.Size()
		if total > 2*maxArchive {
			f.Close()
			return nil, fmt.Errorf("backup review exceeds 2 GiB")
		}
		h, e := hashFile(ctx, f)
		f.Close()
		if e != nil {
			return nil, e
		}
		b.log(name + " SHA256 " + h)
		if strings.HasPrefix(name, "brave-profile-") {
			choices = append(choices, protocol.Choice{ID: fmt.Sprintf("restore-%d", count), Label: "Restore " + name, Operation: "menu", Arguments: []string{"restore", name, "--sha256", h, "--accept-restore"}})
		}
	}
	b.log("Restore replaces the complete selected profile and keeps a unique recovery copy. Close Brave first. OS keyring passwords are not made portable.")
	return choices, nil
}

// Validate the complete gzip stream, not only tar's end marker. Extraction is optional.
func scanArchive(ctx context.Context, f *os.File, dest *os.Root, legacyRoot string) error {
	if _, e := f.Seek(0, io.SeekStart); e != nil {
		return e
	}
	raw := bufio.NewReader(contextReader{ctx, io.LimitReader(f, maxArchive+1)})
	gz, e := gzip.NewReader(raw)
	if e != nil {
		return e
	}
	defer gz.Close()
	gz.Multistream(false)
	expanded := &io.LimitedReader{R: gz, N: maxExpanded + (64 << 20)}
	tr := tar.NewReader(expanded)
	seen := map[string]byte{}
	requiredDirs := map[string]bool{}
	root := ""
	count := 0
	var total int64
	files := 0
	for {
		if e = ctx.Err(); e != nil {
			return e
		}
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
		count++
		if count > maxEntries {
			return fmt.Errorf("archive entry limit")
		}
		n := strings.TrimSuffix(h.Name, "/")
		if !safeEntry(n) || len(h.Xattrs) > 0 || h.Linkname != "" {
			return fmt.Errorf("unsafe archive path/extended metadata")
		}
		for key, value := range h.PAXRecords {
			switch key {
			case "path":
				if value != h.Name {
					return fmt.Errorf("conflicting PAX path")
				}
			case "mtime", "atime", "ctime":
				if len(value) > 64 {
					return fmt.Errorf("oversized PAX timestamp")
				}
			default:
				return fmt.Errorf("unsupported PAX metadata: %s", key)
			}
		}
		if h.Typeflag != tar.TypeDir && h.Typeflag != tar.TypeReg {
			return fmt.Errorf("links/special archive entries refused")
		}
		if h.Mode < 0 || h.Mode&07000 != 0 || h.Size < 0 || h.Size > maxFile {
			return fmt.Errorf("unsafe archive mode/size")
		}
		if h.Typeflag == tar.TypeDir && h.Size != 0 {
			return fmt.Errorf("directory with payload")
		}
		if _, ok := seen[n]; ok {
			return fmt.Errorf("duplicate archive entry: %s", n)
		}
		seen[n] = h.Typeflag
		parts := strings.Split(n, "/")
		if root == "" {
			root = parts[0]
		}
		if parts[0] != root || (root != "Brave-Browser" && root != legacyRoot) {
			return fmt.Errorf("unexpected full-profile archive root")
		}
		if len(parts) == 1 {
			if h.Typeflag != tar.TypeDir {
				return fmt.Errorf("full-profile root must be directory")
			}
			continue
		}
		for parent := path.Dir(n); parent != "."; parent = path.Dir(parent) {
			if t, ok := seen[parent]; ok && t != tar.TypeDir {
				return fmt.Errorf("file/directory archive conflict")
			}
			requiredDirs[parent] = true
		}
		if h.Typeflag == tar.TypeReg && requiredDirs[n] {
			return fmt.Errorf("file/directory archive conflict")
		}
		total += h.Size
		if total > maxExpanded {
			return fmt.Errorf("archive expansion limit")
		}
		rel := strings.Join(parts[1:], "/")
		if strings.HasPrefix(rel, "Singleton") && !strings.Contains(rel, "/") {
			return fmt.Errorf("browser lock entry refused")
		}
		if dest != nil {
			if e = dest.MkdirAll(path.Dir(rel), 0700); e != nil {
				return e
			}
			if h.Typeflag == tar.TypeDir {
				if e = dest.MkdirAll(rel, 0700); e != nil {
					return e
				}
			} else {
				out, e := dest.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
				if e != nil {
					return e
				}
				_, e = io.CopyN(out, contextReader{ctx, tr}, h.Size)
				if e == nil {
					e = out.Sync()
				}
				ce := out.Close()
				if e != nil {
					return e
				}
				if ce != nil {
					return ce
				}
			}
		} else {
			if _, e = io.CopyN(io.Discard, contextReader{ctx, tr}, h.Size); e != nil {
				return e
			}
		}
		if h.Typeflag == tar.TypeReg {
			files++
		}
	}
	if root == "" || seen[root] != tar.TypeDir || files == 0 {
		return fmt.Errorf("not a complete full-profile archive")
	}
	// GNU tar pads archives with zero blocks; accept at most 1 MiB of that padding.
	tail, e := io.ReadAll(io.LimitReader(expanded, (1<<20)+1))
	if e != nil {
		return e
	}
	if len(tail) > 1<<20 || expanded.N <= 0 {
		return fmt.Errorf("archive padding/expansion limit")
	}
	for _, v := range tail {
		if v != 0 {
			return fmt.Errorf("trailing archive payload")
		}
	}
	if _, e = raw.Peek(1); e != io.EOF {
		return fmt.Errorf("concatenated/trailing compressed payload")
	}
	return nil
}

func (b *backend) restore(ctx context.Context, name, want string) error {
	if !backupName.MatchString(name) || !strings.HasPrefix(name, "brave-profile-") || !validDigest(want) {
		return fmt.Errorf("selected full-profile archive and SHA256 required")
	}
	if e := b.closed(ctx, b.profile); e != nil {
		return e
	}
	r, e := openDir(b.backups)
	if e != nil {
		return e
	}
	defer r.Close()
	f, e := regular(r, name, maxArchive)
	if e != nil {
		return e
	}
	defer f.Close()
	got, e := hashFile(ctx, f)
	if e != nil {
		return e
	}
	if got != want {
		return fmt.Errorf("selected archive changed; review backups again")
	}
	if e = scanArchive(ctx, f, nil, filepath.Base(b.profile)); e != nil {
		return e
	}
	j, release, e := openJournal(b.state)
	if e != nil {
		return e
	}
	retain := false
	defer func() {
		if !retain {
			release()
		}
	}()
	parent := filepath.Dir(b.profile)
	if e = realDirs(parent, false, false); e != nil {
		return e
	}
	stage, e := os.MkdirTemp(parent, ".atm-brave-restore-")
	if e != nil {
		return e
	}
	defer func() {
		if !retain {
			_ = os.RemoveAll(stage)
		}
	}()
	payload := filepath.Join(stage, "profile")
	if e = os.Mkdir(payload, 0700); e != nil {
		return e
	}
	rec := operationRecord{Schema: 1, Plugin: "brave", Action: "restore", Status: "running", Phase: "staging", Plan: want, Staging: stage, Target: b.profile}
	if e = j.save(rec); e != nil {
		return e
	}
	out, e := openDir(payload)
	if e != nil {
		return e
	}
	e = scanArchive(ctx, f, out, filepath.Base(b.profile))
	out.Close()
	if e == nil {
		got, e = hashFile(ctx, f)
		if got != want && e == nil {
			e = fmt.Errorf("archive changed during extraction")
		}
	}
	if e == nil {
		e = b.closed(ctx, b.profile)
	}
	var old os.FileInfo
	if e == nil {
		old, e = os.Lstat(b.profile)
		if os.IsNotExist(e) {
			e = nil
			old = nil
		} else if e == nil && (!old.IsDir() || old.Mode()&os.ModeSymlink != 0 || !ownedByCurrentUser(old)) {
			e = fmt.Errorf("profile target conflict")
		}
	}
	if e == nil {
		e = ctx.Err()
	}
	if e != nil {
		rec.Status = "failed"
		rec.Phase = "validation-failed"
		_ = j.save(rec)
		return e
	}
	recovery := b.profile + ".before-restore." + filepath.Base(stage)
	rec.Recovery = recovery
	rec.Phase = "prepared"
	if e = j.save(rec); e != nil {
		return e
	}
	if e = b.closed(ctx, b.profile); e != nil {
		return e
	}
	if e = realDirs(parent, false, false); e != nil {
		return e
	}
	now, ne := os.Lstat(b.profile)
	if old == nil {
		if !os.IsNotExist(ne) {
			return fmt.Errorf("profile appeared before replacement")
		}
	} else if ne != nil || !os.SameFile(old, now) || !ownedByCurrentUser(now) || now.Mode() != old.Mode() {
		return fmt.Errorf("profile identity changed before replacement")
	}
	// From here a crash must leave the journal and recovery tree for manual inspection.
	retain = true
	moved := false
	if old != nil {
		if e = renameAbsent(b.profile, recovery); e != nil {
			return e
		}
		moved = true
		if e = syncDir(parent); e != nil {
			return e
		}
	}
	rec.Phase = "old-preserved"
	if e = j.save(rec); e != nil {
		return e
	}
	if e = ctx.Err(); e == nil {
		e = renameAbsent(payload, b.profile)
	}
	if e != nil {
		if moved {
			if undo := renameAbsent(recovery, b.profile); undo != nil {
				return fmt.Errorf("restore failed: %v; rollback blocked: %w; recovery retained at %s", e, undo, recovery)
			}
		}
		rec.Status = "failed"
		rec.Phase = "rolled-back"
		if se := j.save(rec); se == nil {
			retain = false
		}
		return e
	}
	if e = syncDir(parent); e != nil {
		return e
	}
	rec.Status = "completed"
	rec.Phase = "restored"
	if e = j.save(rec); e != nil {
		return e
	}
	retain = false
	b.log("Profile restored from " + name)
	if moved {
		b.log("Old profile retained; never automatically deleted: " + recovery)
	}
	return nil
}
