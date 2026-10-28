//go:build linux && amd64

package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/AtomyCloud/atm/internal/protocol"
)

func write(t *testing.T, name string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, data, mode); err != nil {
		t.Fatal(err)
	}
}
func newTestBackend(t *testing.T) *backend {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ATM_DRY_RUN", "")
	t.Setenv("JAVA_HOME", filepath.Join(t.TempDir(), "absent"))
	t.Setenv("PATH", t.TempDir())
	req := protocol.Request{Protocol: 1, RequestID: strings.Repeat("a", 32), PluginID: "android_sdk", Operation: "menu", Home: filepath.Join(t.TempDir(), "runtime"), AppsDir: filepath.Join(t.TempDir(), "Apps")}
	b, err := newBackend(req, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func action(b *backend, op string, args ...string) error {
	b.req.Operation = op
	b.req.Arguments = args
	_, err := b.execute(context.Background())
	return err
}
func assertAbsent(t *testing.T, name string) {
	t.Helper()
	if _, err := os.Lstat(name); !os.IsNotExist(err) {
		t.Fatalf("expected absent %s: %v", name, err)
	}
}
func assertContents(t *testing.T, name, want string) {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil || string(data) != want {
		t.Fatalf("%s: %q %v", name, data, err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func testLicense() license {
	return license{ID: "android-sdk-license", Type: "text", Text: "  Test terms\nLine two & conditions.\n "}
}
func testSpec(id string, rev revision) packageSpec {
	dir, _ := packageDir(id)
	typ := "generic:genericDetailsType"
	content := ""
	if strings.HasPrefix(id, "platforms;") {
		typ = "sdk:platformDetailsType"
		content = "<api-level>36</api-level><extension-level>17</extension-level><base-extension>true</base-extension><layoutlib api=\"15\"/>"
	}
	return packageSpec{ID: id, Directory: dir, Name: "Fixture " + id, Type: typ, TypeContent: content, Revision: rev, URL: repositoryBase + strings.ReplaceAll(id, ";", "-") + ".zip", ChecksumType: "sha1", License: testLicense()}
}
func remote(s packageSpec) remotePackage {
	return remotePackage{ID: s.ID, Details: details{s.Type, s.TypeContent}, Revision: s.Revision, Name: s.Name, License: reference{s.License.ID}, Channel: reference{"channel-0"}, Dependencies: s.Dependencies, Archives: []archiveSpec{{OS: "linux", Arch: "x64", JVMBits: 64, Complete: complete{s.Size, checksum{s.ChecksumType, s.Checksum}, strings.TrimPrefix(s.URL, repositoryBase)}}}}
}
func repoBytes(t *testing.T, packages ...remotePackage) []byte {
	t.Helper()
	var buf bytes.Buffer
	buf.WriteString(`<sdk:sdk-repository xmlns:sdk="http://schemas.android.com/sdk/android/repo/repository2/03" xmlns:generic="http://schemas.android.com/repository/android/generic/02" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">`)
	data, err := xml.Marshal(testLicense())
	if err != nil {
		t.Fatal(err)
	}
	buf.Write(data)
	for _, p := range packages {
		data, err = xml.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		buf.Write(data)
	}
	buf.WriteString("</sdk:sdk-repository>")
	return buf.Bytes()
}
func elfBytes() []byte {
	data := make([]byte, 64)
	copy(data, []byte{127, 'E', 'L', 'F', 2, 1, 1})
	binary.LittleEndian.PutUint16(data[16:], 2)
	binary.LittleEndian.PutUint16(data[18:], 62)
	binary.LittleEndian.PutUint32(data[20:], 1)
	binary.LittleEndian.PutUint16(data[52:], 64)
	return data
}

type zipItem struct {
	name string
	data []byte
	mode os.FileMode
}

func zipBytes(t *testing.T, items []zipItem) []byte {
	t.Helper()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for _, item := range items {
		h := &zip.FileHeader{Name: item.name, Method: zip.Deflate}
		h.SetMode(item.mode)
		w, err := z.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write(item.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
func payloadItems(t *testing.T, s packageSpec) []zipItem {
	props := "Pkg.Revision=" + s.Revision.String() + "\nPkg.Path=" + s.ID + "\n"
	if strings.HasPrefix(s.ID, "platforms;") {
		props += "AndroidVersion.ApiLevel=" + strings.TrimPrefix(s.ID, "platforms;android-") + "\n"
	}
	out := []zipItem{{"package/source.properties", []byte(props), 0600}}
	executable := ""
	switch {
	case s.ID == toolsID:
		out = append(out, zipItem{"package/bin/sdkmanager", []byte("#!/bin/sh\nexit 99\n"), 0700}, zipItem{"package/lib/sdkmanager-classpath.jar", zipBytes(t, []zipItem{{"META-INF/MANIFEST.MF", []byte("Manifest-Version: 1.0\nClass-Path: repository.jar\n"), 0600}}), 0600})
	case s.ID == "platform-tools":
		executable = "adb"
	case s.ID == "emulator":
		executable = "emulator"
	case strings.HasPrefix(s.ID, "build-tools;"):
		executable = "aapt2"
	case strings.HasPrefix(s.ID, "cmake;"):
		executable = "bin/cmake"
	case strings.HasPrefix(s.ID, "ndk;"):
		executable = "toolchains/llvm/prebuilt/linux-x86_64/bin/clang"
	case strings.HasPrefix(s.ID, "platforms;"):
		out = append(out, zipItem{"package/android.jar", []byte("fixture"), 0600})
	}
	if executable != "" {
		out = append(out, zipItem{"package/" + executable, elfBytes(), 0700})
	}
	return out
}
func fixture(t *testing.T, b *backend, specs []packageSpec, archives map[string][]byte, metadata []byte) []packageSpec {
	t.Helper()
	for i := range specs {
		data := archives[specs[i].ID]
		if data != nil {
			specs[i].Size = int64(len(data))
			specs[i].Checksum = fmt.Sprintf("%x", sha1.Sum(data))
		}
	}
	routes := map[string][]byte{}
	for _, s := range specs {
		routes[strings.TrimPrefix(s.URL, "https://dl.google.com")] = archives[s.ID]
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/android/repository/repository2-3.xml" {
			_, _ = w.Write(metadata)
			return
		}
		data, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)
	target, _ := url.Parse(srv.URL)
	transport := srv.Client().Transport
	b.client = &http.Client{Timeout: 3 * time.Second, Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		clone := req.Clone(req.Context())
		u := *req.URL
		u.Scheme = target.Scheme
		u.Host = target.Host
		clone.URL = &u
		clone.Host = ""
		return transport.RoundTrip(clone)
	})}
	return specs
}
func installFixture(t *testing.T, b *backend, specs ...packageSpec) []packageSpec {
	t.Helper()
	archives := map[string][]byte{}
	for _, s := range specs {
		archives[s.ID] = zipBytes(t, payloadItems(t, s))
	}
	specs = fixture(t, b, specs, archives, nil)
	// Internal lifecycle fixture uses synthetic artifacts, not a production pin
	// override. The external resolver independently enforces Google's fixed tools pin.
	plan := installPlan{1, "install", b.root, specs}
	if err := b.withLock(func() error { return b.install(context.Background(), plan, false) }); err != nil {
		t.Fatal(err)
	}
	return specs
}

func TestStackParityOptionsAndEarlyRefusal(t *testing.T) {
	expected := map[string][]string{"latest": {"platforms;android-36", "build-tools;36.0.0", "cmake;3.31.6", "ndk;29.0.14206865"}, "api36": {"platforms;android-36", "build-tools;36.0.0", "cmake;3.31.6", "ndk;29.0.14206865"}, "api35": {"platforms;android-35", "build-tools;35.0.1", "cmake;3.31.6", "ndk;29.0.14206865"}, "api35_legacy": {"platforms;android-35", "build-tools;35.0.0", "cmake;3.22.1", "ndk;28.2.13676358"}, "api34": {"platforms;android-34", "build-tools;34.0.0", "cmake;3.22.1", "ndk;27.3.13750724"}}
	for stack, want := range expected {
		o, err := parseOptions([]string{"--stack", stack}, false)
		if err != nil || !reflect.DeepEqual(o.ids()[3:], want) {
			t.Fatal(stack, o, err)
		}
	}
	b := newTestBackend(t)
	b.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("network before acknowledgement")
		return nil, nil
	})}
	for _, op := range []string{"install", "update"} {
		for _, args := range [][]string{nil, {"--accept-licenses", "yes"}, {"--stack", "api37"}, {"--accept-licenses"}, {"--accept-licenses", "ABC"}} {
			if err := action(b, op, args...); err == nil {
				t.Fatal(op, args)
			}
		}
	}
	assertAbsent(t, b.root)
}
func TestStableArchitectureDependencyResolution(t *testing.T) {
	platform := testSpec("platforms;android-36", revision{Major: 2})
	platform.Size = 12
	platform.Checksum = strings.Repeat("1", 40)
	platform.Dependencies = []dependency{{Path: "platform-tools", Minimum: revision{Major: 2}}}
	tool := testSpec("platform-tools", revision{Major: 2})
	tool.Size = 12
	tool.Checksum = strings.Repeat("2", 40)
	stable := remote(tool)
	preview := stable
	preview.Revision.Major = 99
	preview.Channel.Ref = "channel-2"
	arm := stable
	arm.Revision.Major = 100
	arm.Archives = append([]archiveSpec(nil), stable.Archives...)
	arm.Archives[0].Arch = "aarch64"
	wrongbits := stable
	wrongbits.Revision.Major = 101
	wrongbits.Archives = append([]archiveSpec(nil), stable.Archives...)
	wrongbits.Archives[0].JVMBits = 32
	repo, err := parseRepository(repoBytes(t, remote(platform), stable, preview, arm, wrongbits))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := repo.resolvePlan([]string{platform.ID}, "/tmp/fixture-sdk", "install")
	if err != nil || len(plan.Packages) != 2 || plan.Packages[0].ID != "platform-tools" || plan.Packages[0].Revision.Major != 2 {
		t.Fatal(plan, err)
	}
	platform.Dependencies[0].Minimum.Major = 3
	repo, err = parseRepository(repoBytes(t, remote(platform), stable))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.resolvePlan([]string{platform.ID}, "/tmp/root", "install"); err == nil {
		t.Fatal("unsatisfied minimum accepted")
	}
	platform.Dependencies[0].Minimum.Major = 2
	tool.Dependencies = []dependency{{Path: platform.ID}}
	repo, err = parseRepository(repoBytes(t, remote(platform), remote(tool)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.resolvePlan([]string{platform.ID}, "/tmp/root", "install"); err == nil {
		t.Fatal("dependency cycle accepted")
	}
}
func TestPinnedToolsAndMetadataAmbiguity(t *testing.T) {
	canonical, err := packageDir(toolsID)
	if err != nil || canonical != strings.ReplaceAll(toolsID, ";", "/") {
		t.Fatal("command-line tools path does not match SDK package ID", canonical, err)
	}
	s := testSpec(toolsID, revision{Major: 20})
	s.URL = repositoryBase + toolsArchive
	s.Size = toolsSize
	s.Checksum = toolsSHA1
	repo, err := parseRepository(repoBytes(t, remote(s)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.resolve(toolsID); err != nil {
		t.Fatal(err)
	}
	s.Checksum = strings.Repeat("0", 40)
	repo, _ = parseRepository(repoBytes(t, remote(s)))
	if _, err = repo.resolve(toolsID); err == nil {
		t.Fatal("changed pin accepted")
	}
	a := testSpec("platform-tools", revision{Major: 1})
	a.Size = 10
	a.Checksum = strings.Repeat("1", 40)
	bb := a
	bb.Checksum = strings.Repeat("2", 40)
	repo, _ = parseRepository(repoBytes(t, remote(a), remote(bb)))
	if _, err = repo.resolve(a.ID); err == nil {
		t.Fatal("ambiguous same revision accepted")
	}
	if _, err = parseRepository([]byte(`<!DOCTYPE sdk [<!ENTITY bad SYSTEM "file:///etc/passwd">]><sdk/>`)); err == nil {
		t.Fatal("DTD accepted")
	}
}
func TestExactLicenseAndLocalMetadata(t *testing.T) {
	s := testSpec("platforms;android-36", revision{Major: 2})
	s.Size = 10
	s.Checksum = strings.Repeat("1", 40)
	repo, err := parseRepository(repoBytes(t, remote(s)))
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := repo.resolve(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.License.Text != testLicense().Text {
		t.Fatal("license whitespace normalized incorrectly")
	}
	if licenseHash(resolved.License.Text) == licenseHash(strings.TrimSpace(resolved.License.Text)) {
		t.Fatal("license hash silently trimmed")
	}
	data, err := localMetadata(resolved)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		XMLName xml.Name
		License license `xml:"license"`
		Local   struct {
			Path     string    `xml:"path,attr"`
			Details  details   `xml:"type-details"`
			Revision revision  `xml:"revision"`
			License  reference `xml:"uses-license"`
		} `xml:"localPackage"`
	}
	if err = xml.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.XMLName.Space != "http://schemas.android.com/repository/android/common/02" || parsed.Local.Path != s.ID || parsed.Local.Details.Type != "sdk:platformDetailsType" || !strings.Contains(parsed.Local.Details.Content, "<api-level>36</api-level>") || parsed.License.Text != s.License.Text || parsed.Local.License.Ref != s.License.ID {
		t.Fatal(string(data), parsed)
	}
	if bytes.Contains(data, []byte("<remotePackage")) || bytes.Contains(data, []byte("<archives")) {
		t.Fatal("remote-only metadata leaked")
	}
	if bytes.Contains(data, []byte("<dependencies></dependencies>")) {
		t.Fatal("empty dependency element violates repository XSD")
	}
}
func TestLifecycleReceiptsIntegrationAndPreservation(t *testing.T) {
	b := newTestBackend(t)
	write(t, filepath.Join(b.home, ".zshrc"), []byte("# private\n"), 0640)
	specs := installFixture(t, b, testSpec(toolsID, revision{Major: 20}), testSpec("platform-tools", revision{Major: 1}), testSpec("cmake;3.31.6", revision{Major: 3, Minor: 31, Micro: 6}))
	assertAbsent(t, filepath.Join(b.home, ".profile"))
	target, err := os.Readlink(filepath.Join(b.root, "cmdline-tools/latest"))
	if err != nil || target != toolsDirectory {
		t.Fatal(target, err)
	}
	if err = action(b, "integrate"); err != nil {
		t.Fatal(err)
	}
	profile, _ := os.ReadFile(filepath.Join(b.home, ".zshrc"))
	if !bytes.Contains(profile, []byte("cmake/3.31.6/bin")) || !bytes.Contains(profile, []byte("ANDROID_HOME")) {
		t.Fatal(string(profile))
	}
	write(t, filepath.Join(b.root, ".android/avd/private.ini"), []byte("sdk personal"), 0600)
	write(t, filepath.Join(b.home, ".android/avd/private.ini"), []byte("home personal"), 0600)
	updated := testSpec("platform-tools", revision{Major: 2})
	installFixture(t, b, updated)
	histories, _ := filepath.Glob(filepath.Join(filepath.Dir(b.root), ".atm-sdk-history/package-*/previous/source.properties"))
	if len(histories) != 1 {
		t.Fatal(histories)
	}
	if err = action(b, "remove", "platform-tools"); err != nil {
		t.Fatal(err)
	}
	assertAbsent(t, filepath.Join(b.root, "platform-tools"))
	if err = action(b, "uninstall"); err != nil {
		t.Fatal(err)
	}
	assertContents(t, filepath.Join(b.home, ".zshrc"), "# private\n")
	assertContents(t, filepath.Join(b.root, ".android/avd/private.ini"), "sdk personal")
	assertContents(t, filepath.Join(b.home, ".android/avd/private.ini"), "home personal")
	assertContents(t, filepath.Join(b.root, "licenses", specs[0].License.ID), licenseHash(specs[0].License.Text)+"\n")
	if _, err = os.Stat(histories[0]); err != nil {
		t.Fatal("retained update history deleted", err)
	}
}
func TestUnownedAndModifiedPreserved(t *testing.T) {
	b := newTestBackend(t)
	write(t, filepath.Join(b.root, "source.properties"), []byte("legacy"), 0600)
	b.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("network for unowned SDK"); return nil, nil })}
	for _, op := range []string{"install", "update", "remove", "uninstall", "integrate"} {
		args := []string(nil)
		if op == "install" || op == "update" {
			args = []string{"--accept-licenses", strings.Repeat("a", 64)}
		}
		if op == "remove" {
			args = []string{"platform-tools"}
		}
		if err := action(b, op, args...); err == nil {
			t.Fatal(op)
		}
	}
	for _, op := range []string{"menu", "status"} {
		if err := action(b, op); err != nil {
			t.Fatal(err)
		}
	}
	assertContents(t, filepath.Join(b.root, "source.properties"), "legacy")
	b = newTestBackend(t)
	installFixture(t, b, testSpec("platform-tools", revision{Major: 1}))
	write(t, filepath.Join(b.root, "platform-tools/custom"), []byte("mine"), 0600)
	if err := action(b, "remove", "platform-tools"); err == nil {
		t.Fatal("modified package deleted")
	}
	if err := action(b, "uninstall"); err == nil {
		t.Fatal("modified SDK deleted")
	}
	assertContents(t, filepath.Join(b.root, "platform-tools/custom"), "mine")
}
func TestReviewBoundsAndChangedDigestRefusal(t *testing.T) {
	b := newTestBackend(t)
	specs := installFixture(t, b, testSpec("platform-tools", revision{Major: 1}))
	s := specs[0]
	metadata := repoBytes(t, remote(s))
	fixture(t, b, []packageSpec{s}, map[string][]byte{}, metadata)
	var logs []string
	b.log = func(s string) { logs = append(logs, s) }
	choices, err := b.menu(context.Background(), []string{"review-update"})
	if err != nil {
		t.Fatal(err)
	}
	if len(choices) != 2 || len(logs) > 40 {
		t.Fatal(choices, len(logs))
	}
	var all strings.Builder
	for _, l := range logs {
		if len(l) > protocol.MaxMessage {
			t.Fatal("oversized log")
		}
		all.WriteString(l)
	}
	if !strings.Contains(all.String(), testLicense().Text) {
		t.Fatal("license text not emitted in full")
	}
	s.Revision.Major = 2
	fixture(t, b, []packageSpec{s}, map[string][]byte{}, repoBytes(t, remote(s)))
	if err := action(b, "update", choices[0].Arguments...); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatal(err)
	}
	rec, err := b.readReceipt("platform-tools")
	if err != nil || rec.Spec.Revision.Major != 1 {
		t.Fatal(rec, err)
	}
	huge := s
	huge.License.Text = strings.Repeat("a", (128<<10)+1)
	if _, err := reviewOutput(installPlan{1, "install", b.root, []packageSpec{huge}}); err == nil {
		t.Fatal("oversized license offered")
	}
}
func TestOfflineDryRunNoJavaExecution(t *testing.T) {
	b := newTestBackend(t)
	sentinel := filepath.Join(b.home, "executed")
	write(t, filepath.Join(b.req.AppsDir, "Java/current/bin/java"), []byte("#!/bin/sh\ntouch "+sentinel+"\n"), 0700)
	b.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("unexpected network"); return nil, nil })}
	for _, args := range [][]string{nil, {"stacks"}, {"java"}, {"custom"}} {
		if err := action(b, "menu", args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := action(b, "status"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ATM_DRY_RUN", "1")
	for _, op := range []string{"menu", "status", "install", "update", "remove", "uninstall", "integrate"} {
		if err := action(b, op); err != nil {
			t.Fatal(err)
		}
	}
	assertAbsent(t, b.root)
	assertAbsent(t, sentinel)
	assertAbsent(t, filepath.Join(b.home, ".profile"))
}
func TestArchiveFailuresAndIdentity(t *testing.T) {
	for _, kind := range []string{"traversal", "duplicate", "link-escape", "link-cycle", "special", "setuid", "wrong-revision", "wrong-arch", "crc"} {
		t.Run(kind, func(t *testing.T) {
			b := newTestBackend(t)
			s := testSpec("platform-tools", revision{Major: 1})
			items := payloadItems(t, s)
			switch kind {
			case "traversal":
				items = append(items, zipItem{"package/../../escape", []byte("x"), 0600})
			case "duplicate":
				items = append(items, items[0])
			case "link-escape":
				items = append(items, zipItem{"package/escape", []byte("../../outside"), os.ModeSymlink | 0777})
			case "link-cycle":
				items = append(items, zipItem{"package/a", []byte("b"), os.ModeSymlink | 0777}, zipItem{"package/b", []byte("a"), os.ModeSymlink | 0777})
			case "special":
				items = append(items, zipItem{"package/fifo", nil, os.ModeNamedPipe | 0600})
			case "setuid":
				items[1].mode |= os.ModeSetuid
			case "wrong-revision":
				items[0].data = []byte("Pkg.Revision=99.0.0\n")
			case "wrong-arch":
				binary.LittleEndian.PutUint16(items[1].data[18:], 183)
			}
			data := zipBytes(t, items)
			if kind == "crc" {
				header := bytes.Index(data, []byte{'P', 'K', 1, 2})
				if header < 0 {
					t.Fatal("missing ZIP central directory")
				}
				data[header+16] ^= 255
			}
			specs := fixture(t, b, []packageSpec{s}, map[string][]byte{s.ID: data}, nil)
			err := b.withLock(func() error { return b.install(context.Background(), installPlan{1, "install", b.root, specs}, false) })
			if err == nil {
				t.Fatal("bad archive accepted")
			}
			assertAbsent(t, filepath.Join(b.root, s.Directory))
		})
	}
}
func TestCancellationKeepsEarlierPublishedPackages(t *testing.T) {
	b := newTestBackend(t)
	a := testSpec("platform-tools", revision{Major: 1})
	bb := testSpec("emulator", revision{Major: 1})
	archives := map[string][]byte{a.ID: zipBytes(t, payloadItems(t, a)), bb.ID: zipBytes(t, payloadItems(t, bb))}
	specs := fixture(t, b, []packageSpec{a, bb}, archives, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b.log = func(s string) {
		if strings.HasPrefix(s, "Installed platform-tools") {
			cancel()
		}
	}
	err := b.withLock(func() error { return b.install(ctx, installPlan{1, "install", b.root, specs}, false) })
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err = b.verifyOwned(context.Background(), a.ID); err != nil {
		t.Fatal(err)
	}
	assertAbsent(t, filepath.Join(b.root, bb.Directory))
}
func TestCancelledReplacementRestoresOldPackage(t *testing.T) {
	b := newTestBackend(t)
	installFixture(t, b, testSpec("platform-tools", revision{Major: 1}))
	s := testSpec("platform-tools", revision{Major: 2})
	specs := fixture(t, b, []packageSpec{s}, map[string][]byte{s.ID: zipBytes(t, payloadItems(t, s))}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b.log = func(s string) {
		if strings.HasPrefix(s, "Previous package retained:") {
			cancel()
		}
	}
	err := b.withLock(func() error { return b.install(ctx, installPlan{1, "update", b.root, specs}, true) })
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	rec, err := b.verifyOwned(context.Background(), s.ID)
	if err != nil || rec.Spec.Revision.Major != 1 {
		t.Fatal(rec, err)
	}
}
func TestProtocolReviewEventBudget(t *testing.T) {
	b := newTestBackend(t)
	b.req.Operation = "menu"
	data, _ := json.Marshal(b.req)
	var out bytes.Buffer
	if err := serve(context.Background(), bytes.NewReader(data), &out); err != nil {
		t.Fatal(err)
	}
	stream := protocol.Stream{RequestID: b.req.RequestID}
	for _, line := range bytes.Split(bytes.TrimSpace(out.Bytes()), []byte("\n")) {
		if _, err := stream.Accept(line); err != nil {
			t.Fatal(err)
		}
	}
	if stream.Result == nil || len(stream.Result.Choices) != 8 {
		t.Fatal(stream.Result)
	}
	s := testSpec("platform-tools", revision{Major: 1})
	s.License.Text = strings.Repeat("界", 40000)
	chunks, err := reviewOutput(installPlan{1, "install", b.root, []packageSpec{s}})
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) > 40 {
		t.Fatal(len(chunks))
	}
	for _, c := range chunks {
		if len(c) > protocol.MaxMessage {
			t.Fatal("frame too large")
		}
	}
}
func TestOfficialRepositorySnapshot(t *testing.T) {
	name := os.Getenv("ATM_TEST_ANDROID_REPOSITORY")
	if name == "" {
		t.Skip("optional saved official metadata-only snapshot")
	}
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := parseRepository(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, stack := range []string{"latest", "api36", "api35", "api35_legacy", "api34"} {
		o := options{Stack: stack}
		plan, err := repo.resolvePlan(o.ids(), "/tmp/read-only-sdk-fixture", "install")
		if err != nil {
			t.Fatal(stack, err)
		}
		if len(plan.Packages) < 7 {
			t.Fatal(stack, plan)
		}
		if _, err = reviewOutput(plan); err != nil {
			t.Fatal(stack, err)
		}
	}
	// Schema string whitespace is preserved; AOSP License hashes the decoded UTF-8
	// text without trimming, unlike metadata checksum hex whitespace.
	for _, l := range repo.Licenses {
		h := sha1.Sum([]byte(l.Text))
		if licenseHash(l.Text) != fmt.Sprintf("%x", h) {
			t.Fatal("license hash mismatch")
		}
	}
}

func TestDownloadHashSizeCancellationAndRedirects(t *testing.T) {
	for _, kind := range []string{"hash", "size", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			b := newTestBackend(t)
			s := testSpec("platform-tools", revision{Major: 1})
			data := zipBytes(t, payloadItems(t, s))
			specs := fixture(t, b, []packageSpec{s}, map[string][]byte{s.ID: data}, nil)
			ctx := context.Background()
			cancel := func() {}
			switch kind {
			case "hash":
				specs[0].Checksum = strings.Repeat("0", 40)
			case "size":
				specs[0].Size++
			case "cancel":
				var stop context.CancelFunc
				ctx, stop = context.WithTimeout(ctx, 30*time.Millisecond)
				cancel = stop
				b.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })
			}
			defer cancel()
			err := b.withLock(func() error { return b.install(ctx, installPlan{1, "install", b.root, specs}, false) })
			if err == nil {
				t.Fatal("failed download published")
			}
			assertAbsent(t, filepath.Join(b.root, s.Directory))
			stages, _ := filepath.Glob(filepath.Join(b.root, ".atm-stage-*"))
			if len(stages) > 0 {
				t.Fatal(stages)
			}
		})
	}
	b := newTestBackend(t)
	calls := 0
	b.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://example.invalid/file.zip"}}, Body: http.NoBody, Request: r}, nil
	})}
	if _, err := b.get(context.Background(), repositoryURL); err == nil || calls != 1 {
		t.Fatal("redirect escaped official host", err, calls)
	}
}
func TestSafeZIPLinksAndRootLayout(t *testing.T) {
	for _, wrapped := range []bool{false, true} {
		t.Run(fmt.Sprint(wrapped), func(t *testing.T) {
			b := newTestBackend(t)
			s := testSpec("platform-tools", revision{Major: 1})
			items := payloadItems(t, s)
			items = append(items, zipItem{"package/adb-link", []byte("adb"), os.ModeSymlink | 0777})
			if !wrapped {
				for i := range items {
					items[i].name = strings.TrimPrefix(items[i].name, "package/")
				}
			}
			specs := fixture(t, b, []packageSpec{s}, map[string][]byte{s.ID: zipBytes(t, items)}, nil)
			if err := b.withLock(func() error { return b.install(context.Background(), installPlan{1, "install", b.root, specs}, false) }); err != nil {
				t.Fatal(err)
			}
			if _, err := b.verifyOwned(context.Background(), s.ID); err != nil {
				t.Fatal(err)
			}
			if err := action(b, "remove", s.ID); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestDependencyRemovalAndProfileEditGuards(t *testing.T) {
	b := newTestBackend(t)
	sdk := testSpec("platforms;android-36", revision{Major: 2})
	sdk.Dependencies = []dependency{{Path: "platform-tools", Minimum: revision{Major: 1}}}
	installFixture(t, b, testSpec(toolsID, revision{Major: 20}), testSpec("platform-tools", revision{Major: 1}), sdk)
	if err := action(b, "remove", "platform-tools"); err == nil {
		t.Fatal("removed required dependency")
	}
	if err := action(b, "integrate"); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(b.home, ".profile")
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.Replace(data, []byte("export ANDROID_HOME="), []byte("export PERSONAL_ANDROID_HOME="), 1)
	write(t, file, data, 0600)
	if err := action(b, "uninstall"); err == nil {
		t.Fatal("edited profile overwritten")
	}
	if _, err := b.verifyOwned(context.Background(), toolsID); err != nil {
		t.Fatal("uninstall changed packages before refusal", err)
	}
}

func TestCMakeRemovalRefreshesOnlyOwnedIntegration(t *testing.T) {
	for _, scenario := range []string{"fallback-and-last", "edited-block", "deleted-blocks", "not-integrated", "modified-package"} {
		t.Run(scenario, func(t *testing.T) {
			b := newTestBackend(t)
			write(t, filepath.Join(b.home, ".zshrc"), []byte("# personal zsh\n"), 0640)
			older := "cmake;3.22.1"
			newer := "cmake;3.31.6"
			installFixture(t, b, testSpec(toolsID, revision{Major: 20}),
				testSpec(older, revision{Major: 3, Minor: 22, Micro: 1}),
				testSpec(newer, revision{Major: 3, Minor: 31, Micro: 6}))
			if scenario == "not-integrated" {
				if err := action(b, "remove", newer); err != nil {
					t.Fatal(err)
				}
				assertAbsent(t, filepath.Join(b.home, ".profile"))
				assertAbsent(t, filepath.Join(b.home, ".bashrc"))
				assertContents(t, filepath.Join(b.home, ".zshrc"), "# personal zsh\n")
				return
			}
			if err := action(b, "integrate"); err != nil {
				t.Fatal(err)
			}
			profile := filepath.Join(b.home, ".profile")
			data, err := os.ReadFile(profile)
			if err != nil {
				t.Fatal(err)
			}
			data = append(data, []byte("# user added after integration\n")...)
			if scenario == "edited-block" {
				data = bytes.Replace(data, []byte("export ANDROID_HOME="), []byte("export USER_ANDROID_HOME="), 1)
			}
			write(t, profile, data, 0600)
			if scenario == "modified-package" {
				write(t, filepath.Join(b.root, "cmake/3.31.6/personal"), []byte("preserve"), 0600)
			}
			if scenario == "edited-block" || scenario == "modified-package" {
				before := map[string][]byte{}
				for _, name := range []string{profile, filepath.Join(b.home, ".bashrc"), filepath.Join(b.home, ".zshrc"), filepath.Join(b.root, markerName)} {
					before[name], err = os.ReadFile(name)
					if err != nil {
						t.Fatal(err)
					}
				}
				if err := action(b, "remove", newer); err == nil {
					t.Fatal("removal ignored external edit")
				}
				for name, want := range before {
					got, err := os.ReadFile(name)
					if err != nil || !bytes.Equal(got, want) {
						t.Fatalf("changed %s on refusal: %v", name, err)
					}
				}
				if _, err := b.readReceipt(newer); err != nil {
					t.Fatal("package removed on refusal", err)
				}
				return
			}
			if scenario == "deleted-blocks" {
				if err := os.Remove(profile); err != nil {
					t.Fatal(err)
				}
				write(t, filepath.Join(b.home, ".bashrc"), []byte("# user removed ATM block\n"), 0600)
			}
			if err := action(b, "remove", newer); err != nil {
				t.Fatal(err)
			}
			assertAbsent(t, filepath.Join(b.root, "cmake/3.31.6"))
			for _, name := range []string{".profile", ".bashrc", ".zshrc"} {
				file := filepath.Join(b.home, name)
				if scenario == "deleted-blocks" && name == ".profile" {
					assertAbsent(t, file)
					continue
				}
				if scenario == "deleted-blocks" && name == ".bashrc" {
					assertContents(t, file, "# user removed ATM block\n")
					continue
				}
				got, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				if bytes.Contains(got, []byte("cmake/3.31.6/bin")) || !bytes.Contains(got, []byte("cmake/3.22.1/bin")) {
					t.Fatalf("stale CMake PATH: %s", got)
				}
			}
			if scenario == "fallback-and-last" {
				got, _ := os.ReadFile(profile)
				if !bytes.Contains(got, []byte("# user added after integration\n")) {
					t.Fatal("unrelated profile edit lost")
				}
				if err := action(b, "remove", older); err != nil {
					t.Fatal(err)
				}
				for _, name := range []string{".profile", ".bashrc", ".zshrc"} {
					got, err := os.ReadFile(filepath.Join(b.home, name))
					if err != nil || bytes.Contains(got, []byte("/cmake/")) {
						t.Fatalf("last CMake left stale PATH: %s %v", got, err)
					}
				}
			}
			info, err := os.Stat(filepath.Join(b.home, ".zshrc"))
			if err != nil || info.Mode().Perm() != 0640 {
				t.Fatal("profile permissions changed", err)
			}
		})
	}
}
