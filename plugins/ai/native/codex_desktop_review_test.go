//go:build linux

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"testing"
)

type codexReviewReader func([]byte) (int, error)

func (f codexReviewReader) Read(p []byte) (int, error) { return f(p) }

type codexReviewWriter func([]byte) (int, error)

func (f codexReviewWriter) Write(p []byte) (int, error) { return f(p) }

func TestCodexDesktopReviewDownloadTransportBoundary(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		t.Run(arch, func(t *testing.T) {
			f := newFixture(t, arch)
			raw, err := codexDesktopURL(arch)
			if err != nil {
				t.Fatal(err)
			}
			u, err := url.Parse(raw)
			if err != nil {
				t.Fatal(err)
			}
			jar, err := cookiejar.New(nil)
			if err != nil {
				t.Fatal(err)
			}
			jar.SetCookies(u, []*http.Cookie{{Name: "personal", Value: "must-not-leak"}})
			f.b.client.Jar = jar
			calls := 0
			body := []byte("synthetic bytes, not a runnable package")
			f.b.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.String() != raw || r.Method != http.MethodGet || r.Header.Get("Accept-Encoding") != "identity" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
					t.Fatalf("unexpected request: %s %s %v", r.Method, r.URL, r.Header)
				}
				return &http.Response{StatusCode: 200, ContentLength: int64(len(body)), Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body)), Request: r}, nil
			})
			var dst bytes.Buffer
			n, sum, err := f.b.downloadCodexDesktop(context.Background(), raw, &dst)
			if err != nil || n != int64(len(body)) || sum != fmt.Sprintf("%x", sha256.Sum256(body)) || !bytes.Equal(dst.Bytes(), body) || calls != 1 {
				t.Fatalf("valid fixed-origin download: %d %q %v calls=%d", n, sum, err, calls)
			}
			if f.b.client.Jar != jar || len(jar.Cookies(u)) != 1 {
				t.Fatal("download mutated shared client/cookie jar")
			}
			for _, wrong := range []string{raw + "?version=1", strings.Replace(raw, "https:", "http:", 1), "https://example.invalid/chatgpt.deb"} {
				if _, _, err := f.b.downloadCodexDesktop(context.Background(), wrong, io.Discard); err == nil {
					t.Fatalf("accepted alternate URL %q", wrong)
				}
			}
			if calls != 1 {
				t.Fatal("alternate URL reached transport")
			}
		})
	}
}

func TestCodexDesktopReviewRedirectNeverFollowed(t *testing.T) {
	f := newFixture(t, "amd64")
	raw, _ := codexDesktopURL("amd64")
	calls := 0
	f.b.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://example.invalid/untrusted.deb"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	if _, _, err := f.b.downloadCodexDesktop(context.Background(), raw, io.Discard); err == nil || calls != 1 {
		t.Fatalf("redirect accepted/followed: %v calls=%d", err, calls)
	}
}

func TestCodexDesktopReviewResponseFraming(t *testing.T) {
	for _, kind := range []string{"partial", "created", "compressed", "auto-decoded", "chunked"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t, "amd64")
			raw, _ := codexDesktopURL("amd64")
			f.b.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				resp := &http.Response{StatusCode: 200, ContentLength: 7, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("payload")), Request: r}
				switch kind {
				case "partial":
					resp.StatusCode = 206
				case "created":
					resp.StatusCode = 201
				case "compressed":
					resp.Header.Set("Content-Encoding", "gzip")
				case "auto-decoded":
					resp.Uncompressed = true
				case "chunked":
					resp.TransferEncoding = []string{"chunked"}
				}
				return resp, nil
			})
			var dst bytes.Buffer
			if _, _, err := f.b.downloadCodexDesktop(context.Background(), raw, &dst); err == nil || dst.Len() != 0 {
				t.Fatalf("invalid response accepted or written: %v bytes=%d", err, dst.Len())
			}
		})
	}
}

