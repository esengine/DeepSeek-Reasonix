package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/desktop/internal/update"
)

func TestNormalizeVersion(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"dev", "", false},
		{"", "", false},
		{"  ", "", false},
		{"1.2.3", "v1.2.3", true},
		{"v1.2.3", "v1.2.3", true},
		{"v1.2", "v1.2.0", true}, // semver.Canonical fills the patch
		{"garbage", "", false},
	}
	for _, c := range cases {
		got, ok := normalizeVersion(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("normalizeVersion(%q) = (%q,%v), want (%q,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestEvaluate(t *testing.T) {
	mk := func(version string) *update.Manifest {
		return &update.Manifest{
			Version:   version,
			Notes:     "notes",
			Platforms: map[string]update.Asset{update.CurrentPlatform(): {Size: 999}},
		}
	}

	if got := evaluate("v1.0.0", mk("v1.1.0")); !got.Available {
		t.Error("v1.0.0 -> v1.1.0 should be available")
	}
	if got := evaluate("v1.1.0", mk("v1.1.0")); got.Available {
		t.Error("same version should not be available")
	}
	if got := evaluate("v1.2.0", mk("v1.1.0")); got.Available {
		t.Error("newer-than-manifest should not be available")
	}
	// A dev build must never auto-prompt, even against a real release.
	if got := evaluate("dev", mk("v1.1.0")); got.Available {
		t.Error("dev build should not prompt to update")
	}
	// An invalid manifest version is a check error, not an update.
	got := evaluate("v1.0.0", mk("not-a-version"))
	if got.Available || got.Err == "" {
		t.Errorf("invalid manifest version: got %+v", got)
	}
	// Metadata carries through.
	full := evaluate("v1.0.0", mk("v1.1.0"))
	if full.Latest != "v1.1.0" || full.Notes != "notes" || full.AssetSize != 999 {
		t.Errorf("metadata not carried: %+v", full)
	}
	if full.CanSelfUpdate != (runtime.GOOS != "darwin") {
		t.Errorf("CanSelfUpdate = %v on %s", full.CanSelfUpdate, runtime.GOOS)
	}
}

func TestChannelSelectsDistinctPointers(t *testing.T) {
	orig := channel
	t.Cleanup(func() { channel = orig })

	channel = "stable"
	stable := manifestEndpoints()
	channel = "canary"
	canary := manifestEndpoints()

	for _, u := range stable {
		if strings.Contains(u, "canary") {
			t.Errorf("stable endpoint leaks into canary: %q", u)
		}
	}
	if !strings.Contains(stable[0], "/latest/latest.json") {
		t.Errorf("stable primary = %q, want the latest/ pointer", stable[0])
	}
	for _, u := range canary {
		if strings.Contains(u, "/latest/") {
			t.Errorf("canary endpoint hits the stable latest/ pointer: %q", u)
		}
	}
	if !strings.Contains(canary[0], "/canary/latest.json") {
		t.Errorf("canary primary = %q, want the canary/ pointer", canary[0])
	}
	if downloadPage() == (ghReleasesBase + "/latest") {
		t.Error("canary download page should not be the stable latest releases page")
	}
}

func TestCheckSHA256(t *testing.T) {
	data := []byte("hello world")
	// echo -n "hello world" | shasum -a 256
	const sum = "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	if err := checkSHA256(data, sum); err != nil {
		t.Errorf("matching digest should pass: %v", err)
	}
	if err := checkSHA256(data, "deadbeef"); err == nil {
		t.Error("mismatched digest should fail")
	}
	// Case-insensitive hex.
	if err := checkSHA256(data, "B94D27B9934D3E08A52E52D7DA7DABFAC484EFE37A5380EE9088F7ACE2EFCDE9"); err != nil {
		t.Errorf("uppercase digest should pass: %v", err)
	}
}

func TestExtractBinary(t *testing.T) {
	want := []byte("#!/bin/sh\necho reasonix\n")
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	files := map[string][]byte{"README": []byte("ignore me"), "reasonix-desktop": want}
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	gz.Close()

	got, err := extractBinary(buf.Bytes(), "reasonix-desktop")
	if err != nil {
		t.Fatalf("extractBinary: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("extracted %q, want %q", got, want)
	}
	if _, err := extractBinary(buf.Bytes(), "missing"); err == nil {
		t.Error("missing entry should error")
	}
}

func fastRetry(t *testing.T) {
	t.Helper()
	restore := retryBackoff
	retryBackoff = func(int) time.Duration { return time.Millisecond }
	t.Cleanup(func() { retryBackoff = restore })
}

func TestDownloadRecoversFromMidStreamReset(t *testing.T) {
	fastRetry(t)
	const body = "complete-installer-bytes"
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&calls, 1) < int32(downloadAttempts) {
			// Mid-stream reset: promise 100 bytes, send a few, drop the socket —
			// the client's body read fails with unexpected EOF, exactly the CN-IPv6
			// "forcibly closed" case the retry exists for.
			conn, bw, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Errorf("hijack: %v", err)
				return
			}
			bw.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 100\r\n\r\npartial")
			bw.Flush()
			conn.Close()
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	data, err := download(context.Background(), srv.Client(), nil, srv.URL, 0, nil)
	if err != nil {
		t.Fatalf("download should recover after %d resets: %v", downloadAttempts-1, err)
	}
	if string(data) != body {
		t.Fatalf("got %q, want %q", data, body)
	}
	if n := atomic.LoadInt32(&calls); n != int32(downloadAttempts) {
		t.Fatalf("made %d attempts, want %d", n, downloadAttempts)
	}
}

func TestDownloadGivesUpAfterCap(t *testing.T) {
	fastRetry(t)
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		conn.Close()
	}))
	defer srv.Close()

	if _, err := download(context.Background(), srv.Client(), nil, srv.URL, 0, nil); err == nil {
		t.Fatal("download should fail after exhausting retries")
	}
	if n := atomic.LoadInt32(&calls); n != int32(downloadAttempts) {
		t.Fatalf("made %d attempts, want %d", n, downloadAttempts)
	}
}

func TestRetryTransientStopsWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	if err := retryTransient(ctx, func(int) error {
		calls++
		return errors.New("boom")
	}); err == nil {
		t.Fatal("cancelled retry should return the error")
	}
	if calls != 1 {
		t.Fatalf("cancelled retry made %d calls, want 1", calls)
	}
}

func TestDownloadResumesWithRange(t *testing.T) {
	fastRetry(t)
	full := bytes.Repeat([]byte("0123456789"), 50) // 500 bytes
	const cut = 200
	var calls int32
	rangeCh := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			// First attempt: promise the whole file, send a prefix, drop the socket.
			conn, bw, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Errorf("hijack: %v", err)
				return
			}
			fmt.Fprintf(bw, "HTTP/1.1 200 OK\r\nContent-Length: %d\r\n\r\n", len(full))
			bw.Write(full[:cut])
			bw.Flush()
			conn.Close()
			return
		}
		// Resume attempt: honor the Range header with a 206 + Content-Range.
		rng := r.Header.Get("Range")
		rangeCh <- rng
		start := 0
		fmt.Sscanf(rng, "bytes=%d-", &start)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(full)-1, len(full)))
		w.WriteHeader(http.StatusPartialContent)
		w.Write(full[start:])
	}))
	defer srv.Close()

	data, err := download(context.Background(), srv.Client(), nil, srv.URL, 0, nil)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if !bytes.Equal(data, full) {
		t.Fatalf("assembled %d bytes, want %d (equal=%v)", len(data), len(full), bytes.Equal(data, full))
	}
	select {
	case rng := <-rangeCh:
		if rng != fmt.Sprintf("bytes=%d-", cut) {
			t.Fatalf("resume Range = %q, want bytes=%d-", rng, cut)
		}
	default:
		t.Fatal("resume attempt sent no Range header")
	}
}

func TestDownloadFallsBackToSecondClient(t *testing.T) {
	fastRetry(t)
	const body = "served-over-ipv4"
	primary := &http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("connection reset (ipv6)")
	})}
	var fbCalls int32
	fallback := &http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) {
		atomic.AddInt32(&fbCalls, 1)
		return &http.Response{
			StatusCode:    http.StatusOK,
			Body:          io.NopCloser(strings.NewReader(body)),
			ContentLength: int64(len(body)),
			Header:        make(http.Header),
		}, nil
	})}

	data, err := download(context.Background(), primary, fallback, "http://example.invalid/x", 0, nil)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if string(data) != body {
		t.Fatalf("got %q, want %q", data, body)
	}
	if atomic.LoadInt32(&fbCalls) == 0 {
		t.Fatal("fallback client was never used after the primary failed")
	}
}

type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
