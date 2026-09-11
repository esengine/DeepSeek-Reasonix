package releaseasset

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestVerifyChecksum(t *testing.T) {
	data := []byte("payload-bytes")
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	asset := "reasonix-linux-amd64.tar.gz"

	tests := []struct {
		name      string
		checksums string
		wantErr   string
	}{
		{"valid plain entry", digest + "  " + asset, ""},
		{"valid binary-mode star prefix", digest + " *" + asset, ""},
		{"valid uppercase digest", strings.ToUpper(digest) + "  " + asset, ""},
		{"duplicate entries", digest + "  " + asset + "\n" + digest + "  " + asset, "duplicate entries"},
		{"duplicate entries with star prefix", digest + "  " + asset + "\n" + digest + " *" + asset, "duplicate entries"},
		{"missing entry", digest + "  reasonix-linux-arm64.tar.gz", "no valid entry"},
		{"unrelated lines only", "0000000000000000000000000000000000000000000000000000000000000000  some-other-file\n", "no valid entry"},
		{"empty checksums", "", "no valid entry"},
		{"invalid digest format", strings.Repeat("z", sha256.Size*2) + "  " + asset, "invalid digest"},
		{"wrong digest length", "abcd  " + asset, "no valid entry"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := verifyChecksum(data, asset, []byte(tt.checksums))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("verifyChecksum() error = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatal("verifyChecksum() succeeded, want error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("verifyChecksum() error = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestFetchBounded(t *testing.T) {
	const limit = int64(100)

	t.Run("accepts body within limit", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("small"))
		}))
		defer server.Close()

		got, err := fetchBounded(context.Background(), server.Client(), server.URL, limit)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "small" {
			t.Fatalf("body = %q, want %q", got, "small")
		}
	})

	t.Run("accepts body exactly at limit", func(t *testing.T) {
		body := bytes.Repeat([]byte("x"), int(limit))
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(body)
		}))
		defer server.Close()

		got, err := fetchBounded(context.Background(), server.Client(), server.URL, limit)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != int(limit) {
			t.Fatalf("body length = %d, want %d", len(got), limit)
		}
	})

	t.Run("rejects declared ContentLength above limit", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "200")
			_, _ = w.Write(bytes.Repeat([]byte("x"), 200))
		}))
		defer server.Close()

		if _, err := fetchBounded(context.Background(), server.Client(), server.URL, limit); err == nil {
			t.Fatal("fetchBounded() accepted a response whose declared ContentLength exceeds the limit")
		}
	})

	t.Run("rejects chunked body above limit", func(t *testing.T) {
		// No Content-Length header: flushing forces chunked transfer encoding so
		// the client sees ContentLength == -1 and the limit must be enforced by
		// the io.LimitReader overflow check.
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("part"))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			_, _ = w.Write(bytes.Repeat([]byte("x"), 200))
		}))
		defer server.Close()

		if _, err := fetchBounded(context.Background(), server.Client(), server.URL, limit); err == nil {
			t.Fatal("fetchBounded() accepted a chunked body exceeding the limit")
		}
	})

	t.Run("rejects non-200 response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		}))
		defer server.Close()

		_, err := fetchBounded(context.Background(), server.Client(), server.URL, limit)
		if err == nil {
			t.Fatal("fetchBounded() accepted a non-200 response")
		}
		if !strings.Contains(err.Error(), "404") {
			t.Fatalf("fetchBounded() error = %q, want it to contain the status", err)
		}
	})
}

// testTarEntry describes a raw tar entry whose declared size may differ from
// its actual payload, for exercising extractCLI's boundary checks.
type testTarEntry struct {
	name     string
	size     int64
	typeflag byte
	data     []byte
}

// testTarHeader builds a minimal 512-byte ustar header with a valid checksum.
func testTarHeader(name string, size int64, typeflag byte) []byte {
	var hdr [512]byte
	copy(hdr[0:100], name)
	copy(hdr[100:108], "0000755\x00")
	copy(hdr[108:116], "0000000\x00")
	copy(hdr[116:124], "0000000\x00")
	copy(hdr[124:136], fmt.Sprintf("%011o", size))
	copy(hdr[136:148], "00000000000")
	for i := 148; i < 156; i++ {
		hdr[i] = ' '
	}
	hdr[156] = typeflag
	copy(hdr[257:263], "ustar\x0000")
	var sum int64
	for _, b := range hdr {
		sum += int64(b)
	}
	copy(hdr[148:154], fmt.Sprintf("%06o", sum))
	hdr[154] = 0
	hdr[155] = ' '
	return hdr[:]
}

