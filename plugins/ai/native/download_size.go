//go:build linux

package main

import (
	"fmt"
	"io"
	"net/http"
)

// The shared archive manager enforces an upper bound; this adapter also rejects
// short bodies, including chunked responses without a Content-Length header.
type exactSizeTransport struct {
	base http.RoundTripper
	size int64
}

func (t exactSizeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	resp, err := base.RoundTrip(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		return resp, err
	}
	if resp.ContentLength >= 0 && resp.ContentLength != t.size {
		resp.Body.Close()
		return nil, fmt.Errorf("artifact Content-Length differs from release size")
	}
	resp.Body = &exactSizeBody{ReadCloser: resp.Body, expected: t.size}
	return resp, nil
}

type exactSizeBody struct {
	io.ReadCloser
	expected, read int64
}

func (b *exactSizeBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.read += int64(n)
	if b.read > b.expected {
		return n, fmt.Errorf("artifact exceeds release size")
	}
	if err == io.EOF && b.read != b.expected {
		return n, io.ErrUnexpectedEOF
	}
	return n, err
}
