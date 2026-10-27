package coreupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"debug/macho"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/AtomyCloud/atm/internal/install"
)

type paEntry struct {
	name     string
	data     []byte
	mode     fs.FileMode
	typeflag byte
	link     string
	pax      map[string]string
}

func paHash(b []byte) string { return fmt.Sprintf("%x", sha256.Sum256(b)) }

func paBinary(goos, arch string) []byte {
	b := make([]byte, 128)
	switch goos {
	case "linux":
		copy(b, "\x7fELF")
		b[4], b[5], b[6] = 2, 1, 1
		binary.LittleEndian.PutUint16(b[16:], 2)
		machine := uint16(62)
		if arch == "arm64" {
			machine = 183
		}
		binary.LittleEndian.PutUint16(b[18:], machine)
	case "windows":
		copy(b, "MZ")
		binary.LittleEndian.PutUint32(b[60:], 64)
		copy(b[64:], "PE\x00\x00")
		binary.LittleEndian.PutUint16(b[68:], 0x8664)
	case "darwin":
		cpu := macho.CpuAmd64
		if arch == "arm64" {
			cpu = macho.CpuArm64
		}
		binary.LittleEndian.PutUint32(b, macho.Magic64)
		binary.LittleEndian.PutUint32(b[4:], uint32(cpu))
		binary.LittleEndian.PutUint32(b[12:], uint32(macho.TypeExec))
	}
	return b
}

func paFixture(t *testing.T, goos, arch string) (PackageManifest, []paEntry) {
	t.Helper()
	m := PackageManifest{Schema: 1, Scope: PackageScope, Version: "0.0.2", Commit: strings.Repeat("a", 40), OS: goos, Arch: arch, Migration: true}
	ext, executable := ".tar.gz", "atm"
	if goos == "windows" {
		ext, executable = ".zip", "atm.exe"
	}
	m.Source = OfficialSource + "/releases/download/v" + m.Version + "/atm-" + m.Version + "-" + goos + "-" + arch + ext
	entries := []paEntry{
		{name: executable, data: paBinary(goos, arch), mode: 0755},
		{name: "core-api.json", data: []byte(`{"schema":1,"plugin_api":1,"update_protocol":1}`), mode: 0644},
		{name: "LICENSE", data: []byte("license\n"), mode: 0644},
		{name: "README.md", data: []byte("readme\n"), mode: 0644},
		{name: "plugins/demo/plugin.json", data: []byte(`{"id":"demo"}`), mode: 0644},
		{name: "lib/helper.sh", data: []byte("exit 0\n"), mode: 0755},
		{name: "lang/en-us.lang", data: []byte("KEY=value\n"), mode: 0644},
	}
	return m, paIndex(t, &m, entries, nil)
}

func paIndex(t *testing.T, m *PackageManifest, entries []paEntry, mutate func(*packageRelease)) []paEntry {
	t.Helper()
	r := packageRelease{Schema: m.Schema, Version: m.Version, Commit: m.Commit, OS: m.OS, Arch: m.Arch, Migration: m.Migration, Files: map[string]string{}}
	payload := make([]paEntry, 0, len(entries)+1)
	for _, e := range entries {
		if e.name == "release.json" {
			continue
		}
		payload = append(payload, e)
		if !e.mode.IsDir() {
			r.Files[e.name] = paHash(e.data)
		}
	}
	if mutate != nil {
		mutate(&r)
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	m.ReleaseSHA256 = paHash(b)
	return append(payload, paEntry{name: "release.json", data: b, mode: 0644})
}

func paArchive(t *testing.T, m PackageManifest, entries []paEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	prefix := "atm-" + m.Version + "-" + m.OS + "-" + m.Arch
	if m.OS == "windows" {
		z := zip.NewWriter(&buf)
		for _, e := range entries {
			h := &zip.FileHeader{Name: prefix + "/" + e.name, Method: zip.Deflate}
			h.SetMode(e.mode)
			w, err := z.CreateHeader(h)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = w.Write(e.data); err != nil {
				t.Fatal(err)
			}
		}
		if err := z.Close(); err != nil {
			t.Fatal(err)
		}
	} else {
		gz := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gz)
		for _, e := range entries {
			h := &tar.Header{Name: prefix + "/" + e.name, Size: int64(len(e.data)), Mode: int64(e.mode.Perm()), Typeflag: e.typeflag, Linkname: e.link, PAXRecords: e.pax}
			if e.mode.IsDir() {
				h.Typeflag = tar.TypeDir
			}
			if e.mode&os.ModeSetuid != 0 {
				h.Mode |= 04000
			}
			if e.mode&os.ModeSetgid != 0 {
				h.Mode |= 02000
			}
			if e.mode&os.ModeSticky != 0 {
				h.Mode |= 01000
			}
			if err := tw.WriteHeader(h); err != nil {
				t.Fatal(err)
			}
			if _, err := tw.Write(e.data); err != nil {
				t.Fatal(err)
			}
		}
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}
		if err := gz.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return buf.Bytes()
}