func gzipBytes(t *testing.T, raw []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// testRawCLIArchive builds a gzip-compressed tar with full control over each
// entry's declared size vs. actual payload. Entries are block-padded and the
// archive is terminated normally.
func testRawCLIArchive(t *testing.T, entries []testTarEntry) []byte {
	t.Helper()
	var raw []byte
	for _, e := range entries {
		raw = append(raw, testTarHeader(e.name, e.size, e.typeflag)...)
		raw = append(raw, e.data...)
		if rem := len(e.data) % 512; rem != 0 {
			raw = append(raw, make([]byte, 512-rem)...)
		}
	}
	raw = append(raw, make([]byte, 1024)...) // end-of-archive marker
	return gzipBytes(t, raw)
}

func TestExtractCLI(t *testing.T) {
	t.Run("extracts binary", func(t *testing.T) {
		archive := testRawCLIArchive(t, []testTarEntry{
			{name: "reasonix", size: 4, typeflag: tar.TypeReg, data: []byte("bin!")},
		})
		got, err := extractCLI(archive)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "bin!" {
			t.Fatalf("binary = %q, want %q", got, "bin!")
		}
	})

	t.Run("rejects duplicate binaries", func(t *testing.T) {
		archive := testRawCLIArchive(t, []testTarEntry{
			{name: "reasonix", size: 3, typeflag: tar.TypeReg, data: []byte("abc")},
			{name: "reasonix", size: 3, typeflag: tar.TypeReg, data: []byte("def")},
		})
		_, err := extractCLI(archive)
		if err == nil {
			t.Fatal("extractCLI() accepted duplicate binary entries")
		}
		if !strings.Contains(err.Error(), "duplicate binaries") {
			t.Fatalf("extractCLI() error = %q, want it to contain %q", err, "duplicate binaries")
		}
	})

	t.Run("rejects oversized entry", func(t *testing.T) {
		archive := testRawCLIArchive(t, []testTarEntry{
			{name: "reasonix", size: maxExtractedCLIBytes + 1, typeflag: tar.TypeReg, data: []byte("tiny")},
		})
		_, err := extractCLI(archive)
		if err == nil {
			t.Fatal("extractCLI() accepted an entry larger than maxExtractedCLIBytes")
		}
		if !strings.Contains(err.Error(), "not a bounded regular file") {
			t.Fatalf("extractCLI() error = %q, want it to contain %q", err, "not a bounded regular file")
		}
	})

	t.Run("rejects payload shorter than declared size", func(t *testing.T) {
		// The entry declares 10 bytes but only 3 are present before the stream
		// ends. archive/tar surfaces the premature EOF as an error before
		// extractCLI's own size-mismatch check can be reached, so any rejection
		// is sufficient here.
		var raw []byte
		raw = append(raw, testTarHeader("reasonix", 10, tar.TypeReg)...)
		raw = append(raw, []byte("abc")...) // no block padding, no end marker
		archive := gzipBytes(t, raw)

		if _, err := extractCLI(archive); err == nil {
			t.Fatal("extractCLI() accepted an entry whose payload is shorter than its declared size")
		}
	})

	t.Run("rejects archive without binary", func(t *testing.T) {
		archive := testRawCLIArchive(t, []testTarEntry{
			{name: "LICENSE", size: 4, typeflag: tar.TypeReg, data: []byte("MIT\n")},
		})
		_, err := extractCLI(archive)
		if err == nil {
			t.Fatal("extractCLI() accepted an archive without a reasonix binary")
		}
		if !strings.Contains(err.Error(), "binary not found") {
			t.Fatalf("extractCLI() error = %q, want it to contain %q", err, "binary not found")
		}
	})
}

func TestValidateOfficialRedirect(t *testing.T) {
	mkreq := func(rawurl string) *http.Request {
		u, err := url.Parse(rawurl)
		if err != nil {
			t.Fatal(err)
		}
		return &http.Request{URL: u}
	}
	via := func(n int) []*http.Request { return make([]*http.Request, n) }

	tests := []struct {
		name    string
		rawurl  string
		via     int
		wantErr string
	}{
		{"github.com", "https://github.com/esengine/DeepSeek-Reasonix/releases/download/v1.2.3/reasonix-linux-amd64.tar.gz", 0, ""},
		{"githubusercontent subdomain", "https://objects.githubusercontent.com/gh-resources/foo", 0, ""},
		{"non-GitHub host", "https://example.com/evil", 0, "refused redirect host"},
		{"githubusercontent lookalike", "https://notgithubusercontent.com/evil", 0, "refused redirect host"},
		{"githubusercontent suffix masquerade", "https://evil.com.githubusercontent.com.evil.example/", 0, "refused redirect host"},
		{"http scheme", "http://github.com/releases/download/x", 0, "unsafe redirect"},
		{"userinfo", "https://attacker@github.com/releases/download/x", 0, "unsafe redirect"},
		{"userinfo with password", "https://user:pass@github.com/x", 0, "unsafe redirect"},
		{"explicit port", "https://github.com:8443/x", 0, "unsafe redirect"},
		{"10 redirects", "https://github.com/x", 10, "stopped after 10 redirects"},
		{"11 redirects", "https://github.com/x", 11, "stopped after 10 redirects"},
		{"9 redirects allowed", "https://github.com/x", 9, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateOfficialRedirect(mkreq(tt.rawurl), via(tt.via))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateOfficialRedirect() error = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatal("validateOfficialRedirect() succeeded, want error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("validateOfficialRedirect() error = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}
