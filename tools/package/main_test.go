package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"io"
	"testing"
)

func TestArchives(t *testing.T) {
	files := []payload{{"atm", []byte("binary"), 0755}, {"plugins/node/plugin.json", []byte("{}"), 0644}}
	for _, windows := range []bool{false, true} {
		var b bytes.Buffer
		if err := archive(&b, "atm-test", files, windows); err != nil {
			t.Fatal(err)
		}
		if windows {
			z, err := zip.NewReader(bytes.NewReader(b.Bytes()), int64(b.Len()))
			if err != nil {
				t.Fatal(err)
			}
			if len(z.File) != 2 || z.File[0].Name != "atm-test/atm" || z.File[0].Mode().Perm() != 0755 {
				t.Fatal("invalid ZIP")
			}
		} else {
			gz, err := gzip.NewReader(&b)
			if err != nil {
				t.Fatal(err)
			}
			defer gz.Close()
			tr := tar.NewReader(gz)
			h, err := tr.Next()
			if err != nil || h.Name != "atm-test/atm" || h.Mode != 0755 {
				t.Fatal(h, err)
			}
			data, err := io.ReadAll(tr)
			if err != nil || string(data) != "binary" {
				t.Fatal(string(data), err)
			}
		}
	}
}