func paWrite(t *testing.T, m *PackageManifest, raw []byte) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(dir, "input.archive")
	if err = os.WriteFile(name, raw, 0600); err != nil {
		t.Fatal(err)
	}
	m.Archive = install.File{SHA256: paHash(raw), Size: int64(len(raw)), Mode: 0644}
	return name
}

func paReject(t *testing.T, m PackageManifest, raw []byte, reason string) {
	t.Helper()
	name := paWrite(t, &m, raw)
	got, err := InspectPackage(context.Background(), name, m)
	if err == nil || got != nil {
		t.Fatalf("accepted %s: files=%v err=%v", reason, got, err)
	}
}

func TestInspectPackageTargets(t *testing.T) {
	for _, target := range []string{"linux/amd64", "linux/arm64", "windows/amd64", "darwin/amd64", "darwin/arm64"} {
		t.Run(target, func(t *testing.T) {
			goos, arch, _ := strings.Cut(target, "/")
			m, entries := paFixture(t, goos, arch)
			raw := paArchive(t, m, entries)
			name := paWrite(t, &m, raw)
			before, err := os.ReadDir(filepath.Dir(name))
			if err != nil {
				t.Fatal(err)
			}
			got, err := InspectPackage(context.Background(), name, m)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(entries) {
				t.Fatalf("files = %v", got)
			}
			for _, e := range entries {
				want := install.File{SHA256: paHash(e.data), Size: int64(len(e.data)), Mode: uint32(e.mode)}
				if got[e.name] != want {
					t.Errorf("%s: got %+v want %+v", e.name, got[e.name], want)
				}
			}
			after, err := os.ReadDir(filepath.Dir(name))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatal("inspection changed directory")
			}
			unchanged, err := os.ReadFile(name)
			if err != nil || !bytes.Equal(raw, unchanged) {
				t.Fatal("inspection changed archive")
			}
			entries[0].data = paBinary("linux", "amd64")
			if goos == "linux" {
				entries[0].data = paBinary("windows", "amd64")
			}
			entries = paIndex(t, &m, entries, nil)
			paReject(t, m, paArchive(t, m, entries), "wrong target binary")
		})
	}
}

