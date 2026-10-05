package update

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

// rangeServer serves body honouring Range, and lets a test decide per request
// how much to send before the connection stops delivering.
func rangeServer(t *testing.T, body []byte, cut func(call int, from int64) (send int, stall bool)) (*httptest.Server, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var ranges []string
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := int(calls.Add(1))
		mu.Lock()
		ranges = append(ranges, r.Header.Get("Range"))
		mu.Unlock()
		from := int64(0)
		if rg := r.Header.Get("Range"); rg != "" {
			from, _ = strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(rg, "bytes="), "-"), 10, 64)
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", from, len(body)-1, len(body)))
			w.Header().Set("Content-Length", strconv.FormatInt(int64(len(body))-from, 10))
			w.WriteHeader(http.StatusPartialContent)
		} else {
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			w.WriteHeader(http.StatusOK)
		}
		send, stall := len(body)-int(from), false
		if cut != nil {
			send, stall = cut(call, from)
		}
		_, _ = w.Write(body[from : from+int64(send)])
		w.(http.Flusher).Flush()
		if stall {
			<-r.Context().Done()
			return
		}
		if from+int64(send) < int64(len(body)) {
			conn, _, _ := w.(http.Hijacker).Hijack()
			_ = conn.Close()
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &ranges
}

// A connection that goes quiet is dropped after StallTimeout and the download
// resumes where it stopped, rather than waiting on the whole move's deadline.
func TestDownloadResumesAfterAStall(t *testing.T) {
	fastRetry(t)
	body := bytes.Repeat([]byte("studio"), 1000)
	srv, ranges := rangeServer(t, body, func(call int, from int64) (int, bool) {
		if call == 1 {
			return 1000, true
		}
		return len(body) - int(from), false
	})
	tr := tp(srv.Client(), nil)
	tr.StallTimeout = 100 * time.Millisecond
	done := make(chan struct{})
	var got []byte
	var err error
	go func() {
		defer close(done)
		got, err = tr.Download(context.Background(), srv.URL, int64(len(body)), nil)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("a stalled download was never dropped")
	}
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("Download = %d bytes, %v", len(got), err)
	}
	if len(*ranges) != 2 || (*ranges)[1] != "bytes=1000-" {
		t.Fatalf("ranges = %q, want a resume from byte 1000", *ranges)
	}
}

// A slow link that keeps getting further is not abandoned after Attempts
// resets: only an attempt that got nowhere counts.
func TestDownloadKeepsResumingWhileItAdvances(t *testing.T) {
	fastRetry(t)
	body := bytes.Repeat([]byte("x"), 100)
	srv, ranges := rangeServer(t, body, func(_ int, from int64) (int, bool) {
		return min(10, len(body)-int(from)), false
	})
	got, err := tp(srv.Client(), nil).Download(context.Background(), srv.URL, int64(len(body)), nil)
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("Download = %d bytes, %v", len(got), err)
	}
	if n := len(*ranges); n != 10 {
		t.Fatalf("made %d requests, want 10 that each advanced", n)
	}
}

// What an earlier download left on disk is resumed, not fetched again.
func TestDownloadFileResumesAPartialFromDisk(t *testing.T) {
	fastRetry(t)
	body := bytes.Repeat([]byte("0123456789"), 50)
	srv, ranges := rangeServer(t, body, nil)
	partial := filepath.Join(testenv.TempDir(t), "partial")
	if err := os.WriteFile(partial, body[:120], 0o600); err != nil {
		t.Fatal(err)
	}
	var first int64 = -1
	err := tp(srv.Client(), nil).DownloadFile(context.Background(), []string{srv.URL}, int64(len(body)), partial, func(received, _ int64) {
		if first < 0 {
			first = received
		}
	})
	if err != nil {
		t.Fatalf("DownloadFile: %v", err)
	}
	if got, _ := os.ReadFile(partial); !bytes.Equal(got, body) {
		t.Fatalf("partial holds %d bytes, want the whole body", len(got))
	}
	if len(*ranges) != 1 || (*ranges)[0] != "bytes=120-" {
		t.Fatalf("ranges = %q, want one resume from byte 120", *ranges)
	}
	if first < 120 {
		t.Fatalf("progress started at %d, want it to count what was already on disk", first)
	}
}

