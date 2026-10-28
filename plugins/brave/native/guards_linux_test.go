//go:build linux

package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestProcessGuardSyntheticVisibilityAndSingleton(t *testing.T) {
	b := testBackend(t)
	fixtureFile(t, filepath.Join(b.profile, "Default/Bookmarks"), "fixture")
	proc := filepath.Join(privateFixture(t), "proc")
	fixtureFile(t, filepath.Join(proc, "mounts"), "proc /proc proc rw,hidepid=0 0 0\n")
	fixtureFile(t, filepath.Join(proc, "123/cmdline"), "ordinary\x00arg\x00")
	fixtureFile(t, filepath.Join(proc, "123/comm"), "ordinary\n")
	if e := os.Symlink("/usr/bin/ordinary", filepath.Join(proc, "123/exe")); e != nil {
		t.Fatal(e)
	}
	if e := checkBrowser(context.Background(), b.profile, proc); e != nil {
		t.Fatal(e)
	}
	fixtureFile(t, filepath.Join(proc, "123/cmdline"), "ordinary\x00--user-data-dir="+b.profile+"\x00")
	if e := checkBrowser(context.Background(), b.profile, proc); e == nil {
		t.Fatal("profile args ignored")
	}
	fixtureFile(t, filepath.Join(proc, "123/cmdline"), "ordinary\x00")
	if e := os.Remove(filepath.Join(proc, "123/exe")); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink("/opt/brave.com/brave/brave", filepath.Join(proc, "123/exe")); e != nil {
		t.Fatal(e)
	}
	if e := checkBrowser(context.Background(), b.profile, proc); e == nil {
		t.Fatal("actual Brave executable ignored")
	}
	if e := os.RemoveAll(filepath.Join(proc, "123")); e != nil {
		t.Fatal(e)
	}
	if e := checkBrowser(context.Background(), b.profile, proc); e != nil {
		t.Fatal("disappeared process", e)
	}
	fixtureFile(t, filepath.Join(proc, "mounts"), "proc /proc proc rw,hidepid=2 0 0\n")
	if e := checkBrowser(context.Background(), b.profile, proc); e == nil {
		t.Fatal("hidden processes accepted")
	}
	fixtureFile(t, filepath.Join(proc, "mounts"), "proc /proc proc rw 0 0\n")
	if e := os.Symlink("hostname-123", filepath.Join(b.profile, "SingletonLock")); e != nil {
		t.Fatal(e)
	}
	if e := checkBrowser(context.Background(), b.profile, proc); e == nil {
		t.Fatal("singleton accepted")
	}
}
func TestProcessAgentFallbackNarrow(t *testing.T) {
	profile := "/private/Brave-Browser"
	for _, name := range []string{"ssh-agent", "gpg-agent"} {
		if e := inspectProcess([]byte("/usr/bin/"+name+"\x00--daemon\x00"), []byte(name+"\n"), "", syscall.EACCES, profile); e != nil {
			t.Fatal(e)
		}
	}
	cases := []struct {
		cmd, comm, exe string
		err            error
	}{
		{"brave\x00", "ssh-agent\n", "", syscall.EACCES},
		{"ssh-agent\x00", "brave\n", "", syscall.EACCES},
		{"ssh-agent\x00--user-data-dir=" + profile + "\x00", "ssh-agent\n", "", syscall.EACCES},
		{"ssh-agent\x00--user-data-dir\x00../relative\x00", "ssh-agent\n", "", syscall.EACCES},
		{"ssh-agent", "ssh-agent\n", "", syscall.EACCES},
		{"\x00", "ssh-agent\n", "", syscall.EACCES},
		{"python\x00", "python\n", "", syscall.EACCES},
		{"unknown\x00", "unknown\n", "", syscall.EACCES},
		{"ssh-agent\x00", "gpg-agent\n", "", syscall.EACCES},
		{"ssh-agent\x00", "ssh-agent\n", "", os.ErrNotExist},
		{"ssh-agent\x00", "sixteen-characters\n", "", syscall.EACCES},
		{strings.Repeat("a", 128<<10) + "\x00", "ssh-agent\n", "", syscall.EACCES},
	}
	for _, c := range cases {
		if e := inspectProcess([]byte(c.cmd), []byte(c.comm), c.exe, c.err, profile); e == nil {
			t.Fatal("unsafe fallback", c.comm)
		}
	}
}
func TestProcessMetadataReadFailureAndCancellation(t *testing.T) {
	b := testBackend(t)
	fixtureFile(t, filepath.Join(b.profile, "Default/Bookmarks"), "fixture")
	proc := privateFixture(t)
	fixtureFile(t, filepath.Join(proc, "mounts"), "proc /proc proc rw 0 0\n")
	fixtureFile(t, filepath.Join(proc, "123/comm"), "unknown\n")
	if e := checkBrowser(context.Background(), b.profile, proc); e == nil {
		t.Fatal("unreadable command accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e := checkBrowser(ctx, b.profile, proc); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}

type fixtureTransport func(*http.Request) (*http.Response, error)

func (f fixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestKeyDownloadPinsBoundsAndFailure(t *testing.T) {
	for _, mode := range []string{"valid", "wrong", "oversized", "http", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			b := testBackend(t)
			key := "synthetic-public-key"
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			b.client = &http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() != aptKeyURL {
					t.Fatal(r.URL)
				}
				status := 200
				body := key
				switch mode {
				case "wrong":
					body = "wrong"
				case "oversized":
					body = strings.Repeat("x", (1<<20)+1)
				case "http":
					status = 403
				case "cancel":
					cancel()
					return nil, context.Canceled
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})}
			v, e := b.fetchKey(ctx, systemPlan{Manager: "apt-get", KeyHash: digest([]byte(key))})
			if mode == "valid" {
				if e != nil || string(v) != key {
					t.Fatal(string(v), e)
				}
			} else if e == nil {
				t.Fatal("unsafe key accepted")
			}
		})
	}
}
func TestPathAndRootTrustGuards(t *testing.T) {
	for _, p := range []string{"relative", "/", "/tmp/a/../b", "/tmp/a\n"} {
		if e := absolutePath(p); e == nil {
			t.Fatal(p)
		}
	}
	for _, p := range [][2]string{{"/a", "/a/b"}, {"/a/b", "/a"}, {"/a", "/a"}} {
		if !overlaps(p[0], p[1]) {
			t.Fatal(p)
		}
	}
	if overlaps("/a", "/another") {
		t.Fatal("prefix collision")
	}
	p := privateFixture(t)
	f := filepath.Join(p, "input")
	fixtureFile(t, f, "private")
	if e := checkNewStage(p); e == nil && os.Geteuid() != 0 {
		t.Fatal("user-owned root stage accepted")
	}
	if e := checkSystemInput(f, []byte("private")); e == nil && os.Geteuid() != 0 {
		t.Fatal("user-owned privileged input accepted")
	}
	if e := os.Symlink(f, filepath.Join(p, "link")); e != nil {
		t.Fatal(e)
	}
	r, e := openDir(p)
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	if _, e = regular(r, "link", 1024); e == nil {
		t.Fatal("linked data followed")
	}
	if e = checkNewStage(filepath.Join(p, "link")); e == nil {
		t.Fatal("linked stage accepted")
	}
	if e = os.Chmod(p, 0777); e != nil {
		t.Fatal(e)
	}
	if e = realDirs(p, false, false); e == nil {
		t.Fatal("writable ancestry accepted")
	}
}
func TestRestoreRollbackAfterCancellation(t *testing.T) {
	b := testBackend(t)
	name, h := fullBackup(t, b)
	fixtureFile(t, filepath.Join(b.profile, "only-old"), "keep")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	b.closed = func(context.Context, string) error {
		calls++
		if calls == 3 {
			cancel()
		}
		return nil
	}
	if e := b.restore(ctx, name, h); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	v, e := os.ReadFile(filepath.Join(b.profile, "only-old"))
	if e != nil || string(v) != "keep" {
		t.Fatal("rollback lost original", e)
	}
	r, _, e := readRecord(b.state)
	if e != nil || r.Phase != "rolled-back" {
		t.Fatal(r, e)
	}
}