func TestInspectPackagePaths(t *testing.T) {
	bad := []string{"../escape", "/absolute", "plugins/../../escape", "plugins//double", "plugins/./dot", "plugins\\escape", "plugins/C:drive", "plugins/x/CON", "plugins/x/lpt1.txt", "plugins/x/trailing.", "plugins/x/trailing ", "plugins/x/a\nname", "plugins/x/colon:stream", "plugins/.secret", "plugins/x/.env", "plugins/tests/data", "plugins/__pycache__/data", "internal-docs/MEMORY.md", ".codex/config.toml", "config.json", "unknown.txt", "plugins/" + strings.Repeat("x", 240), "plugins/" + strings.Repeat("a/", 16) + "x"}
	for _, goos := range []string{"linux", "windows"} {
		for _, path := range bad {
			t.Run(goos+"/"+path, func(t *testing.T) {
				m, entries := paFixture(t, goos, "amd64")
				entries = append(entries, paEntry{name: path, data: []byte("unexpected"), mode: 0644})
				entries = paIndex(t, &m, entries, nil)
				paReject(t, m, paArchive(t, m, entries), path)
			})
		}
		for _, name := range []string{"LICENSE", "license", "plugins/Demo/extra"} {
			t.Run(goos+"/collision/"+name, func(t *testing.T) {
				m, entries := paFixture(t, goos, "amd64")
				entries = append(entries, paEntry{name: name, data: []byte("duplicate"), mode: 0644})
				entries = paIndex(t, &m, entries, nil)
				paReject(t, m, paArchive(t, m, entries), "duplicate or case collision")
			})
		}
		for _, firstDir := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/type/%v", goos, firstDir), func(t *testing.T) {
				m, entries := paFixture(t, goos, "amd64")
				extra := paEntry{name: "plugins/demo", data: []byte("not directory"), mode: 0644}
				if firstDir {
					entries = append(entries, extra)
				} else {
					entries = append([]paEntry{extra}, entries...)
				}
				entries = paIndex(t, &m, entries, nil)
				paReject(t, m, paArchive(t, m, entries), "file/directory collision")
			})
		}
	}
}

func TestInspectPackageSpecials(t *testing.T) {
	for _, typ := range []byte{tar.TypeSymlink, tar.TypeLink, tar.TypeFifo, tar.TypeChar, tar.TypeBlock} {
		t.Run(fmt.Sprintf("tar/%d", typ), func(t *testing.T) {
			m, entries := paFixture(t, "linux", "amd64")
			e := paEntry{name: "plugins/demo/link", mode: 0644, typeflag: typ}
			if typ == tar.TypeSymlink || typ == tar.TypeLink {
				e.link = "../../outside"
			}
			entries = paIndex(t, &m, append(entries, e), nil)
			paReject(t, m, paArchive(t, m, entries), "tar special")
		})
	}
	for _, mode := range []fs.FileMode{os.ModeSymlink, os.ModeNamedPipe, os.ModeDevice, os.ModeSocket, os.ModeSetuid, os.ModeSetgid, os.ModeSticky} {
		t.Run(fmt.Sprintf("zip/%d", mode), func(t *testing.T) {
			m, entries := paFixture(t, "windows", "amd64")
			entries = paIndex(t, &m, append(entries, paEntry{name: "plugins/demo/special", mode: mode | 0644}), nil)
			paReject(t, m, paArchive(t, m, entries), "zip special/mode")
		})
	}
	for _, mode := range []fs.FileMode{os.ModeSetuid, os.ModeSetgid, os.ModeSticky} {
		m, entries := paFixture(t, "linux", "amd64")
		entries[0].mode |= mode
		paReject(t, m, paArchive(t, m, entries), "tar special mode")
	}
	m, entries := paFixture(t, "linux", "amd64")
	entries[0].pax = map[string]string{"comment": "extension"}
	paReject(t, m, paArchive(t, m, entries), "PAX")
}

