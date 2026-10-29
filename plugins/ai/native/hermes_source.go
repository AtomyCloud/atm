//go:build linux

package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const (
	hermesSourceAPI         = "https://api.github.com/repos/NousResearch/hermes-agent/"
	hermesSourceArchive     = "https://codeload.github.com/NousResearch/hermes-agent/tar.gz/"
	hermesSourceMaxMetadata = int64(32 << 20)
	hermesSourceMaxDownload = int64(256 << 20)
	hermesSourceMaxFile     = int64(64 << 20)
	hermesSourceMaxFiles    = 50000
	hermesSourceMaxPayload  = int64(512 << 20)
	hermesSourceMaxExpanded = int64(640 << 20)
)

type hermesSource struct {
	Version, Commit, Tree string
	Files                 map[string]hermesSourceFile
}

// Mode is the decimal spelling of the Git mode, not a Unix permission mask.
type hermesSourceFile struct {
	Mode int64
	Size int64
	SHA  string
}

func hermesSourceSHA(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == sha1.Size && strings.ToLower(s) == s
}

func hermesSourcePath(s string) bool {
	if !fs.ValidPath(s) || s == "." || len(s) > 1024 || strings.Count(s, "/") > 32 || strings.ContainsAny(s, "\\:") {
		return false
	}
	for _, p := range strings.Split(s, "/") {
		if len(p) > 255 || strings.EqualFold(p, ".git") {
			return false
		}
	}
	for _, c := range s {
		if c < 32 || c == 127 {
			return false
		}
	}
	return true
}

func hermesObjectHash(kind string, data []byte) string {
	h := sha1.New() // Git object identity, not a standalone authenticity signature.
	fmt.Fprintf(h, "%s %d\x00", kind, len(data))
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

// Rebuild every tree, including Git's directory-aware ordering, so the pinned
// root commits to the complete inventory rather than trusting blob IDs alone.
func hermesSourceTrees(ctx context.Context, s hermesSource) (map[string]string, error) {
	if !versionRE.MatchString(s.Version) || !hermesSourceSHA(s.Commit) || !hermesSourceSHA(s.Tree) || len(s.Files) == 0 || len(s.Files) > hermesSourceMaxFiles {
		return nil, fmt.Errorf("invalid Hermes source identity or inventory")
	}
	dirs := map[string]bool{".": true}
	var total int64
	for name, f := range s.Files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !hermesSourcePath(name) || (f.Mode != 100644 && f.Mode != 100755) || !hermesSourceSHA(f.SHA) || f.Size < 0 || f.Size > hermesSourceMaxFile || f.Size > hermesSourceMaxPayload-total {
			return nil, fmt.Errorf("invalid Hermes tracked file: %s", name)
		}
		total += f.Size
		for p := path.Dir(name); p != "."; p = path.Dir(p) {
			if _, exists := s.Files[p]; exists {
				return nil, fmt.Errorf("Hermes file ancestor conflict")
			}
			dirs[p] = true
		}
		if len(dirs)+len(s.Files) > hermesSourceMaxFiles {
			return nil, fmt.Errorf("Hermes tree entry limit")
		}
	}
	children := map[string][]string{}
	for name := range s.Files {
		children[path.Dir(name)] = append(children[path.Dir(name)], name)
	}
	for name := range dirs {
		if name != "." {
			children[path.Dir(name)] = append(children[path.Dir(name)], name)
		}
	}
	hashes := map[string]string{}
	var tree func(string) (string, error)
	tree = func(dir string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		names := children[dir]
		key := func(n string) string {
			if dirs[n] {
				return path.Base(n) + "/"
			}
			return path.Base(n)
		}
		sort.Slice(names, func(i, j int) bool { return key(names[i]) < key(names[j]) })
		var data bytes.Buffer
		for _, n := range names {
			mode, sha := strconv.FormatInt(s.Files[n].Mode, 10), s.Files[n].SHA
			if dirs[n] {
				var err error
				sha, err = tree(n)
				if err != nil {
					return "", err
				}
				mode = "40000"
			}
			fmt.Fprintf(&data, "%s %s\x00", mode, path.Base(n))
			raw, _ := hex.DecodeString(sha)
			data.Write(raw)
		}
		hashes[dir] = hermesObjectHash("tree", data.Bytes())
		return hashes[dir], nil
	}
	_, err := tree(".")
	return hashes, err
}

func (s hermesSource) validate(ctx context.Context) (map[string]string, error) {
	trees, err := hermesSourceTrees(ctx, s)
	if err != nil {
		return nil, err
	}
	if trees["."] != s.Tree {
		return nil, fmt.Errorf("Hermes Git tree hash mismatch")
	}
	return trees, nil
}

