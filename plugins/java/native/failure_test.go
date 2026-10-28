//go:build linux

package main

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVendorRedirectPolicies(t *testing.T) {
	cases := []struct {
		vendor, source, target string
		allowed                bool
	}{
		{"openjdk", "https://download.java.net/start", "https://download.java.net/archive", true},
		{"openjdk", "https://download.java.net/start", "https://download.oracle.com/archive", false},
		{"corretto", "https://corretto.aws/start", "https://corretto.aws/downloads/resources/archive", true},
		{"corretto", "https://corretto.aws/start", "https://evil.invalid/archive", false},
		{"microsoft", "https://aka.ms/start", "https://download.visualstudio.microsoft.com/archive", true},
		{"microsoft", "https://aka.ms/start", "https://download.visualstudio.microsoft.com.evil.invalid/archive", false},
		{"graalvm", "https://download.oracle.com/start", "https://download.oracle.com/archive", true},
		{"graalvm", "https://download.oracle.com/start", "http://download.oracle.com/archive", false},
		{"temurin", "https://github.com/adoptium/temurin26-binaries/releases/download/start", "https://release-assets.githubusercontent.com/archive?token=fixture", true},
		{"temurin", "https://github.com/adoptium/temurin26-binaries/releases/download/start", "https://objects.githubusercontent.com/archive", true},
		{"temurin", "https://api.adoptium.net/start", "https://api.adoptium.net/archive", false},
		{"temurin", "https://github.com/adoptium/temurin26-binaries/releases/download/start", "https://github.com/unrelated/release", false},
		{"custom", "https://custom.invalid/start", "https://custom.invalid/archive", true},
		{"custom", "https://custom.invalid/start", "https://other.invalid/archive", false},
		{"custom", "https://custom.invalid/start", "https://user@custom.invalid/archive", false},
	}
	for _, tc := range cases {
		t.Run(tc.vendor+"/"+tc.target, func(t *testing.T) {
			b := newTestBackend(t)
			tlsFixture(t, b, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/start") {
					http.Redirect(w, r, tc.target, 302)
					return
				}
				fmt.Fprint(w, "archive")
			})
			p := policy{vendor: tc.vendor}
			if tc.vendor == "custom" {
				p.custom = true
				p.origin = "https://custom.invalid"
			}
			var data bytes.Buffer
			final, _, err := b.fetch(context.Background(), tc.source, p, &data, 100)
			if tc.allowed && (err != nil || data.String() != "archive" || final != tc.target) {
				t.Fatal(final, err, data.String())
			}
			if !tc.allowed && err == nil {
				t.Fatal("unsafe redirect accepted")
			}
		})
	}
}

func TestTemurinRealShapeAndMetadataRefusals(t *testing.T) {
	for _, kind := range []string{"offline", "duplicate", "empty", "preview", "vendor", "arch", "jre", "url", "checksum", "size", "missing-major"} {
		t.Run(kind, func(t *testing.T) {
			b := newTestBackend(t)
			data := archiveBytes(t, b.platform, "26.0.2.1")
			row := temurinMetadata(b.platform, "26.0.2.1", data)
			rows := []any{row}
			bin := row["binary"].(map[string]any)
			pkg := bin["package"].(map[string]any)
			switch kind {
			case "duplicate":
				rows = append(rows, row)
			case "empty":
				rows = nil
			case "preview":
				row["release_name"] = "jdk-27-ea+4"
			case "vendor":
				row["vendor"] = "unknown"
			case "arch":
				bin["architecture"] = "ppc64"
			case "jre":
				bin["image_type"] = "jre"
			case "url":
				pkg["link"] = "https://evil.invalid/archive.tar.gz"
			case "checksum":
				pkg["checksum"] = "bad"
			case "size":
				pkg["size"] = limits.DownloadBytes + 1
			case "missing-major":
				delete(row, "version")
			}
			tlsFixture(t, b, func(w http.ResponseWriter, r *http.Request) {
				if kind == "offline" {
					http.Error(w, "forbidden", 403)
					return
				}
				_ = json.NewEncoder(w).Encode(rows)
			})
			o, _ := parseOptions([]string{"--vendor", "temurin"})
			if _, err := b.resolve(context.Background(), o); err == nil {
				t.Fatal("invalid metadata accepted")
			}
			assertAbsent(t, b.root)
		})
	}
}