// A partial longer than the artifact cannot be a prefix of it.
func TestDownloadFileDiscardsAPartialLongerThanTheArtifact(t *testing.T) {
	fastRetry(t)
	body := []byte("short artifact")
	srv, ranges := rangeServer(t, body, nil)
	partial := filepath.Join(testenv.TempDir(t), "partial")
	if err := os.WriteFile(partial, bytes.Repeat([]byte("z"), 100), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := tp(srv.Client(), nil).DownloadFile(context.Background(), []string{srv.URL}, int64(len(body)), partial, nil); err != nil {
		t.Fatalf("DownloadFile: %v", err)
	}
	if got, _ := os.ReadFile(partial); !bytes.Equal(got, body) {
		t.Fatalf("partial = %q", got)
	}
	if (*ranges)[0] != "" {
		t.Fatalf("asked for %q, want the whole artifact", (*ranges)[0])
	}
}

// The updater keeps its partial in the cache under the digest it must reach:
// a partial of this release is resumed, one of another is removed, and none
// is left behind once the release is cached.
func TestDownloadManifestResumesItsPartialAndClearsOthers(t *testing.T) {
	fastRetry(t)
	body := bytes.Repeat([]byte("the v2.0.0 build "), 40000)
	rs := serveReleases(t, release{"v2.0.0", body})
	u := rs.updater(t, "v1.0.0")
	sum := sha256Hex(body)
	mine := filepath.Join(u.opts.CacheDir, partialPrefix+sum+partialSuffix)
	stale := filepath.Join(u.opts.CacheDir, partialPrefix+strings.Repeat("0", 64)+partialSuffix)
	const kept = 300 << 10
	if err := os.WriteFile(mine, body[:kept], 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("another release"), 0o600); err != nil {
		t.Fatal(err)
	}
	var first int64 = -1
	c, err := u.Download(context.Background(), "v2.0.0", Report{Bytes: func(received, _ int64) {
		if first < 0 {
			first = received
		}
	}})
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if got, _ := os.ReadFile(c.Path); !bytes.Equal(got, body) {
		t.Fatal("the cache does not hold the release")
	}
	// Progress is reported every 256 KiB received; counted from zero, the first
	// report would land before what was kept.
	if first <= kept {
		t.Fatalf("first progress at %d, want it past the %d bytes kept", first, kept)
	}
	for _, p := range []string{mine, stale} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s is still on disk", filepath.Base(p))
		}
	}
}

// A partial whose bytes are wrong fails the signature once and is gone: the
// next download starts clean rather than resuming the same corruption forever.
func TestDownloadManifestDropsAPartialThatFailsVerification(t *testing.T) {
	fastRetry(t)
	body := bytes.Repeat([]byte("the v2.0.0 build "), 200)
	rs := serveReleases(t, release{"v2.0.0", body})
	u := rs.updater(t, "v1.0.0")
	mine := filepath.Join(u.opts.CacheDir, partialPrefix+sha256Hex(body)+partialSuffix)
	if err := os.WriteFile(mine, bytes.Repeat([]byte("?"), 500), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := u.Download(context.Background(), "v2.0.0", Report{}); !errors.Is(err, ErrVerify) {
		t.Fatalf("err = %v, want ErrVerify", err)
	}
	if _, err := os.Stat(mine); !os.IsNotExist(err) {
		t.Fatal("the corrupt partial was kept for the next attempt to resume")
	}
	if _, err := u.Download(context.Background(), "v2.0.0", Report{}); err != nil {
		t.Fatalf("the next download should start clean: %v", err)
	}
}