// No caller-selected origins, authentication, release asset URLs, or redirects.
// Canonical API/codeload URLs must respond directly; even same-host moves fail.
func (b *backend) fetchHermesSource(ctx context.Context, raw string, max int64, consume func(io.Reader) error) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.RawPath != "" {
		return fmt.Errorf("invalid Hermes source URL")
	}
	valid := false
	if strings.HasPrefix(raw, hermesSourceAPI) {
		rel := strings.TrimPrefix(raw, hermesSourceAPI)
		switch {
		case rel == "releases/latest":
			valid = true
		case strings.HasPrefix(rel, "releases/tags/v"):
			valid = versionRE.MatchString(strings.TrimPrefix(rel, "releases/tags/v"))
		case strings.HasPrefix(rel, "git/ref/tags/v"):
			valid = versionRE.MatchString(strings.TrimPrefix(rel, "git/ref/tags/v"))
		case strings.HasPrefix(rel, "git/tags/"):
			valid = hermesSourceSHA(strings.TrimPrefix(rel, "git/tags/"))
		case strings.HasPrefix(rel, "git/commits/"):
			valid = hermesSourceSHA(strings.TrimPrefix(rel, "git/commits/"))
		case strings.HasPrefix(rel, "git/trees/") && strings.HasSuffix(rel, "?recursive=1"):
			valid = hermesSourceSHA(strings.TrimSuffix(strings.TrimPrefix(rel, "git/trees/"), "?recursive=1"))
		}
	} else if strings.HasPrefix(raw, hermesSourceArchive) {
		valid = hermesSourceSHA(strings.TrimPrefix(raw, hermesSourceArchive))
	}
	if !valid || max <= 0 || max > hermesSourceMaxDownload {
		return fmt.Errorf("fixed Hermes source endpoint required")
	}
	c := *b.client
	c.Timeout = 5 * time.Minute
	c.Jar = nil
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return fmt.Errorf("Hermes source redirect rejected") }
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("User-Agent", "ATM-AI-Preview/0.0.1")
	if u.Host == "api.github.com" {
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.ContentLength > max || (resp.Header.Get("Content-Encoding") != "" && resp.Header.Get("Content-Encoding") != "identity") {
		return fmt.Errorf("Hermes source response rejected: status=%d length=%d", resp.StatusCode, resp.ContentLength)
	}
	lr := &io.LimitedReader{R: contextReader{ctx, resp.Body}, N: max + 1}
	if err = consume(lr); err != nil {
		return err
	}
	if _, err = io.Copy(io.Discard, lr); err != nil {
		return err
	}
	n := max + 1 - lr.N
	if n == 0 || n > max || (resp.ContentLength >= 0 && resp.ContentLength != n) {
		return fmt.Errorf("Hermes response size mismatch or limit")
	}
	return ctx.Err()
}

func (b *backend) hermesSourceJSON(ctx context.Context, endpoint string, out any) error {
	return b.fetchHermesSource(ctx, hermesSourceAPI+endpoint, hermesSourceMaxMetadata, func(r io.Reader) error {
		data, err := io.ReadAll(r)
		if err != nil {
			return err
		}
		if int64(len(data)) > hermesSourceMaxMetadata {
			return fmt.Errorf("Hermes metadata limit")
		}
		return decode(data, out, false)
	})
}

func (b *backend) resolveHermesSource(ctx context.Context, version string) (hermesSource, error) {
	s, err := b.resolveHermesSourceIdentity(ctx, version)
	if err != nil {
		return hermesSource{}, err
	}
	return s, nil
}

