package install

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type archiveEntry struct {
	name, body string
	kind       byte
}

func tarBytes(t *testing.T, entries ...archiveEntry) []byte {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		kind := e.kind
		if kind == 0 {
			kind = tar.TypeReg
		}
		h := &tar.Header{Name: e.name, Mode: 0755, Size: int64(len(e.body)), Typeflag: kind}
		if kind != tar.TypeReg {
			h.Size = 0
		}
		if kind == tar.TypeSymlink || kind == tar.TypeLink {
			h.Linkname = "../../escape"
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Size > 0 {
			if _, err := io.WriteString(tw, e.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func fixture(t *testing.T, body []byte) (Manager, Spec, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.Write(body) }))
	t.Cleanup(server.Close)
	t.Setenv("ATM_INSTALL_TEST_CA", hex.EncodeToString(server.Certificate().Raw))
	m := Manager{Root: filepath.Join(t.TempDir(), "store space \u00e7"), PluginID: "fixture", Client: server.Client()}
	s := Spec{Version: "1.0.0", URL: server.URL + "/archive", SHA256: fmt.Sprintf("%x", sha256.Sum256(body)), Format: "tar.gz", StripPrefix: "release", Origins: []string{server.URL}}
	return m, s, &calls
}

func TestInstallActivateAndPreserveVersions(t *testing.T) {
	m, s, calls := fixture(t, tarBytes(t, archiveEntry{"release/bin/tool", "payload", 0}))
	ctx := context.Background()
	first, err := m.Install(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if first.Generation != 1 {
		t.Fatal(first)
	}
	data, err := os.ReadFile(filepath.Join(first.Path, "bin/tool"))
	if err != nil || string(data) != "payload" {
		t.Fatal(string(data), err)
	}
	second, err := m.Install(ctx, s)
	if err != nil || second.Generation != 1 || calls.Load() != 1 {
		t.Fatal("reuse downloaded or changed activation", second, err)
	}
	s.Version = "2.0.0"
	second, err = m.Install(ctx, s)
	if err != nil || second.Generation != 2 {
		t.Fatal(second, err)
	}
	back, err := m.Activate(ctx, "1.0.0")
	if err != nil || back.Generation != 3 {
		t.Fatal(back, err)
	}
	active, err := m.Active(ctx)
	if err != nil || active.Version != "1.0.0" {
		t.Fatal(active, err)
	}
	if _, err = os.Stat(filepath.Join(second.Path, "bin/tool")); err != nil {
		t.Fatal("old version removed", err)
	}
	if err = m.Recover(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidArtifactsNeverActivate(t *testing.T) {
	cases := map[string][]archiveEntry{
		"traversal": {{"release/../escape", "bad", 0}}, "absolute": {{"/escape", "bad", 0}},
		"symlink": {{"release/link", "", tar.TypeSymlink}}, "hardlink": {{"release/link", "", tar.TypeLink}},
		"device": {{"release/device", "", tar.TypeChar}}, "duplicate": {{"release/a", "one", 0}, {"release/a", "two", 0}},
		"case":        {{"release/A/file", "one", 0}, {"release/a/other", "two", 0}},
		"parent-file": {{"release/a", "one", 0}, {"release/a/b", "two", 0}}, "reserved": {{"release/NUL.exe", "bad", 0}},
		"outside-prefix": {{"other/a", "bad", 0}}, "empty": {}, "file-limit": {{"release/a", "payload", 0}},
		"expanded-limit": {{"release/a", "1234", 0}, {"release/b", "5678", 0}}, "entry-limit": {{"release/a", "1", 0}, {"release/b", "2", 0}},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			m, s, _ := fixture(t, tarBytes(t, entries...))
			switch name {
			case "file-limit":
				m.Limits.FileBytes = 2
			case "expanded-limit":
				m.Limits.ExpandedBytes = 5
			case "entry-limit":
				m.Limits.Entries = 1
			}
			if _, err := m.Install(context.Background(), s); err == nil {
				t.Fatal("invalid archive accepted")
			}
			active, err := m.Active(context.Background())
			if err != nil || active.Version != "" {
				t.Fatal("failed install activated", active, err)
			}
			entries, err := os.ReadDir(filepath.Join(m.Root, "staging"))
			if err != nil || len(entries) != 0 {
				t.Fatal("failed stage not cleaned", err)
			}
		})
	}
}

func TestZIPAndCorruptGzip(t *testing.T) {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	h := &zip.FileHeader{Name: "release/bin/tool", Method: zip.Deflate}
	h.SetMode(0755)
	w, err := z.CreateHeader(h)
	if err != nil {
		t.Fatal(err)
	}
	io.WriteString(w, "zip payload")
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	m, s, _ := fixture(t, b.Bytes())
	s.Format = "zip"
	if _, err = m.Install(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	bad := tarBytes(t, archiveEntry{"release/a", "body", 0})
	bad[len(bad)-5] ^= 0xFF
	m, s, _ = fixture(t, bad)
	if _, err = m.Install(context.Background(), s); err == nil {
		t.Fatal("gzip trailer corruption accepted")
	}
}

func TestNoAdoptionTamperingAndLock(t *testing.T) {
	m, s, _ := fixture(t, tarBytes(t, archiveEntry{"release/a", "original", 0}))
	if err := os.Mkdir(m.Root, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Install(context.Background(), s); err == nil {
		t.Fatal("unowned directory adopted")
	}
	if err := os.Remove(m.Root); err != nil {
		t.Fatal(err)
	}
	result, err := m.Install(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	locked, err := m.open(false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Install(context.Background(), s); err == nil {
		t.Fatal("concurrent lock ignored")
	}
	locked.close()
	if err = os.WriteFile(filepath.Join(result.Path, "a"), []byte("modified"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Active(context.Background()); err == nil {
		t.Fatal("tampered active payload accepted")
	}
	s.Version = "2.0.0"
	if _, err = m.Install(context.Background(), s); err == nil {
		t.Fatal("modified active state ignored")
	}
}

func TestFailureBoundariesPreserveActive(t *testing.T) {
	for _, boundary := range []string{"staged", "verified", "published", "before-activation"} {
		t.Run(boundary, func(t *testing.T) {
			m, s, _ := fixture(t, tarBytes(t, archiveEntry{"release/a", "payload", 0}))
			if _, err := m.Install(context.Background(), s); err != nil {
				t.Fatal(err)
			}
			s.Version = "2.0.0"
			m.checkpoint = func(name string) error {
				if name == boundary {
					return fmt.Errorf("injected failure")
				}
				return nil
			}
			if _, err := m.Install(context.Background(), s); err == nil {
				t.Fatal("injection failed")
			}
			m.checkpoint = nil
			active, err := m.Active(context.Background())
			if err != nil || active.Version != "1.0.0" {
				t.Fatal("prior activation lost", active, err)
			}
			if _, err = m.Install(context.Background(), s); err != nil {
				t.Fatal("retry failed", err)
			}
		})
	}
}

func TestDryRunCancellationAndDownloadGuards(t *testing.T) {
	m, s, calls := fixture(t, tarBytes(t, archiveEntry{"release/a", "body", 0}))
	m.DryRun = true
	if _, err := m.Install(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Activate(context.Background(), "1.0.0"); err != nil {
		t.Fatal(err)
	}
	if err := m.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(m.Root); !os.IsNotExist(err) || calls.Load() != 0 {
		t.Fatal("dry-run has side effects")
	}
	m.DryRun = false
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.Install(ctx, s); err != context.Canceled {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatal("cancelled operation downloaded")
	}
	for _, mode := range []string{"checksum", "download-limit", "origin", "http", "version"} {
		t.Run(mode, func(t *testing.T) {
			m := m
			m.Root = filepath.Join(t.TempDir(), "app")
			s := s
			switch mode {
			case "checksum":
				s.SHA256 = strings.Repeat("0", 64)
			case "download-limit":
				m.Limits.DownloadBytes = 1
			case "origin":
				s.Origins = []string{"https://invalid.example"}
			case "http":
				s.URL = "http://example.com/file"
			case "version":
				s.Version = "../escape"
			}
			if _, err := m.Install(context.Background(), s); err == nil {
				t.Fatal("invalid request accepted")
			}
		})
	}
}

func TestRedirectCannotLeaveApprovedOrigin(t *testing.T) {
	var contacted atomic.Bool
	other := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { contacted.Store(true); w.Write([]byte("bad")) }))
	defer other.Close()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL, http.StatusFound) }))
	defer server.Close()
	m := Manager{Root: filepath.Join(t.TempDir(), "app"), PluginID: "fixture", Client: server.Client()}
	s := Spec{Version: "1", URL: server.URL, SHA256: strings.Repeat("0", 64), Format: "zip", Origins: []string{server.URL}}
	if _, err := m.Install(context.Background(), s); err == nil || contacted.Load() {
		t.Fatal("redirect escaped approved origin", err)
	}
}

func TestProcessInterruptionRecovery(t *testing.T) {
	if os.Getenv("ATM_INSTALL_CRASH_HELPER") == "1" {
		var s Spec
		if err := json.Unmarshal([]byte(os.Getenv("ATM_INSTALL_SPEC")), &s); err != nil {
			os.Exit(3)
		}
		m := Manager{Root: os.Getenv("ATM_INSTALL_ROOT"), PluginID: "fixture"}
		// The test server's explicit TLS transport is supplied through the inherited test CA.
		m.Client = &http.Client{Transport: helperTransport()}
		m.checkpoint = func(name string) error {
			if name == os.Getenv("ATM_INSTALL_BOUNDARY") {
				os.Exit(71)
			}
			return nil
		}
		_, err := m.Install(context.Background(), s)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(72)
		}
		os.Exit(0)
	}
	for _, boundary := range []string{"verified", "published", "before-activation", "committed"} {
		t.Run(boundary, func(t *testing.T) {
			m, s, _ := fixture(t, tarBytes(t, archiveEntry{"release/a", "payload", 0}))
			if _, err := m.Install(context.Background(), s); err != nil {
				t.Fatal(err)
			}
			s.Version = "2.0.0"
			b, _ := json.Marshal(s)
			cmd := exec.Command(os.Args[0], "-test.run=^TestProcessInterruptionRecovery$")
			cmd.Env = append(os.Environ(), "ATM_INSTALL_CRASH_HELPER=1", "ATM_INSTALL_ROOT="+m.Root, "ATM_INSTALL_SPEC="+string(b), "ATM_INSTALL_BOUNDARY="+boundary)
			output, err := cmd.CombinedOutput()
			if e, ok := err.(*exec.ExitError); !ok || e.ExitCode() != 71 {
				t.Fatalf("helper failed: %v %s", err, output)
			}
			if _, err = m.Install(context.Background(), s); err == nil {
				t.Fatal("abandoned staging ignored")
			}
			if err = m.Recover(context.Background()); err != nil {
				t.Fatal(err)
			}
			active, err := m.Active(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			want := "1.0.0"
			if boundary == "committed" {
				want = "2.0.0"
			}
			if active.Version != want {
				t.Fatal("recovery changed committed state", active)
			}
			if _, err = m.Install(context.Background(), s); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func helperTransport() *http.Transport {
	b, err := hex.DecodeString(os.Getenv("ATM_INSTALL_TEST_CA"))
	if err != nil {
		panic(err)
	}
	cert, err := x509.ParseCertificate(b)
	if err != nil {
		panic(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}
}

func TestDownloadCancellationAndUnknownRecoveryEntry(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	m := Manager{Root: filepath.Join(t.TempDir(), "app"), PluginID: "fixture", Client: server.Client()}
	s := Spec{Version: "1", URL: server.URL, SHA256: strings.Repeat("0", 64), Format: "zip", Origins: []string{server.URL}}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if _, err := m.Install(ctx, s); err == nil {
		t.Fatal("download did not cancel")
	}
	entries, err := os.ReadDir(filepath.Join(m.Root, "staging"))
	if err != nil || len(entries) != 0 {
		t.Fatal("cancelled stage retained", err)
	}
	unknown := filepath.Join(m.Root, "staging", "personal.txt")
	if err = os.WriteFile(unknown, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = m.Recover(context.Background()); err == nil {
		t.Fatal("unknown recovery entry accepted")
	}
	b, err := os.ReadFile(unknown)
	if err != nil || string(b) != "preserve" {
		t.Fatal("unknown file removed", err)
	}
}

func TestZIPLinksTraversalAndCaseCollision(t *testing.T) {
	for _, mode := range []string{"symlink", "traversal", "case"} {
		t.Run(mode, func(t *testing.T) {
			var b bytes.Buffer
			z := zip.NewWriter(&b)
			names := []string{"release/a"}
			if mode == "traversal" {
				names = []string{"release/../escape"}
			}
			if mode == "case" {
				names = []string{"release/A/file", "release/a/other"}
			}
			for _, name := range names {
				h := &zip.FileHeader{Name: name, Method: zip.Deflate}
				h.SetMode(0600)
				if mode == "symlink" {
					h.SetMode(os.ModeSymlink | 0777)
				}
				w, err := z.CreateHeader(h)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = io.WriteString(w, "content"); err != nil {
					t.Fatal(err)
				}
			}
			if err := z.Close(); err != nil {
				t.Fatal(err)
			}
			m, s, _ := fixture(t, b.Bytes())
			s.Format = "zip"
			if _, err := m.Install(context.Background(), s); err == nil {
				t.Fatal("unsafe ZIP accepted")
			}
		})
	}
}

func TestSymlinkRootsAndIncompleteActivationRefused(t *testing.T) {
	m, s, _ := fixture(t, tarBytes(t, archiveEntry{"release/a", "payload", 0}))
	if _, err := m.Install(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		alias := filepath.Join(t.TempDir(), "alias")
		if err := os.Symlink(m.Root, alias); err != nil {
			t.Fatal(err)
		}
		other := m
		other.Root = alias + string(os.PathSeparator)
		if _, err := other.Install(context.Background(), s); err == nil {
			t.Fatal("symlink root adopted")
		}
	}
	path := filepath.Join(m.Root, "activations", "00000000000000000002.json")
	if err := os.WriteFile(path, []byte(`{"generation":2}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Active(context.Background()); err == nil {
		t.Fatal("partial activation inherited prior fields")
	}
}
