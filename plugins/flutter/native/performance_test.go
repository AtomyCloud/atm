//go:build linux

package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"

	"github.com/ulikunitz/xz"
)

type countedXZInput struct {
	io.Reader
	reads int
}

func (r *countedXZInput) Read(p []byte) (int, error) {
	r.reads++
	return r.Reader.Read(p)
}

func TestXZReaderBatchesInput(t *testing.T) {
	block := make([]byte, 4096)
	state := uint32(1)
	for i := range block {
		state ^= state << 13
		state ^= state >> 17
		state ^= state << 5
		block[i] = byte(state)
	}
	want := bytes.Repeat(block, 32)
	var compressed bytes.Buffer
	w, err := xz.NewWriter(&compressed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Write(want); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	input := &countedXZInput{Reader: bytes.NewReader(compressed.Bytes())}
	r, err := newXZReader(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatal("decoded data mismatch", err)
	}
	if limit := compressed.Len()/(64<<10) + 3; input.reads > limit {
		t.Fatalf("decoder performed %d source reads for %d compressed bytes (limit %d)", input.reads, compressed.Len(), limit)
	}
	for _, kind := range []string{"truncated", "trailing", "concatenated", "corrupt", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			data := append([]byte(nil), compressed.Bytes()...)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch kind {
			case "truncated":
				data = data[:len(data)-1]
			case "trailing":
				data = append(data, 1)
			case "concatenated":
				data = append(data, data...)
			case "corrupt":
				data[len(data)-20] ^= 1
			}
			r, err := newXZReader(ctx, bytes.NewReader(data))
			if kind == "cancelled" {
				cancel()
			}
			if err == nil {
				_, err = io.Copy(io.Discard, r)
			}
			if err == nil {
				t.Fatal("invalid/cancelled stream accepted")
			}
		})
	}
}

// Opt-in read-only microbenchmark. Decode a bounded prefix from a trusted local
// archive; no full SDK extraction, network, provider execution or installation.
func BenchmarkFlutterXZDecodePrefix(b *testing.B) {
	archive := os.Getenv("ATM_FLUTTER_BENCH_ARCHIVE")
	if archive == "" {
		b.Skip("set ATM_FLUTTER_BENCH_ARCHIVE to a trusted Flutter tar.xz")
	}
	const size = 16 << 20
	b.SetBytes(size)
	for i := 0; i < b.N; i++ {
		f, err := os.Open(archive)
		if err != nil {
			b.Fatal(err)
		}
		r, err := newXZReader(context.Background(), f)
		if err == nil {
			_, err = io.CopyN(io.Discard, r, size)
		}
		closeErr := f.Close()
		if err != nil {
			b.Fatal(err)
		}
		if closeErr != nil {
			b.Fatal(closeErr)
		}
	}
}
