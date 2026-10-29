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
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestCodexRPMMetadata(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		rpmArch, err := codexRPMArch(arch)
		if err != nil {
			t.Fatal(err)
		}
		for _, version := range []string{"2026.1029.1", "vNext", "1.0~rc1", "1.0^git20261029", "a_b+c.9", strings.Repeat("a", 256)} {
			want := codexRPMMetadata{"chatgpt", "0", version, "1.fc43", rpmArch}
			got, err := parseCodexRPMMetadata("chatgpt\t0\t"+version+"\t1.fc43\t"+rpmArch+"\n", arch)
			if err != nil || got != want {
				t.Fatalf("%s: got %+v %v, want %+v", arch, got, err, want)
			}
		}
	}
	good := "chatgpt\t0\t1.0\t1\tx86_64\n"
	bad := []string{"", strings.TrimSuffix(good, "\n"), good + "\n", good + good, " " + good,
		strings.ReplaceAll(good, "\t", " "), strings.Replace(good, "\n", "\r\n", 1),
		strings.Replace(good, "chatgpt", "other", 1), strings.Replace(good, "x86_64", "noarch", 1),
		strings.Replace(good, "x86_64", "aarch64", 1), strings.Repeat("x", 1025) + "\n"}
	for _, epoch := range []string{"", "00", "01", "+1", "-1", "(none)", "1.0", "4294967296", " 0", "0\x00"} {
		bad = append(bad, "chatgpt\t"+epoch+"\t1.0\t1\tx86_64\n")
	}
	for _, part := range []string{"", "-1", "1-2", "1:2", "1/2", "1 2", "1\t2", "1\n2", "1\x00", "1;id", "1%{lua:x}", "1\u00e9", strings.Repeat("a", 257)} {
		bad = append(bad, "chatgpt\t0\t"+part+"\t1\tx86_64\n", "chatgpt\t0\t1\t"+part+"\tx86_64\n")
	}
	for _, output := range bad {
		if m, err := parseCodexRPMMetadata(output, "amd64"); err == nil || m != (codexRPMMetadata{}) {
			t.Fatalf("accepted bad metadata %q: %+v %v", output, m, err)
		}
	}
	if _, err := parseCodexRPMMetadata(strings.Replace(good, "\t0\t", "\t4294967295\t", 1), "amd64"); err != nil {
		t.Fatal(err)
	}
	for _, arch := range []string{"", "386", "x86_64", "aarch64", "AMD64", "amd64/other"} {
		if _, err := codexRPMArch(arch); err == nil {
			t.Fatalf("accepted arch %q", arch)
		}
		if _, err := codexRPMURL(arch); err == nil {
			t.Fatalf("accepted URL arch %q", arch)
		}
		if _, err := parseCodexRPMMetadata(good, arch); err == nil {
			t.Fatalf("accepted metadata arch %q", arch)
		}
	}
}