func (b *backend) resolveHermesSourceIdentity(ctx context.Context, version string) (hermesSource, error) {
	var s hermesSource
	endpoint := "releases/latest"
	if version != "latest" {
		if !versionRE.MatchString(version) {
			return s, fmt.Errorf("stable Hermes version required")
		}
		endpoint = "releases/tags/v" + version
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	var rel struct {
		Tag               string `json:"tag_name"`
		Draft, Prerelease *bool
	}
	if err := b.hermesSourceJSON(ctx, endpoint, &rel); err != nil {
		return s, err
	}
	s.Version = strings.TrimPrefix(rel.Tag, "v")
	if rel.Tag != "v"+s.Version || !versionRE.MatchString(s.Version) || rel.Draft == nil || *rel.Draft || rel.Prerelease == nil || *rel.Prerelease || (version != "latest" && version != s.Version) {
		return s, fmt.Errorf("exact stable Hermes release required")
	}
	type object struct{ Type, SHA string }
	var ref struct {
		Ref    string
		Object object
	}
	if err := b.hermesSourceJSON(ctx, "git/ref/tags/"+rel.Tag, &ref); err != nil {
		return s, err
	}
	if ref.Ref != "refs/tags/"+rel.Tag {
		return s, fmt.Errorf("Hermes tag ref mismatch")
	}
	obj := ref.Object
	seen := map[string]bool{}
	for obj.Type == "tag" {
		if !hermesSourceSHA(obj.SHA) || seen[obj.SHA] || len(seen) >= 8 {
			return s, fmt.Errorf("invalid or excessive Hermes tag chain")
		}
		seen[obj.SHA] = true
		var tag struct {
			SHA    string
			Object object
		}
		if err := b.hermesSourceJSON(ctx, "git/tags/"+obj.SHA, &tag); err != nil {
			return s, err
		}
		if tag.SHA != obj.SHA {
			return s, fmt.Errorf("Hermes tag object mismatch")
		}
		obj = tag.Object
	}
	if obj.Type != "commit" || !hermesSourceSHA(obj.SHA) {
		return s, fmt.Errorf("Hermes tag must resolve to commit")
	}
	s.Commit = obj.SHA
	var commit struct {
		SHA  string
		Tree struct{ SHA string }
	}
	if err := b.hermesSourceJSON(ctx, "git/commits/"+s.Commit, &commit); err != nil {
		return s, err
	}
	if commit.SHA != s.Commit || !hermesSourceSHA(commit.Tree.SHA) {
		return s, fmt.Errorf("Hermes commit/tree mismatch")
	}
	s.Tree = commit.Tree.SHA
	var tree struct {
		SHA       string
		Truncated *bool
		Tree      []struct {
			Path, Mode, Type, SHA string
			Size                  *int64
		}
	}
	if err := b.hermesSourceJSON(ctx, "git/trees/"+s.Tree+"?recursive=1", &tree); err != nil {
		return s, err
	}
	if tree.SHA != s.Tree || tree.Truncated == nil || *tree.Truncated || len(tree.Tree) == 0 || len(tree.Tree) > hermesSourceMaxFiles {
		return s, fmt.Errorf("incomplete or excessive Hermes Git tree")
	}
	s.Files = map[string]hermesSourceFile{}
	directories := map[string]string{}
	seen = map[string]bool{}
	for _, e := range tree.Tree {
		if err := ctx.Err(); err != nil {
			return s, err
		}
		if !hermesSourcePath(e.Path) || seen[e.Path] || !hermesSourceSHA(e.SHA) {
			return s, fmt.Errorf("unsafe or duplicate Hermes tree entry")
		}
		seen[e.Path] = true
		switch {
		case e.Type == "tree" && e.Mode == "040000" && e.Size == nil:
			directories[e.Path] = e.SHA
		case e.Type == "blob" && (e.Mode == "100644" || e.Mode == "100755") && e.Size != nil:
			mode, _ := strconv.ParseInt(e.Mode, 10, 64)
			s.Files[e.Path] = hermesSourceFile{Mode: mode, Size: *e.Size, SHA: e.SHA}
		default:
			return s, fmt.Errorf("unsupported Hermes tree entry (links/submodules refused): %s", e.Path)
		}
	}
	trees, err := s.validate(ctx)
	if err != nil {
		return s, err
	}
	if len(trees) != len(directories)+1 {
		return s, fmt.Errorf("missing Hermes tree ancestors")
	}
	for dir, sha := range directories {
		if trees[dir] != sha {
			return s, fmt.Errorf("Hermes subtree mismatch: %s", dir)
		}
	}
	return s, nil
}

// Destination must already exist, be empty, owned and 0700. On failure it is
// deliberately retained for the caller's lifecycle policy; nothing is executed.
func (b *backend) downloadHermesSource(ctx context.Context, source hermesSource, dest string) error {
	trees, err := source.validate(ctx)
	if err != nil {
		return err
	}
	es, err := entries(dest, 1)
	if err != nil {
		return err
	}
	if len(es) != 0 {
		return fmt.Errorf("empty Hermes source destination required")
	}
	identity, err := os.Lstat(dest)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	err = b.fetchHermesSource(ctx, hermesSourceArchive+source.Commit, hermesSourceMaxDownload, func(r io.Reader) error {
		if err := dirs(dest, false, true); err != nil {
			return err
		}
		current, err := os.Lstat(dest)
		if err != nil || !os.SameFile(identity, current) {
			return fmt.Errorf("Hermes destination changed")
		}
		return extractHermesSource(ctx, source, trees, dest, identity, r)
	})
	if err != nil {
		return err
	}
	return verifyHermesSource(ctx, source, dest)
}

type hermesCountReader struct {
	r io.Reader
	n int64
}

func (r *hermesCountReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	r.n += int64(n)
	return n, err
}

func extractHermesSource(ctx context.Context, s hermesSource, trees map[string]string, dest string, identity os.FileInfo, input io.Reader) error {
	br := bufio.NewReader(input)
	gz, err := gzip.NewReader(br)
	if err != nil {
		return err
	}
	defer gz.Close()
	gz.Multistream(false)
	lr := &io.LimitedReader{R: contextReader{ctx, gz}, N: hermesSourceMaxExpanded + 1}
	counted := &hermesCountReader{r: lr}
	tr := tar.NewReader(counted)
	root, err := os.OpenRoot(dest)
	if err != nil {
		return err
	}
	defer root.Close()
	current, err := root.Stat(".")
	if err != nil || !os.SameFile(identity, current) {
		return fmt.Errorf("Hermes destination changed while opening")
	}
	initial, err := root.Open(".")
	if err != nil {
		return err
	}
	es, readErr := initial.ReadDir(1)
	closeErr := initial.Close()
	if readErr != nil && readErr != io.EOF {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	if len(es) != 0 {
		return fmt.Errorf("Hermes destination is no longer empty")
	}
	prefix := "hermes-agent-" + s.Commit
	seen := map[string]bool{}
	files, global := 0, false
	for count := 0; ; count++ {
		before := counted.n
		h, err := tr.Next()
		if err == io.EOF {
			if counted.n-before < 1024 {
				return fmt.Errorf("Hermes tar end markers missing")
			}
			break
		}
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if count >= hermesSourceMaxFiles+2 {
			return fmt.Errorf("Hermes archive entry limit")
		}
		// Git archives may carry one global commit comment before the root.
		if h.Typeflag == tar.TypeXGlobalHeader {
			if count != 0 || global || len(h.PAXRecords) != 1 || h.PAXRecords["comment"] != s.Commit {
				return fmt.Errorf("unsupported Hermes global tar header")
			}
			global = true
			continue
		}
		for key := range h.PAXRecords {
			if key != "path" && key != "mtime" {
				return fmt.Errorf("unsupported Hermes PAX field: %s", key)
			}
		}
		name := h.Name
		if h.Typeflag == tar.TypeDir {
			name = strings.TrimSuffix(name, "/")
		}
		if (name != prefix && !strings.HasPrefix(name, prefix+"/")) || seen[name] || h.Linkname != "" || h.Mode < 0 || h.Mode&^int64(0777) != 0 {
			return fmt.Errorf("unsafe Hermes archive path/mode")
		}
		seen[name] = true
		rel := strings.TrimPrefix(name, prefix+"/")
		if name == prefix {
			if h.Typeflag != tar.TypeDir || h.Size != 0 || (h.Mode != 0755 && h.Mode != 0775) {
				return fmt.Errorf("invalid Hermes archive root")
			}
			continue
		}
		if !seen[prefix] || !hermesSourcePath(rel) {
			return fmt.Errorf("Hermes archive root/path required")
		}
		if h.Typeflag == tar.TypeDir {
			if trees[rel] == "" || h.Size != 0 || (h.Mode != 0755 && h.Mode != 0775) {
				return fmt.Errorf("unexpected Hermes directory")
			}
			if err = root.MkdirAll(rel, 0700); err != nil {
				return err
			}
			continue
		}
		f, ok := s.Files[rel]
		mode := os.FileMode(0644)
		if f.Mode == 100755 {
			mode = 0755
		}
		// Git tar exporters may retain group-write bits; normalize to the pinned
		// Git executable bit and never materialize group-writable source files.
		crlf := strings.HasSuffix(rel, ".ps1") && h.Size > f.Size && h.Size <= 2*f.Size && h.Size <= hermesSourceMaxFile
		if !ok || (h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA) || (h.Mode != int64(mode) && h.Mode != int64(mode)|0020) || (h.Size != f.Size && !crlf) {
			return fmt.Errorf("unexpected Hermes file/type/size/mode: %s (tracked=%t type=%d mode=%o want=%o size=%d want=%d)", rel, ok, h.Typeflag, h.Mode, mode, h.Size, f.Size)
		}
		var content io.Reader = contextReader{ctx, tr}
		if crlf {
			// Upstream .gitattributes exports PowerShell with CRLF. Restore Git
			// bytes, then require the original pinned blob hash below. Neither
			// archive attributes nor a transformed checksum grant trust.
			data, err := io.ReadAll(content)
			if err != nil {
				return err
			}
			data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
			if int64(len(data)) != f.Size {
				return fmt.Errorf("Hermes PowerShell normalized size mismatch: %s", rel)
			}
			content = contextReader{ctx, bytes.NewReader(data)}
		}
		if err = root.MkdirAll(path.Dir(rel), 0700); err != nil {
			return err
		}
		out, err := root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL|unix.O_NOFOLLOW, 0600)
		if err != nil {
			return err
		}
		hash := sha1.New()
		fmt.Fprintf(hash, "blob %d\x00", f.Size)
		n, copyErr := io.Copy(io.MultiWriter(out, hash), content)
		if copyErr == nil && (n != f.Size || hex.EncodeToString(hash.Sum(nil)) != f.SHA) {
			copyErr = fmt.Errorf("Hermes Git blob mismatch: %s", rel)
		}
		if copyErr == nil {
			copyErr = out.Chmod(mode)
		}
		if copyErr == nil {
			copyErr = out.Sync()
		}
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		files++
	}
	// Bound all padding and consume the gzip checksum. A second gzip member,
	// compressed trailer or nonzero tar trailer is never silently ignored.
	buf := make([]byte, 32<<10)
	for {
		n, err := lr.Read(buf)
		for _, v := range buf[:n] {
			if v != 0 {
				return fmt.Errorf("Hermes tar trailer rejected")
			}
		}
		if lr.N <= 0 {
			return fmt.Errorf("Hermes expanded stream limit")
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	if _, err = br.ReadByte(); err != io.EOF {
		if err != nil {
			return err
		}
		return fmt.Errorf("Hermes gzip trailer rejected")
	}
	if files != len(s.Files) || !seen[prefix] {
		return fmt.Errorf("incomplete Hermes source archive")
	}
	return ctx.Err()
}

// Strict initial-source verification rejects all untracked entries.
func verifyHermesSource(ctx context.Context, source hermesSource, dest string) error {
	return verifyHermesSourceFiles(ctx, source, dest, true)
}

// Post-PM verification checks every pinned file and ancestor. Untracked PM
// output is neither inspected nor trusted/authorized by this function.
func verifyHermesTrackedSource(ctx context.Context, source hermesSource, dest string) error {
	return verifyHermesSourceFiles(ctx, source, dest, false)
}

func verifyHermesSourceFiles(ctx context.Context, source hermesSource, dest string, strict bool) error {
	trees, err := source.validate(ctx)
	if err != nil {
		return err
	}
	if err = dirs(dest, false, true); err != nil {
		return err
	}
	for dir := range trees {
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = dirs(filepath.Join(dest, filepath.FromSlash(dir)), false, true); err != nil {
			return err
		}
	}
	for name, entry := range source.Files {
		if err = ctx.Err(); err != nil {
			return err
		}
		mode := os.FileMode(0644)
		if entry.Mode == 100755 {
			mode = 0755
		}
		f, err := regular(filepath.Join(dest, filepath.FromSlash(name)), mode, entry.Size)
		if err != nil {
			return err
		}
		h := sha1.New()
		fmt.Fprintf(h, "blob %d\x00", entry.Size)
		n, copyErr := io.Copy(h, io.LimitReader(contextReader{ctx, f}, entry.Size+1))
		closeErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if n != entry.Size || hex.EncodeToString(h.Sum(nil)) != entry.SHA {
			return fmt.Errorf("Hermes tracked blob mismatch: %s", name)
		}
	}
	if strict {
		// ReadDir in batches avoids allocating for an unbounded foreign directory.
		var walk func(string) error
		walk = func(rel string) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := dirs(filepath.Join(dest, rel), false, true); err != nil {
				return err
			}
			f, err := os.Open(filepath.Join(dest, rel))
			if err != nil {
				return err
			}
			defer f.Close()
			for {
				es, err := f.ReadDir(128)
				if err != nil && err != io.EOF {
					return err
				}
				for _, e := range es {
					if err := ctx.Err(); err != nil {
						return err
					}
					name := path.Join(rel, e.Name())
					if _, ok := source.Files[name]; ok {
						continue
					}
					if trees[name] == "" {
						return fmt.Errorf("untracked Hermes source entry: %s", name)
					}
					if err := walk(name); err != nil {
						return err
					}
				}
				if err == io.EOF {
					return nil
				}
			}
		}
		if err = walk("."); err != nil {
			return err
		}
	}
	return ctx.Err()
}
