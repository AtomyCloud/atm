//go:build linux

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type hermesSourceFixture struct {
	s         hermesSource
	b         *backend
	data      map[string][]byte
	responses map[string][]byte
	calls     []string
}

func hermesTestHash(kind string, data []byte) string {
	sum := sha1.Sum(append([]byte(fmt.Sprintf("%s %d\x00", kind, len(data))), data...))
	return hex.EncodeToString(sum[:])
}

func hermesTestTree(entries ...string) string {
	var raw []byte
	for i := 0; i < len(entries); i += 2 {
		raw = append(raw, []byte(entries[i]+"\x00")...)
		sha, _ := hex.DecodeString(entries[i+1])
		raw = append(raw, sha...)
	}
	return hermesTestHash("tree", raw)
}

func hermesTestJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func newHermesSourceFixture(t *testing.T) *hermesSourceFixture {
	t.Helper()
	f := &hermesSourceFixture{
		s:         hermesSource{Version: "1.2.3", Commit: strings.Repeat("a", 40), Files: map[string]hermesSourceFile{}},
		data:      map[string][]byte{"README.md": []byte("hello\n"), "pkg/main.py": []byte("fixture only\n"), "pkg.txt": []byte("sibling\n"), "run.py": {}},
		responses: map[string][]byte{},
	}
	for name, data := range f.data {
		mode := int64(100644)
		if name == "run.py" {
			mode = 100755
		}
		f.s.Files[name] = hermesSourceFile{Mode: mode, Size: int64(len(data)), SHA: hermesTestHash("blob", data)}
	}
	sub := hermesTestTree("100644 main.py", f.s.Files["pkg/main.py"].SHA)
	// pkg.txt sorts before pkg/ in Git, despite the directory's shorter name.
	f.s.Tree = hermesTestTree("100644 README.md", f.s.Files["README.md"].SHA, "100644 pkg.txt", f.s.Files["pkg.txt"].SHA, "40000 pkg", sub, "100755 run.py", f.s.Files["run.py"].SHA)
	f.responses[hermesSourceAPI+"releases/latest"] = []byte(`{"tag_name":"v1.2.3","draft":false,"prerelease":false,"target_commitish":"ignored"}`)
	f.responses[hermesSourceAPI+"releases/tags/v1.2.3"] = f.responses[hermesSourceAPI+"releases/latest"]
	f.responses[hermesSourceAPI+"git/ref/tags/v1.2.3"] = hermesTestJSON(t, map[string]any{"ref": "refs/tags/v1.2.3", "object": map[string]string{"type": "commit", "sha": f.s.Commit}})
	f.responses[hermesSourceAPI+"git/commits/"+f.s.Commit] = hermesTestJSON(t, map[string]any{"sha": f.s.Commit, "tree": map[string]string{"sha": f.s.Tree}})
	rows := []map[string]any{{"path": "pkg", "type": "tree", "mode": "040000", "sha": sub}}
	for _, name := range []string{"README.md", "pkg/main.py", "pkg.txt", "run.py"} {
		e := f.s.Files[name]
		rows = append(rows, map[string]any{"path": name, "type": "blob", "mode": fmt.Sprint(e.Mode), "sha": e.SHA, "size": e.Size})
	}
	f.responses[hermesSourceAPI+"git/trees/"+f.s.Tree+"?recursive=1"] = hermesTestJSON(t, map[string]any{"sha": f.s.Tree, "truncated": false, "tree": rows})
	f.b = &backend{client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		f.calls = append(f.calls, r.URL.String())
		if err := r.Context().Err(); err != nil {
			return nil, err
		}
		data, ok := f.responses[r.URL.String()]
		if !ok {
			return nil, fmt.Errorf("unexpected fixture request: %s", r.URL)
		}
		if r.Header.Get("Accept-Encoding") != "identity" {
			t.Error("missing identity encoding")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), ContentLength: int64(len(data)), Body: io.NopCloser(bytes.NewReader(data)), Request: r}, nil
	})}}
	return f
}

type hermesTarEntry struct {
	h    tar.Header
	data []byte
}