func TestCodexDesktopReviewDownloadFailureEvidence(t *testing.T) {
	for _, mode := range []string{"cancel-after-bytes", "writer-error", "reader-error"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t, "amd64")
			raw, _ := codexDesktopURL("amd64")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			failure := errors.New("injected I/O failure")
			var dst io.Writer = io.Discard
			if mode == "writer-error" {
				dst = codexReviewWriter(func([]byte) (int, error) { return 0, failure })
			}
			f.b.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				body := codexReviewReader(func(p []byte) (int, error) {
					n := copy(p, "payload")
					if mode == "cancel-after-bytes" {
						cancel()
					}
					if mode == "reader-error" {
						return n, failure
					}
					return n, io.EOF
				})
				return &http.Response{StatusCode: 200, ContentLength: 7, Header: make(http.Header), Body: io.NopCloser(body), Request: r}, nil
			})
			n, sum, err := f.b.downloadCodexDesktop(ctx, raw, dst)
			if err == nil || n != 0 || sum != "" {
				t.Fatalf("failed download published evidence: n=%d hash=%q err=%v", n, sum, err)
			}
			want := failure
			if mode == "cancel-after-bytes" {
				want = context.Canceled
			}
			if !errors.Is(err, want) {
				t.Fatalf("lost failure: got %v want %v", err, want)
			}
		})
	}
}

func TestCodexDesktopReviewPostconditionIdentity(t *testing.T) {
	for _, kind := range []string{"version", "architecture", "unpacked", "held", "database-error"} {
		t.Run(kind, func(t *testing.T) {
			f := newCodexDesktopFixture(t, "amd64")
			original := f.b.codexDesktop.packageRecord
			f.b.codexDesktop.packageRecord = func() (codexPackageRecord, error) {
				rec, err := original()
				if !f.hasCommand("apt-get") {
					return rec, err
				}
				switch kind {
				case "version":
					rec.Version = "0.0.1"
				case "architecture":
					rec.Arch = "arm64"
				case "unpacked":
					rec.Status = "install ok unpacked"
				case "held":
					rec.Status = "hold ok installed"
				case "database-error":
					return codexPackageRecord{}, errors.New("post-APT database unknown")
				}
				return rec, err
			}
			if err := f.run(context.Background(), "install", codexDesktopArgs()...); err == nil {
				t.Fatal("false installation success")
			}
			if !f.hasCommand("apt-get") || f.hasCommand("rm") || f.hasCommand("rmdir") {
				t.Fatal("test did not reach postcondition, or cleaned uncertain state")
			}
			rec, err := f.b.codexDesktopRecord()
			if err != nil || rec == nil || rec.Phase != "unknown" || rec.LastPhase != "apt-running" {
				t.Fatalf("missing uncertain journal: %+v %v", rec, err)
			}
			if _, err := os.Lstat(f.b.root(codexDesktopSystem) + ".lock"); err != nil {
				t.Fatal("uncertain privileged operation lost lock", err)
			}
		})
	}
}

func TestCodexDesktopReviewConfigurationBinding(t *testing.T) {
	const base = "Dir=/\nDir::State=var/lib/apt\nDir::State::status=/var/lib/dpkg/status\nDir::Bin::dpkg=/usr/bin/dpkg\n"
	if err := validateCodexAPTConfig(base, "/usr/bin/dpkg"); err != nil {
		t.Fatal("standard configuration refused", err)
	}
	for _, extra := range []string{
		"RootDir=/tmp/other\n",
		"DPkg::Options::=--admindir%3d/tmp/other\n",
		"DPkg::Options::=--root%3d/tmp/other\n",
		"DPkg::Chroot-Directory=/tmp/other\n",
		"Binary::apt-get::Dir::State::status=/tmp/other/status\n",
		"Binary::apt-get::Debug::NoLocking=true\n",
		"Debug::NoLocking=true\n",
		"Debug::pkgDPkgPM=true\n",
		"DIR::STATE::STATUS=/tmp/other/status\n",
		"Dir%3a%3aState%3a%3astatus=/tmp/other/status\n",
	} {
		if err := validateCodexAPTConfig(base+extra, "/usr/bin/dpkg"); err == nil {
			t.Fatalf("accepted redirect/lock override %q", extra)
		}
	}
	for _, bad := range []string{
		strings.Replace(base, "Dir::State::status=/var/lib/dpkg/status", "Dir::State::status=/tmp/other/status", 1),
		strings.Replace(base, "Dir=/", "Dir=/tmp/other", 1),
		strings.Replace(base, "Dir::Bin::dpkg=/usr/bin/dpkg", "Dir::Bin::dpkg=/tmp/custom-dpkg", 1),
	} {
		if err := validateCodexAPTConfig(bad, "/usr/bin/dpkg"); err == nil {
			t.Fatal("accepted alternate package database/executable")
		}
	}
	for _, cfg := range []string{"admindir /tmp/other", "root=/tmp/other", "--root /tmp/other", "instdir /tmp/other", "force-not-root", "status-logger /tmp/other"} {
		if err := validateCodexDpkgConfig(cfg); err == nil {
			t.Fatalf("accepted dpkg configuration %q", cfg)
		}
	}
}