func TestInspectPackageIndex(t *testing.T) {
	mutations := map[string]func(*packageRelease){
		"schema":    func(r *packageRelease) { r.Schema++ },
		"version":   func(r *packageRelease) { r.Version = "0.0.3" },
		"commit":    func(r *packageRelease) { r.Commit = strings.Repeat("b", 40) },
		"os":        func(r *packageRelease) { r.OS = "windows" },
		"arch":      func(r *packageRelease) { r.Arch = "arm64" },
		"migration": func(r *packageRelease) { r.Migration = false },
		"hash":      func(r *packageRelease) { r.Files["LICENSE"] = strings.Repeat("0", 64) },
		"unlisted":  func(r *packageRelease) { delete(r.Files, "LICENSE") },
		"missing":   func(r *packageRelease) { r.Files["lib/missing"] = paHash(nil) },
		"self":      func(r *packageRelease) { r.Files["release.json"] = paHash(nil) },
		"empty":     func(r *packageRelease) { r.Files = nil },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			m, entries := paFixture(t, "linux", "amd64")
			entries = paIndex(t, &m, entries, mutate)
			paReject(t, m, paArchive(t, m, entries), name)
		})
	}
	for _, missing := range []string{"atm", "core-api.json", "LICENSE", "README.md"} {
		t.Run("required/"+missing, func(t *testing.T) {
			m, entries := paFixture(t, "linux", "amd64")
			var kept []paEntry
			for _, e := range entries {
				if e.name != missing {
					kept = append(kept, e)
				}
			}
			kept = paIndex(t, &m, kept, nil)
			paReject(t, m, paArchive(t, m, kept), "missing required file")
		})
	}
	for _, mutation := range []string{"duplicate", "case-alias", "trailing", "unknown", "bad-utf8", "duplicate-path"} {
		t.Run("json/"+mutation, func(t *testing.T) {
			m, entries := paFixture(t, "linux", "amd64")
			e := &entries[len(entries)-1]
			s := string(e.data)
			switch mutation {
			case "duplicate":
				s = strings.Replace(s, `"schema":1`, `"schema":1,"schema":1`, 1)
			case "case-alias":
				s = strings.Replace(s, `"schema":1`, `"Schema":1`, 1)
			case "trailing":
				s += `{}`
			case "unknown":
				s = strings.Replace(s, `"schema":1`, `"schema":1,"extra":1`, 1)
			case "bad-utf8":
				s = strings.Replace(s, `"version"`, "\xff\"version\"", 1)
			case "duplicate-path":
				s = strings.Replace(s, `"files":{`, `"files":{"LICENSE":"`+paHash([]byte("license\n"))+`",`, 1)
			}
			e.data = []byte(s)
			m.ReleaseSHA256 = paHash(e.data)
			paReject(t, m, paArchive(t, m, entries), mutation)
		})
	}
}