func (f *hermesSourceFixture) archiveEntries() []hermesTarEntry {
	prefix := "hermes-agent-" + f.s.Commit + "/"
	es := []hermesTarEntry{{h: tar.Header{Name: prefix, Typeflag: tar.TypeDir, Mode: 0755}}, {h: tar.Header{Name: prefix + "pkg/", Typeflag: tar.TypeDir, Mode: 0755}}}
	for _, name := range []string{"README.md", "pkg/main.py", "pkg.txt", "run.py"} {
		mode := int64(0644)
		if f.s.Files[name].Mode == 100755 {
			mode = 0755
		}
		es = append(es, hermesTarEntry{h: tar.Header{Name: prefix + name, Typeflag: tar.TypeReg, Mode: mode, Size: int64(len(f.data[name]))}, data: f.data[name]})
	}
	return es
}

func hermesTestTar(t *testing.T, es []hermesTarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range es {
		if err := tw.WriteHeader(&e.h); err != nil {
			t.Fatal(err)
		}
		if len(e.data) > 0 {
			if _, err := tw.Write(e.data); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func hermesTestGzip(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func hermesTestDest(t *testing.T) string {
	t.Helper()
	dest := t.TempDir()
	if err := os.Chmod(dest, 0700); err != nil {
		t.Fatal(err)
	}
	return dest
}

func (f *hermesSourceFixture) download(t *testing.T, es []hermesTarEntry, dest string) error {
	t.Helper()
	f.responses[hermesSourceArchive+f.s.Commit] = hermesTestGzip(t, hermesTestTar(t, es))
	return f.b.downloadHermesSource(context.Background(), f.s, dest)
}

func TestHermesSourcePositive(t *testing.T) {
	for _, version := range []string{"latest", "1.2.3"} {
		t.Run(version, func(t *testing.T) {
			f := newHermesSourceFixture(t)
			s, err := f.b.resolveHermesSource(context.Background(), version)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(hermesTestJSON(t, s), hermesTestJSON(t, f.s)) {
				t.Fatal("source identity differs")
			}
			dest := hermesTestDest(t)
			if err = f.download(t, f.archiveEntries(), dest); err != nil {
				t.Fatal(err)
			}
			for name, data := range f.data {
				got, err := os.ReadFile(filepath.Join(dest, name))
				if err != nil || !bytes.Equal(got, data) {
					t.Fatalf("%s: %v", name, err)
				}
			}
			if err = verifyHermesSource(context.Background(), s, dest); err != nil {
				t.Fatal(err)
			}
			if err = verifyHermesTrackedSource(context.Background(), s, dest); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestHermesSourceGitExporterHeaders(t *testing.T) {
	f := newHermesSourceFixture(t)
	es := f.archiveEntries()
	for i := range es {
		es[i].h.Mode |= 0020
	}
	es = append([]hermesTarEntry{{h: tar.Header{Name: "pax_global_header", Typeflag: tar.TypeXGlobalHeader, Format: tar.FormatPAX, PAXRecords: map[string]string{"comment": f.s.Commit}}}}, es...)
	if err := f.download(t, es, hermesTestDest(t)); err != nil {
		t.Fatal(err)
	}
}

func TestHermesSourcePowerShellArchiveNormalization(t *testing.T) {
	for _, kind := range []string{"crlf", "tampered", "too-large", "wrong-extension", "lone-cr", "unchanged-crlf-blob"} {
		t.Run(kind, func(t *testing.T) {
			f := newHermesSourceFixture(t)
			es := f.archiveEntries()
			name := "activate.ps1"
			if kind == "wrong-extension" {
				name = "activate.py"
			}
			entry := f.s.Files["README.md"]
			delete(f.s.Files, "README.md")
			es[2].h.Name = "hermes-agent-" + f.s.Commit + "/" + name
			es[2].data = []byte("hello\r\n")
			if kind == "tampered" {
				es[2].data = []byte("HELLO\r\n")
			}
			if kind == "too-large" {
				es[2].data = bytes.Repeat([]byte("\r"), 13)
			}
			if kind == "lone-cr" {
				es[2].data = []byte("hello\r\r")
			}
			if kind == "unchanged-crlf-blob" {
				entry.Size = int64(len(es[2].data))
				entry.SHA = hermesTestHash("blob", es[2].data)
			}
			f.s.Files[name] = entry
			es[2].h.Size = int64(len(es[2].data))
			trees, err := hermesSourceTrees(context.Background(), f.s)
			if err != nil {
				t.Fatal(err)
			}
			f.s.Tree = trees["."]
			dest := hermesTestDest(t)
			err = f.download(t, es, dest)
			good := kind == "crlf" || kind == "unchanged-crlf-blob"
			if (err == nil) != good {
				t.Fatalf("normalization result: %v", err)
			}
			if good {
				data, err := os.ReadFile(filepath.Join(dest, name))
				if err != nil || hermesTestHash("blob", data) != entry.SHA {
					t.Fatal("not canonical Git bytes", err)
				}
			}
		})
	}
}

func TestHermesSourceAnnotatedTags(t *testing.T) {
	for _, kind := range []string{"valid", "cycle", "wrong-object", "non-commit", "depth"} {
		t.Run(kind, func(t *testing.T) {
			f := newHermesSourceFixture(t)
			first := strings.Repeat("b", 40)
			f.responses[hermesSourceAPI+"git/ref/tags/v1.2.3"] = hermesTestJSON(t, map[string]any{"ref": "refs/tags/v1.2.3", "object": map[string]string{"type": "tag", "sha": first}})
			obj := map[string]string{"type": "commit", "sha": f.s.Commit}
			sha := first
			switch kind {
			case "cycle":
				obj = map[string]string{"type": "tag", "sha": first}
			case "wrong-object":
				sha = f.s.Commit
			case "non-commit":
				obj["type"] = "blob"
			case "depth":
				obj = map[string]string{"type": "tag", "sha": fmt.Sprintf("%040x", 1)}
				for i := 1; i <= 8; i++ {
					id := fmt.Sprintf("%040x", i)
					f.responses[hermesSourceAPI+"git/tags/"+id] = hermesTestJSON(t, map[string]any{"sha": id, "object": map[string]string{"type": "tag", "sha": fmt.Sprintf("%040x", i+1)}})
				}
			}
			f.responses[hermesSourceAPI+"git/tags/"+first] = hermesTestJSON(t, map[string]any{"sha": sha, "object": obj})
			_, err := f.b.resolveHermesSource(context.Background(), "latest")
			if (err == nil) != (kind == "valid") {
				t.Fatalf("unexpected result %v", err)
			}
		})
	}
}

func TestHermesSourceRejectReleases(t *testing.T) {
	for _, raw := range []string{
		`{"tag_name":"v1.2.3","draft":true,"prerelease":false}`,
		`{"tag_name":"v1.2.3","draft":false,"prerelease":true}`,
		`{"tag_name":"v1.2.3","draft":false}`,
		`{"tag_name":"v1.2.3","draft":null,"prerelease":false}`,
		`{"tag_name":"v1.2.3-rc1","draft":false,"prerelease":false}`,
		`{"tag_name":"1.2.3","draft":false,"prerelease":false}`,
		`{"tag_name":"v1.2.3","tag_name":"v1.2.3","draft":false,"prerelease":false}`,
		`{"tag_name":"v1.2.3","draft":false,"prerelease":false`,
		`null`, `[]`, `{} {}`,
	} {
		t.Run(raw, func(t *testing.T) {
			f := newHermesSourceFixture(t)
			f.responses[hermesSourceAPI+"releases/latest"] = []byte(raw)
			s, err := f.b.resolveHermesSource(context.Background(), "latest")
			if err == nil || s.Files != nil {
				t.Fatalf("release accepted: %+v %v", s, err)
			}
		})
	}
	f := newHermesSourceFixture(t)
	for _, v := range []string{"main", "v1.2.3", "../latest", "1.2", "1.2.3-rc1"} {
		if _, err := f.b.resolveHermesSource(context.Background(), v); err == nil {
			t.Fatalf("accepted %s", v)
		}
	}
	if len(f.calls) != 0 {
		t.Fatal("invalid selector made network calls")
	}
	f.responses[hermesSourceAPI+"releases/tags/v1.2.3"] = []byte(`{"tag_name":"v1.2.4","draft":false,"prerelease":false}`)
	if _, err := f.b.resolveHermesSource(context.Background(), "1.2.3"); err == nil {
		t.Fatal("exact release mismatch accepted")
	}
}

func TestHermesSourceRejectTreeMetadata(t *testing.T) {
	for _, kind := range []string{"truncated", "missing-truncated", "duplicate", "symlink", "submodule", "missing-ancestor", "wrong-root", "wrong-subtree", "wrong-blob", "no-size", "negative-size", "oversize", "empty", "traversal", "bad-mode", "truncated-json"} {
		t.Run(kind, func(t *testing.T) {
			f := newHermesSourceFixture(t)
			key := hermesSourceAPI + "git/trees/" + f.s.Tree + "?recursive=1"
			var obj map[string]any
			if err := json.Unmarshal(f.responses[key], &obj); err != nil {
				t.Fatal(err)
			}
			rows := obj["tree"].([]any)
			row := rows[1].(map[string]any)
			switch kind {
			case "truncated":
				obj["truncated"] = true
			case "missing-truncated":
				delete(obj, "truncated")
			case "duplicate":
				obj["tree"] = append(rows, rows[1])
			case "symlink":
				row["mode"] = "120000"
			case "submodule":
				row["mode"] = "160000"
				row["type"] = "commit"
			case "missing-ancestor":
				obj["tree"] = rows[1:]
			case "wrong-root":
				obj["sha"] = strings.Repeat("d", 40)
			case "wrong-subtree":
				rows[0].(map[string]any)["sha"] = strings.Repeat("d", 40)
			case "wrong-blob":
				row["sha"] = strings.Repeat("d", 40)
			case "no-size":
				delete(row, "size")
			case "negative-size":
				row["size"] = -1
			case "oversize":
				row["size"] = hermesSourceMaxFile + 1
			case "empty":
				obj["tree"] = []any{}
			case "traversal":
				row["path"] = "../bad"
			case "bad-mode":
				row["mode"] = "100600"
			}
			f.responses[key] = hermesTestJSON(t, obj)
			if kind == "truncated-json" {
				f.responses[key] = f.responses[key][:len(f.responses[key])-1]
			}
			if _, err := f.b.resolveHermesSource(context.Background(), "latest"); err == nil {
				t.Fatal("unsafe metadata accepted")
			}
		})
	}
}

func TestHermesSourceRejectArchiveEntries(t *testing.T) {
	for _, kind := range []string{"duplicate", "duplicate-directory", "traversal", "absolute", "backslash", "wrong-prefix", "extra", "extra-directory", "hash", "size", "missing", "mode", "setuid", "symlink", "hardlink", "fifo", "file-ancestor", "pax", "global", "no-root"} {
		t.Run(kind, func(t *testing.T) {
			f := newHermesSourceFixture(t)
			es := f.archiveEntries()
			switch kind {
			case "duplicate":
				es = append(es, es[2])
			case "duplicate-directory":
				es = append(es, es[1])
			case "traversal":
				es[2].h.Name = "hermes-agent-" + f.s.Commit + "/../outside"
			case "absolute":
				es[2].h.Name = "/outside"
			case "backslash":
				es[2].h.Name = "hermes-agent-" + f.s.Commit + "/..\\outside"
			case "wrong-prefix":
				es[2].h.Name = "hermes-agent-main/README.md"
			case "extra":
				extra := es[2]
				extra.h.Name += ".extra"
				es = append(es, extra)
			case "extra-directory":
				extra := es[1]
				extra.h.Name += "extra/"
				es = append(es, extra)
			case "hash":
				es[2].data = []byte("HELLO\n")
			case "size":
				es[2].data = []byte("short")
				es[2].h.Size = 5
			case "missing":
				es = es[:len(es)-1]
			case "mode":
				es[2].h.Mode = 0755
			case "setuid":
				es[2].h.Mode = 04755
			case "symlink":
				es[2].h.Typeflag = tar.TypeSymlink
				es[2].h.Size = 0
				es[2].h.Linkname = "README.md"
				es[2].data = nil
			case "hardlink":
				es[2].h.Typeflag = tar.TypeLink
				es[2].h.Size = 0
				es[2].h.Linkname = es[3].h.Name
				es[2].data = nil
			case "fifo":
				es[2].h.Typeflag = tar.TypeFifo
				es[2].h.Size = 0
				es[2].data = nil
			case "file-ancestor":
				es[2].h.Name = es[1].h.Name[:len(es[1].h.Name)-1]
			case "pax":
				es[2].h.Format = tar.FormatPAX
				es[2].h.PAXRecords = map[string]string{"SCHILY.xattr.user.test": "value"}
			case "global":
				es = append([]hermesTarEntry{{h: tar.Header{Name: "pax_global_header", Typeflag: tar.TypeXGlobalHeader, Format: tar.FormatPAX, PAXRecords: map[string]string{"comment": "wrong"}}}}, es...)
			case "no-root":
				es = es[1:]
			}
			if err := f.download(t, es, hermesTestDest(t)); err == nil {
				t.Fatal("unsafe archive accepted")
			}
		})
	}
}

func TestHermesSourceRejectArchiveTrailers(t *testing.T) {
	for _, kind := range []string{"missing-end", "single-end", "tar-trailer", "gzip-trailer", "gzip-member", "gzip-crc", "gzip-short", "short-file"} {
		t.Run(kind, func(t *testing.T) {
			f := newHermesSourceFixture(t)
			raw := hermesTestTar(t, f.archiveEntries())
			switch kind {
			case "missing-end":
				raw = raw[:len(raw)-1024]
			case "single-end":
				raw = raw[:len(raw)-512]
			case "tar-trailer":
				raw = append(raw, []byte("not padding")...)
			case "short-file":
				raw = raw[:600]
			}
			data := hermesTestGzip(t, raw)
			switch kind {
			case "gzip-trailer":
				data = append(data, 0)
			case "gzip-member":
				data = append(data, hermesTestGzip(t, nil)...)
			case "gzip-crc":
				data[len(data)-8] ^= 0xff
			case "gzip-short":
				data = data[:len(data)-1]
			}
			f.responses[hermesSourceArchive+f.s.Commit] = data
			if err := f.b.downloadHermesSource(context.Background(), f.s, hermesTestDest(t)); err == nil {
				t.Fatal("bad trailer accepted")
			}
		})
	}
}

func TestHermesSourceStrictAndTrackedVerification(t *testing.T) {
	f := newHermesSourceFixture(t)
	dest := hermesTestDest(t)
	if err := f.download(t, f.archiveEntries(), dest); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".hermes/state", "__pycache__/cache", "launchers/hermes"} {
		p := filepath.Join(dest, name)
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("PM generated"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := verifyHermesSource(context.Background(), f.s, dest); err == nil {
		t.Fatal("strict verification accepted extras")
	}
	if err := verifyHermesTrackedSource(context.Background(), f.s, dest); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "README.md"), []byte("HELLO\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := verifyHermesTrackedSource(context.Background(), f.s, dest); err == nil {
		t.Fatal("tracked change accepted")
	}
}

func TestHermesSourceRejectLocalMutation(t *testing.T) {
	for _, kind := range []string{"missing", "hash", "mode", "hardlink", "symlink", "ancestor", "extra", "directory-mode"} {
		t.Run(kind, func(t *testing.T) {
			f := newHermesSourceFixture(t)
			dest := hermesTestDest(t)
			if err := f.download(t, f.archiveEntries(), dest); err != nil {
				t.Fatal(err)
			}
			name := filepath.Join(dest, "README.md")
			var err error
			switch kind {
			case "missing":
				err = os.Remove(name)
			case "hash":
				err = os.WriteFile(name, []byte("HELLO\n"), 0644)
			case "mode":
				err = os.Chmod(name, 0755)
			case "hardlink":
				err = os.Link(name, filepath.Join(dest, "alias"))
			case "symlink":
				if err = os.Remove(name); err == nil {
					err = os.Symlink("pkg.txt", name)
				}
			case "ancestor":
				if err = os.Rename(filepath.Join(dest, "pkg"), filepath.Join(dest, "moved")); err == nil {
					err = os.Symlink("moved", filepath.Join(dest, "pkg"))
				}
			case "extra":
				err = os.WriteFile(filepath.Join(dest, "extra"), nil, 0600)
			case "directory-mode":
				err = os.Chmod(filepath.Join(dest, "pkg"), 0755)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = verifyHermesSource(context.Background(), f.s, dest); err == nil {
				t.Fatal("strict mutation accepted")
			}
			err = verifyHermesTrackedSource(context.Background(), f.s, dest)
			if (err == nil) != (kind == "extra") {
				t.Fatalf("tracked mutation result: %v", err)
			}
		})
	}
}

func TestHermesSourceDestinationGuards(t *testing.T) {
	for _, kind := range []string{"nonempty", "mode", "symlink", "changed", "late-entry", "late-link"} {
		t.Run(kind, func(t *testing.T) {
			f := newHermesSourceFixture(t)
			dest := hermesTestDest(t)
			outside := hermesTestDest(t)
			var err error
			switch kind {
			case "nonempty":
				err = os.WriteFile(filepath.Join(dest, "keep"), []byte("keep"), 0600)
			case "mode":
				err = os.Chmod(dest, 0755)
			case "symlink":
				dest = filepath.Join(dest, "link")
				err = os.Symlink(outside, dest)
			default:
				base := f.b.client.Transport
				f.b.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
					switch kind {
					case "changed":
						if err := os.Rename(dest, dest+".old"); err != nil {
							return nil, err
						}
						t.Cleanup(func() { os.RemoveAll(dest + ".old") })
						if err := os.Mkdir(dest, 0700); err != nil {
							return nil, err
						}
					case "late-entry":
						if err := os.WriteFile(filepath.Join(dest, "keep"), nil, 0600); err != nil {
							return nil, err
						}
					case "late-link":
						if err := os.Remove(dest); err != nil {
							return nil, err
						}
						if err := os.Symlink(outside, dest); err != nil {
							return nil, err
						}
					}
					return base.RoundTrip(r)
				})
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = f.download(t, f.archiveEntries(), dest); err == nil {
				t.Fatal("unsafe destination accepted")
			}
			es, err := os.ReadDir(outside)
			if err != nil || len(es) != 0 {
				t.Fatal("outside destination modified", err)
			}
		})
	}
}

type hermesCancelReader struct {
	r      io.Reader
	cancel context.CancelFunc
	atEOF  bool
}

func (r *hermesCancelReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	if !r.atEOF || err == io.EOF {
		r.cancel()
	}
	if r.atEOF && err == io.EOF {
		return n, context.Canceled
	}
	return n, err
}

func TestHermesSourceCancellation(t *testing.T) {
	f := newHermesSourceFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.b.resolveHermesSource(ctx, "latest"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := f.b.downloadHermesSource(ctx, f.s, hermesTestDest(t)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := verifyHermesSource(ctx, f.s, hermesTestDest(t)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	data := hermesTestGzip(t, hermesTestTar(t, f.archiveEntries()))
	f.b.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), ContentLength: -1, Body: io.NopCloser(&hermesCancelReader{r: bytes.NewReader(data), cancel: cancel})}, nil
	})
	if err := f.b.downloadHermesSource(ctx, f.s, hermesTestDest(t)); !errors.Is(err, context.Canceled) {
		t.Fatalf("midstream cancellation: %v", err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	f.b.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), ContentLength: -1, Body: io.NopCloser(&hermesCancelReader{r: bytes.NewReader(data), cancel: cancel, atEOF: true})}, nil
	})
	if err := f.b.downloadHermesSource(ctx, f.s, hermesTestDest(t)); !errors.Is(err, context.Canceled) {
		t.Fatalf("trailer cancellation: %v", err)
	}
}

func TestHermesSourceFetchGuards(t *testing.T) {
	f := newHermesSourceFixture(t)
	consume := func(r io.Reader) error { _, err := io.Copy(io.Discard, r); return err }
	for _, u := range []string{"http://api.github.com/repos/NousResearch/hermes-agent/releases/latest", hermesSourceAPI + "../other", hermesSourceAPI + "releases/latest?x=1", hermesSourceArchive + "main", "https://api.github.com.evil/repos/NousResearch/hermes-agent/releases/latest", hermesSourceAPI + "releases/latest#fragment", "https://user@api.github.com/repos/NousResearch/hermes-agent/releases/latest"} {
		if err := f.b.fetchHermesSource(context.Background(), u, 100, consume); err == nil {
			t.Fatal("unsafe URL accepted", u)
		}
	}
	if len(f.calls) != 0 {
		t.Fatal("unsafe URL made request")
	}
	for _, kind := range []string{"redirect", "same-origin-redirect", "status", "encoding", "declared-size", "actual-size", "short", "empty"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			f.b.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				resp := &http.Response{StatusCode: 200, Header: make(http.Header), ContentLength: -1, Body: io.NopCloser(strings.NewReader("fixture")), Request: r}
				switch kind {
				case "redirect":
					resp.StatusCode = 302
					resp.Header.Set("Location", "https://evil.invalid/payload")
				case "same-origin-redirect":
					resp.StatusCode = 302
					resp.Header.Set("Location", hermesSourceAPI+"releases/latest")
				case "status":
					resp.StatusCode = 404
				case "encoding":
					resp.Header.Set("Content-Encoding", "gzip")
				case "declared-size":
					resp.ContentLength = 1000
				case "short":
					resp.ContentLength = 8
				case "empty":
					resp.Body = io.NopCloser(strings.NewReader(""))
				}
				return resp, nil
			})
			limit := int64(100)
			if kind == "actual-size" {
				limit = 3
			}
			if err := f.b.fetchHermesSource(context.Background(), hermesSourceAPI+"releases/latest", limit, consume); err == nil {
				t.Fatal("unsafe response accepted")
			}
			if calls != 1 {
				t.Fatalf("redirect escaped: %d requests", calls)
			}
		})
	}
}

func TestHermesSourceInventoryBounds(t *testing.T) {
	f := newHermesSourceFixture(t)
	for _, name := range []string{"../bad", "/absolute", "a//b", "a/./b", "a\\b", "a:b", "a\nb", ".git/config", "a/.GiT/config", strings.Repeat("x", 256), strings.Repeat("a/", 34) + "b"} {
		if hermesSourcePath(name) {
			t.Fatalf("unsafe path accepted: %q", name)
		}
	}
	for _, kind := range []string{"ancestor", "file-size", "payload-size", "entries", "mode", "identity", "tree"} {
		t.Run(kind, func(t *testing.T) {
			s := f.s
			s.Files = map[string]hermesSourceFile{}
			for n, e := range f.s.Files {
				s.Files[n] = e
			}
			e := s.Files["README.md"]
			switch kind {
			case "ancestor":
				s.Files["pkg"] = e
			case "file-size":
				e.Size = hermesSourceMaxFile + 1
				s.Files["README.md"] = e
			case "payload-size":
				for i := 0; i < 9; i++ {
					e.Size = hermesSourceMaxFile
					s.Files[fmt.Sprint(i)] = e
				}
			case "entries":
				for i := 0; i < hermesSourceMaxFiles; i++ {
					s.Files[fmt.Sprint(i)] = e
				}
			case "mode":
				e.Mode = 0100644
				s.Files["README.md"] = e
			case "identity":
				s.Commit = "main"
			case "tree":
				s.Tree = strings.Repeat("d", 40)
			}
			if _, err := s.validate(context.Background()); err == nil {
				t.Fatal("invalid inventory accepted")
			}
		})
	}
	if got := hermesObjectHash("blob", nil); got != "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391" {
		t.Fatal(got)
	}
	if got := hermesObjectHash("tree", nil); got != "4b825dc642cb6eb9a060e54bf8d69288fbee4904" {
		t.Fatal(got)
	}
}

// Explicit opt-in: fetch and verify source in disposable storage, never run it.
func TestHermesSourceOfficialArchive(t *testing.T) {
	if os.Getenv("ATM_TEST_AI_HERMES_SOURCE") != "1" {
		t.Skip("set ATM_TEST_AI_HERMES_SOURCE=1 for official source extraction without execution")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Minute)
	defer cancel()
	b := &backend{client: &http.Client{}}
	s, err := b.resolveHermesSource(ctx, "0.22.5")
	if err != nil {
		t.Fatal(err)
	}
	if s.Commit != "bf0203a794a07aac3ff764972ab9087baa4e4f4e" || s.Tree != "79cfdac56f6c46036c319172fd39678fb8f0c472" {
		t.Fatalf("official 0.22.5 identity changed: commit=%s tree=%s", s.Commit, s.Tree)
	}
	var size int64
	for _, f := range s.Files {
		size += f.Size
	}
	t.Logf("Resolved %s commit=%s tree=%s files=%d bytes=%d", s.Version, s.Commit, s.Tree, len(s.Files), size)
	dest := hermesTestDest(t)
	if err = b.downloadHermesSource(ctx, s, dest); err != nil {
		t.Fatal(err)
	}
	if err = verifyHermesTrackedSource(ctx, s, dest); err != nil {
		t.Fatal(err)
	}
	t.Log("Official source extracted and strict/tracked verified in temporary storage; no provider execution")
}