func TestChecksumAndPayloadFailuresPreserveCurrent(t *testing.T) {
	for _, kind := range []string{"bad-hash", "duplicate-hash", "wrong-hash", "wrong-filename", "truncated", "traversal", "absolute", "outside-root", "duplicate", "symlink-escape", "hardlink-escape", "link-cycle", "link-parent", "fifo", "suid", "missing-javac", "wrong-arch", "release-injection", "release-duplicate", "wrong-version", "official-ea", "reserved"} {
		t.Run(kind, func(t *testing.T) {
			b := newTestBackend(t)
			legacy(t, b, "temurin/21", "21.0.4")
			if err := action(b, "use", "temurin/21"); err != nil {
				t.Fatal(err)
			}
			data := archiveBytes(t, b.platform, "26.0.1")
			switch kind {
			case "traversal":
				data = archiveBytes(t, b.platform, "26.0.1", tarItem{"jdk/../../outside", "x", "", tar.TypeReg, 0644})
			case "absolute":
				data = archiveBytes(t, b.platform, "26.0.1", tarItem{"/outside", "x", "", tar.TypeReg, 0644})
			case "outside-root":
				data = archiveBytes(t, b.platform, "26.0.1", tarItem{"other/file", "x", "", tar.TypeReg, 0644})
			case "duplicate":
				data = archiveBytes(t, b.platform, "26.0.1", tarItem{"jdk/bin/java", "x", "", tar.TypeReg, 0755})
			case "symlink-escape":
				data = archiveBytes(t, b.platform, "26.0.1", tarItem{"jdk/lib/link", "", "../../outside", tar.TypeSymlink, 0777})
			case "hardlink-escape":
				data = archiveBytes(t, b.platform, "26.0.1", tarItem{"jdk/lib/link", "", "other/file", tar.TypeLink, 0644})
			case "link-cycle":
				data = archiveBytes(t, b.platform, "26.0.1", tarItem{"jdk/lib/a", "", "b", tar.TypeSymlink, 0777}, tarItem{"jdk/lib/b", "", "a", tar.TypeSymlink, 0777})
			case "link-parent":
				data = archiveBytes(t, b.platform, "26.0.1", tarItem{"jdk/link", "", "lib", tar.TypeSymlink, 0777}, tarItem{"jdk/link/child", "", "../lib/data", tar.TypeSymlink, 0777})
			case "fifo":
				data = archiveBytes(t, b.platform, "26.0.1", tarItem{"jdk/pipe", "", "", tar.TypeFifo, 0600})
			case "suid":
				data = archiveBytes(t, b.platform, "26.0.1", tarItem{"jdk/privileged", "x", "", tar.TypeReg, 04755})
			case "missing-javac":
				data = archiveItems(t, []tarItem{{"jdk/bin/java", string(elfBytes(b.platform)), "", tar.TypeReg, 0755}, {"jdk/release", "JAVA_VERSION=\"26.0.1\"", "", tar.TypeReg, 0644}})
			case "wrong-arch":
				arch := "aarch64"
				if b.platform == arch {
					arch = "x64"
				}
				data = archiveBytes(t, arch, "26.0.1")
			case "release-injection":
				data = archiveBytes(t, b.platform, "$(touch /tmp/never-execute-java)")
			case "release-duplicate":
				data = archiveBytes(t, b.platform, "26.0.1\"\nJAVA_VERSION=\"26.0.1")
			case "wrong-version":
				data = archiveBytes(t, b.platform, "25.0.1")
			case "official-ea":
				data = archiveBytes(t, b.platform, "26.0.1-ea")
			case "reserved":
				data = archiveBytes(t, b.platform, "26.0.1", tarItem{"jdk/" + receiptName, "{}", "", tar.TypeReg, 0644})
			case "truncated":
				data = data[:len(data)-6]
			}
			sum := fmt.Sprintf("%x", sha256.Sum256(data))
			switch kind {
			case "bad-hash":
				sum = "bad"
			case "duplicate-hash":
				sum += "\n" + sum
			case "wrong-hash":
				sum = strings.Repeat("0", 64)
			case "wrong-filename":
				sum += "  other.tar.gz"
			}
			tlsFixture(t, b, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, ".sha256") {
					fmt.Fprint(w, sum)
					return
				}
				_, _ = w.Write(data)
			})
			if err := action(b, "install"); err == nil {
				t.Fatal("unsafe JDK accepted")
			}
			assertCurrent(t, b, "temurin/21")
			assertAbsent(t, filepath.Join(b.root, "openjdk/26.0.1"))
			assertClean(t, b)
		})
	}
}

func TestCancellationBoundsAndLegacyConflict(t *testing.T) {
	b := newTestBackend(t)
	legacy(t, b, "temurin/21", "21.0.4")
	if err := action(b, "use", "temurin/21"); err != nil {
		t.Fatal(err)
	}
	data := archiveBytes(t, b.platform, "26.0.1")
	sum := fmt.Sprintf("%x", sha256.Sum256(data))
	tlsFixture(t, b, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			fmt.Fprint(w, sum)
			return
		}
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	b.req.Operation = "install"
	b.req.Arguments = nil
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := b.execute(ctx); err == nil {
		t.Fatal("cancelled download succeeded")
	}
	assertCurrent(t, b, "temurin/21")
	assertClean(t, b)
	tlsFixture(t, b, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, strings.Repeat("x", 100)) })
	if _, _, err := b.fetch(context.Background(), openjdkBase+"file", policy{vendor: "openjdk"}, io.Discard, 10); err == nil {
		t.Fatal("HTTP budget ignored")
	}
	legacy(t, b, "openjdk/26.0.1", "26.0.1")
	vendorFixture(t, b, "openjdk", "26.0.1")
	if err := action(b, "install"); err == nil {
		t.Fatal("legacy adopted")
	}
	assertAbsent(t, filepath.Join(b.root, "openjdk/26.0.1", receiptName))
	assertCurrent(t, b, "temurin/21")
}

func TestJavaArchiveBudgets(t *testing.T) {
	for _, kind := range []string{"file", "expanded", "entries"} {
		t.Run(kind, func(t *testing.T) {
			data := archiveBytes(t, "x64", "26.0.1")
			old := limits
			defer func() { limits = old }()
			switch kind {
			case "file":
				limits.FileBytes = 16
			case "expanded":
				limits.ExpandedBytes = 16
			case "entries":
				limits.Entries = 1
			}
			archive := filepath.Join(t.TempDir(), "archive.tar.gz")
			write(t, archive, data, 0600)
			root, err := os.OpenRoot(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			if _, err = extractArchive(context.Background(), archive, root); err == nil {
				t.Fatal("archive budget ignored")
			}
		})
	}
}