func TestInspectPackageArchiveIdentity(t *testing.T) {
	m, entries := paFixture(t, "linux", "amd64")
	raw := paArchive(t, m, entries)
	name := paWrite(t, &m, raw)
	for _, mutate := range []func(*PackageManifest){
		func(m *PackageManifest) { m.Archive.SHA256 = strings.Repeat("0", 64) },
		func(m *PackageManifest) { m.Archive.Size++ },
		func(m *PackageManifest) { m.Archive.Size-- },
		func(m *PackageManifest) { m.Archive.Size = MaxPackageBytes + 1 },
		func(m *PackageManifest) { m.ReleaseSHA256 = strings.Repeat("0", 64) },
		func(m *PackageManifest) { m.Scope = Scope },
		func(m *PackageManifest) { m.Source = "https://example.invalid/package" },
	} {
		bad := m
		mutate(&bad)
		if got, err := InspectPackage(context.Background(), name, bad); err == nil || got != nil {
			t.Fatal("accepted bad expected manifest")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := InspectPackage(ctx, name, m); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	if _, err := InspectPackage(context.Background(), filepath.Dir(name), m); err == nil {
		t.Fatal("accepted directory")
	}
	if _, err := InspectPackage(context.Background(), name+"-missing", m); err == nil {
		t.Fatal("accepted missing file")
	}
	link := name + "-link"
	if err := os.Symlink(name, link); err == nil {
		if _, err = InspectPackage(context.Background(), link, m); err == nil {
			t.Fatal("accepted archive symlink")
		}
		parentLink := filepath.Join(filepath.Dir(name), "parent-link")
		if err = os.Symlink(filepath.Dir(name), parentLink); err != nil {
			t.Fatal(err)
		}
		if _, err = InspectPackage(context.Background(), filepath.Join(parentLink, filepath.Base(name)), m); err == nil {
			t.Fatal("accepted linked parent")
		}
	}
	wrong := m
	wrong.Version = "0.0.3"
	paReject(t, m, paArchive(t, wrong, entries), "wrong target prefix")
}

func TestInspectPackageGzipIntegrity(t *testing.T) {
	m, entries := paFixture(t, "linux", "amd64")
	raw := paArchive(t, m, entries)
	for _, n := range []int{1, 4, 8, len(raw) / 2} {
		paReject(t, m, raw[:len(raw)-n], "truncated gzip")
	}
	corrupt := append([]byte(nil), raw...)
	corrupt[len(corrupt)-8] ^= 1
	paReject(t, m, corrupt, "gzip CRC")
	paReject(t, m, append(append([]byte(nil), raw...), raw...), "concatenated gzip")
	paReject(t, m, append(append([]byte(nil), raw...), 'x'), "trailing compressed data")
	gz, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := io.ReadAll(gz)
	if err != nil {
		t.Fatal(err)
	}
	gz.Close()
	var out bytes.Buffer
	w := gzip.NewWriter(&out)
	if _, err = w.Write(append(plain, 'x')); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	paReject(t, m, out.Bytes(), "hidden tar tail")
}

func TestInspectPackageBounds(t *testing.T) {
	m, _ := paFixture(t, "linux", "amd64")
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	h := &tar.Header{Name: "atm-0.0.2-linux-amd64/atm", Size: MaxPackageBytes + 1, Mode: 0755}
	if err := tw.WriteHeader(h); err != nil {
		t.Fatal(err)
	}
	// Intentionally omit the huge body; inspection must reject the declared size.
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	paReject(t, m, buf.Bytes(), "oversized tar declaration")
	x := packageInspector{ctx: context.Background(), prefix: "root", explicit: map[string]string{}, seen: map[string]string{}, files: map[string]install.File{}}
	x.expanded = packageExpandedBytes
	if err := x.entry("root/lib/extra", 0644, 1, strings.NewReader("x")); err == nil {
		t.Fatal("accepted expanded overflow")
	}
	x.entries = packageEntries
	if err := x.entry("root/lib/other", 0644, 0, bytes.NewReader(nil)); err == nil {
		t.Fatal("accepted entry overflow")
	}
	x.entries = 0
	x.expanded = 0
	if err := x.entry("root/release.json", 0644, packageReleaseBytes+1, bytes.NewReader(nil)); err == nil {
		t.Fatal("accepted oversized release")
	}
	for _, goos := range []string{"linux", "windows"} {
		m, entries := paFixture(t, goos, "amd64")
		entries[0].data = []byte("not a binary")
		entries = paIndex(t, &m, entries, nil)
		paReject(t, m, paArchive(t, m, entries), "short binary")
	}
}

func TestInspectPackageDirectories(t *testing.T) {
	for _, goos := range []string{"linux", "windows"} {
		t.Run(goos, func(t *testing.T) {
			m, entries := paFixture(t, goos, "amd64")
			dirs := []paEntry{{name: "", mode: os.ModeDir | 0755}, {name: "plugins/", mode: os.ModeDir | 0755}, {name: "plugins/demo/", mode: os.ModeDir | 0755}}
			// Explicit directories may follow their implicitly created parents.
			entries = append(entries, dirs...)
			name := paWrite(t, &m, paArchive(t, m, entries))
			got, err := InspectPackage(context.Background(), name, m)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(entries)-len(dirs) {
				t.Fatal("directories included in payload files")
			}
			paReject(t, m, paArchive(t, m, append(entries, dirs[0])), "duplicate root directory")
			paReject(t, m, paArchive(t, m, append(entries, dirs[1])), "duplicate runtime directory")
			paReject(t, m, paArchive(t, m, append(entries, paEntry{name: "README.md/", mode: os.ModeDir | 0755})), "root file as directory")
		})
	}
}

func TestInspectPackageZIPIntegrity(t *testing.T) {
	m, entries := paFixture(t, "windows", "amd64")
	raw := paArchive(t, m, entries)
	paReject(t, m, raw[:len(raw)-8], "truncated zip directory")
	central := bytes.Index(raw, []byte("PK\x01\x02"))
	if central < 0 {
		t.Fatal("no ZIP directory")
	}
	for _, test := range []struct {
		name   string
		offset int
		value  uint32
	}{
		{"oversize", 24, uint32(MaxPackageBytes + 1)},
		{"size-mismatch", 24, 1},
		{"crc", 16, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			bad := append([]byte(nil), raw...)
			binary.LittleEndian.PutUint32(bad[central+test.offset:], test.value)
			paReject(t, m, bad, "zip "+test.name)
		})
	}
	extra := make([]paEntry, packageEntries+1)
	for i := range extra {
		extra[i] = paEntry{name: fmt.Sprintf("lib/file%d", i), mode: 0644}
	}
	paReject(t, m, paArchive(t, m, extra), "zip entry count")
}

func TestInspectPackageMachOTablesOutsidePrefix(t *testing.T) {
	m, entries := paFixture(t, "darwin", "arm64")
	const tableOffset = (1 << 20) + 64
	b := make([]byte, tableOffset+1)
	copy(b, entries[0].data)
	binary.LittleEndian.PutUint32(b[16:], 1)
	binary.LittleEndian.PutUint32(b[20:], 24)
	binary.LittleEndian.PutUint32(b[32:], uint32(macho.LoadCmdSymtab))
	binary.LittleEndian.PutUint32(b[36:], 24)
	binary.LittleEndian.PutUint32(b[40:], tableOffset)
	binary.LittleEndian.PutUint32(b[48:], tableOffset)
	binary.LittleEndian.PutUint32(b[52:], 1)
	entries[0].data = b
	entries = paIndex(t, &m, entries, nil)
	name := paWrite(t, &m, paArchive(t, m, entries))
	if _, err := InspectPackage(context.Background(), name, m); err != nil {
		t.Fatal(err)
	}
}

type paCancelReader struct{ cancel context.CancelFunc }

func (r paCancelReader) Read(p []byte) (int, error) {
	r.cancel()
	if len(p) == 0 {
		return 0, nil
	}
	p[0] = 'x'
	return 1, nil
}

func TestInspectPackageReadFailures(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	x := packageInspector{ctx: ctx, prefix: "root", explicit: map[string]string{}, seen: map[string]string{}, files: map[string]install.File{}}
	if err := x.entry("root/lib/data", 0644, 100, paCancelReader{cancel}); !errors.Is(err, context.Canceled) {
		t.Fatalf("mid-read cancellation: %v", err)
	}
	if len(x.files) != 0 {
		t.Fatal("partial entry recorded")
	}
	for _, data := range []string{"short", "oversized content"} {
		x := packageInspector{ctx: context.Background(), prefix: "root", explicit: map[string]string{}, seen: map[string]string{}, files: map[string]install.File{}}
		if err := x.entry("root/lib/data", 0644, 10, strings.NewReader(data)); err == nil {
			t.Fatal("accepted size mismatch")
		}
	}
	r := packageContextReaderAt{ctx, bytes.NewReader([]byte("data"))}
	if _, err := r.ReadAt(make([]byte, 1), 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("readerAt cancellation: %v", err)
	}
	for _, raw := range [][]byte{nil, []byte("not an archive")} {
		for _, goos := range []string{"linux", "windows"} {
			m, _ := paFixture(t, goos, "amd64")
			paReject(t, m, raw, "invalid archive")
		}
	}
}