func TestCodexRPMQueries(t *testing.T) {
	const output = "chatgpt\t0\t1.0~rc1\t1.fc43\tx86_64\n"
	for _, installed := range []bool{false, true} {
		for _, mode := range []string{"success", "empty", "multiple", "wrong-arch", "resolve-error", "run-error", "cancel-before", "cancel-resolve", "cancel-run", "parent-deadline"} {
			t.Run(fmt.Sprintf("installed=%t/%s", installed, mode), func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if mode == "parent-deadline" {
					var stop context.CancelFunc
					ctx, stop = context.WithTimeout(ctx, time.Minute/10)
					defer stop()
				}
				if mode == "cancel-before" {
					cancel()
				}
				failure := errors.New("injected query failure")
				resolved, ran := 0, 0
				s := &systemTools{resolve: func(name string) (string, error) {
					resolved++
					if name != "rpm" {
						t.Fatalf("unexpected tool %s", name)
					}
					if mode == "resolve-error" {
						return "", failure
					}
					if mode == "cancel-resolve" {
						cancel()
					}
					return "/trusted/rpm", nil
				}, run: func(runCtx context.Context, ex string, args []string) (string, error) {
					ran++
					want := []string{"-qp", "--queryformat", "%{NAME}\t%{EPOCHNUM}\t%{VERSION}\t%{RELEASE}\t%{ARCH}\n", "--", "/tmp/package with spaces.rpm"}
					if installed {
						want[0], want[4] = "-qa", "chatgpt"
					}
					if ex != "/trusted/rpm" || !reflect.DeepEqual(args, want) {
						t.Fatalf("unexpected command %s %#v", ex, args)
					}
					deadline, ok := runCtx.Deadline()
					limit := 20 * time.Second
					if mode == "parent-deadline" {
						limit = time.Minute / 10
					}
					if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > limit {
						t.Fatal("query deadline missing or exceeds bound")
					}
					switch mode {
					case "run-error":
						return "", failure
					case "cancel-run":
						cancel()
					case "empty":
						return "", nil
					case "multiple":
						return output + output, nil
					case "wrong-arch":
						return strings.Replace(output, "x86_64", "aarch64", 1), nil
					}
					return output, nil
				}}
				s.quietRun = s.run
				var m codexRPMMetadata
				var present bool
				var err error
				if installed {
					m, present, err = installedCodexRPM(ctx, s, "amd64")
				} else {
					m, err = queryCodexRPM(ctx, s, "/tmp/package with spaces.rpm", "amd64")
				}
				success := mode == "success" || mode == "parent-deadline" || installed && mode == "empty"
				if (err == nil) != success {
					t.Fatalf("unexpected result %+v %t %v", m, present, err)
				}
				if !success || mode == "empty" {
					if m != (codexRPMMetadata{}) || present {
						t.Fatal("published metadata/presence without successful record")
					}
				} else if m.Name != "chatgpt" || installed && !present {
					t.Fatal("lost successful record")
				}
				if strings.HasPrefix(mode, "cancel-") && !errors.Is(err, context.Canceled) {
					t.Fatalf("lost cancellation: %v", err)
				}
				if (mode == "resolve-error" || mode == "run-error") && !errors.Is(err, failure) {
					t.Fatalf("lost query error: %v", err)
				}
				if mode == "cancel-before" && resolved != 0 || (mode == "cancel-before" || mode == "cancel-resolve" || mode == "resolve-error") && ran != 0 {
					t.Fatal("query proceeded after failed preflight")
				}
			})
		}
	}
	s := &systemTools{resolve: func(string) (string, error) { t.Fatal("invalid input resolved a tool"); return "", nil }}
	for _, path := range []string{"chatgpt.rpm", "--eval=x", "/tmp/../chatgpt.rpm", "/tmp/chatgpt.deb", "/tmp/a\n.rpm"} {
		if _, err := queryCodexRPM(context.Background(), s, path, "amd64"); err == nil {
			t.Fatalf("accepted unsafe path %q", path)
		}
	}
	if _, _, err := installedCodexRPM(context.Background(), s, "386"); err == nil {
		t.Fatal("accepted unsupported installed architecture")
	}
}

