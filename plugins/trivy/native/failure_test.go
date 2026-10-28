//go:build linux

package main

import (
	"archive/tar"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCancelledBinaryDownloadPreservesSelection(t *testing.T) {
	b := newTestBackend(t)
	legacy(t, b, "0.75.0")
	if err := action(b, "use", "0.75.0"); err != nil {
		t.Fatal(err)
	}
	data := archiveBytes(t, b.platform)
	row, sums := releaseData("0.76.1", b.platform, data)
	tlsFixture(t, b, func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "api.github.com" {
			_ = json.NewEncoder(w).Encode(row)
			return
		}
		if strings.HasSuffix(r.URL.Path, "_checksums.txt") {
			_, _ = w.Write([]byte(sums))
			return
		}
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	b.req.Operation = "install"
	b.req.Arguments = []string{"--method", "binary"}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := b.execute(ctx); err == nil {
		t.Fatal("cancelled install succeeded")
	}
	assertCurrent(t, b, "0.75.0")
	assertAbsent(t, filepath.Join(b.root, "0.76.1"))
	assertClean(t, b)
}

func TestArchiveResourceBudgets(t *testing.T) {
	for _, kind := range []string{"entries", "file", "expanded"} {
		t.Run(kind, func(t *testing.T) {
			old := limits
			defer func() { limits = old }()
			data := archiveBytes(t, "Linux-64bit", tarItem{"extra", "data", "", tar.TypeReg, 0644})
			switch kind {
			case "entries":
				limits.Entries = 1
			case "file":
				limits.FileBytes = 16
			case "expanded":
				limits.ExpandedBytes = 16
			}
			archive := filepath.Join(t.TempDir(), "archive.tar.gz")
			write(t, archive, data, 0600)
			root, err := os.OpenRoot(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			if err := extractArchive(context.Background(), archive, root); err == nil {
				t.Fatal("resource budget ignored", kind)
			}
		})
	}
}

func TestNoDowngradeAndStrictArguments(t *testing.T) {
	b := newTestBackend(t)
	legacy(t, b, "0.77.0")
	if err := action(b, "use", "0.77.0"); err != nil {
		t.Fatal(err)
	}
	releasesFixture(t, b, "0.76.1")
	if err := action(b, "update", "--method", "binary"); err == nil {
		t.Fatal("downgraded active binary")
	}
	assertCurrent(t, b, "0.77.0")
	for _, args := range [][]string{{"--method", "shell"}, {"--version", "../bad"}, {"--version", "0.1.2-rc1"}, {"--method"}, {"--method", "binary", "--method", "docker"}, {"--version", "0.1.2", "extra"}} {
		if _, err := parseOptions(args); err == nil {
			t.Fatal("invalid options accepted", args)
		}
	}
	root := dockerFixture(t, b)
	if err := action(b, "install", "--method", "docker"); err != nil {
		t.Fatal(err)
	}
	releasesFixture(t, b, "0.75.0")
	first := len(dockerCalls(t, root))
	if err := action(b, "update", "--method", "docker"); err == nil {
		t.Fatal("downgraded Docker selection")
	}
	forbidMutations(t, dockerCalls(t, root)[first:])
}