func TestCodexRPMDownload(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		rpmArch, _ := codexRPMArch(arch)
		wantURL := "https://persistent.oaistatic.com/codex-app-prod/linux/rpm/latest/chatgpt." + rpmArch + ".rpm"
		gotURL, err := codexRPMURL(arch)
		if err != nil || gotURL != wantURL {
			t.Fatalf("wrong URL %q %v", gotURL, err)
		}
		u, _ := url.Parse(wantURL)
		jar, _ := cookiejar.New(nil)
		jar.SetCookies(u, []*http.Cookie{{Name: "private", Value: "must-not-send"}})
		calls := 0
		b := &backend{arch: arch, client: &http.Client{Jar: jar, Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			if r.URL.String() != wantURL || r.Method != "GET" || r.Header.Get("Accept-Encoding") != "identity" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
				t.Fatalf("unexpected request %s %s %v", r.Method, r.URL, r.Header)
			}
			deadline, ok := r.Context().Deadline()
			if !ok || time.Until(deadline) > 5*time.Minute {
				t.Fatal("download deadline missing or excessive")
			}
			return &http.Response{StatusCode: 200, ContentLength: 7, Header: http.Header{"Content-Encoding": {"identity"}}, Body: io.NopCloser(strings.NewReader("payload")), Request: r}, nil
		})}}
		var dst bytes.Buffer
		n, sum, err := b.downloadCodexRPM(context.Background(), &dst)
		if err != nil || n != 7 || dst.String() != "payload" || sum != fmt.Sprintf("%x", sha256.Sum256([]byte("payload"))) || calls != 1 || b.client.Jar != jar {
			t.Fatalf("download failed: n=%d hash=%q err=%v", n, sum, err)
		}
	}
}

func TestCodexRPMDownloadFailures(t *testing.T) {
	for _, mode := range []string{"unsupported", "cancel-before", "redirect", "status", "empty", "unknown-length", "oversized", "gzip", "decoded", "chunked", "short", "long", "reader-error", "writer-error", "cancel-read", "transport-error"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			failure := errors.New("injected I/O error")
			calls := 0
			b := &backend{arch: "amd64", client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				resp := &http.Response{StatusCode: 200, ContentLength: 7, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("payload")), Request: r}
				switch mode {
				case "redirect":
					resp.StatusCode = 302
					resp.Header.Set("Location", "https://example.invalid/package.rpm")
				case "status":
					resp.StatusCode = 206
				case "empty":
					resp.ContentLength = 0
				case "unknown-length":
					resp.ContentLength = -1
				case "oversized":
					resp.ContentLength = (512 << 20) + 1
				case "gzip":
					resp.Header.Set("Content-Encoding", "gzip")
				case "decoded":
					resp.Uncompressed = true
				case "chunked":
					resp.TransferEncoding = []string{"chunked"}
				case "short":
					resp.ContentLength = 8
				case "long":
					resp.ContentLength = 1
				case "reader-error", "cancel-read":
					resp.Body = io.NopCloser(codexReviewReader(func(p []byte) (int, error) {
						n := copy(p, "payload")
						if mode == "cancel-read" {
							cancel()
							return n, io.EOF
						}
						return n, failure
					}))
				case "transport-error":
					return nil, failure
				}
				return resp, nil
			})}}
			if mode == "unsupported" {
				b.arch = "386"
			}
			if mode == "cancel-before" {
				cancel()
			}
			var dst bytes.Buffer
			var writer io.Writer = &dst
			if mode == "writer-error" {
				writer = codexReviewWriter(func([]byte) (int, error) { return 0, failure })
			}
			n, sum, err := b.downloadCodexRPM(ctx, writer)
			if err == nil || n != 0 || sum != "" {
				t.Fatalf("published failed download: %d %q %v", n, sum, err)
			}
			if (mode == "cancel-before" || mode == "cancel-read") && !errors.Is(err, context.Canceled) {
				t.Fatalf("lost cancellation: %v", err)
			}
			if (mode == "reader-error" || mode == "writer-error" || mode == "transport-error") && !errors.Is(err, failure) {
				t.Fatalf("lost I/O error: %v", err)
			}
			if calls > 1 || (mode == "unsupported" || mode == "cancel-before") && calls != 0 {
				t.Fatal("unexpected network attempt or followed redirect")
			}
			switch mode {
			case "short", "reader-error", "cancel-read":
			case "long":
				if dst.Len() != 2 {
					t.Fatal("read beyond declared length plus one")
				}
			default:
				if dst.Len() != 0 {
					t.Fatal("invalid response wrote payload")
				}
			}
		})
	}
}
