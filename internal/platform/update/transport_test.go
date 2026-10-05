package update

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fastRetry(t *testing.T) {
	t.Helper()
	restore := Backoff
	Backoff = func(int) time.Duration { return time.Millisecond }
	t.Cleanup(func() { Backoff = restore })
}

// tp is a Transport with the attempt bound the desktop ships, so a stalled
// primary route cannot hold the whole budget.
func tp(client, fallback *http.Client) Transport {
	return Transport{Client: client, Fallback: fallback, UserAgent: "Reasonix-Updater/test", AttemptTimeout: 5 * time.Second}
}

func TestDownloadRecoversFromMidStreamReset(t *testing.T) {
	fastRetry(t)
	const body = "complete-installer-bytes"
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) < int32(Attempts) {
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

	data, err := tp(srv.Client(), nil).Download(context.Background(), srv.URL, 0, nil)
	if err != nil {
		t.Fatalf("download should recover after %d resets: %v", Attempts-1, err)
	}
	if string(data) != body {
		t.Fatalf("got %q, want %q", data, body)
	}
	if n := calls.Load(); n != int32(Attempts) {
		t.Fatalf("made %d attempts, want %d", n, Attempts)
	}
}

func TestDownloadGivesUpAfterCap(t *testing.T) {
	fastRetry(t)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		conn.Close()
	}))
	defer srv.Close()

	if _, err := tp(srv.Client(), nil).Download(context.Background(), srv.URL, 0, nil); err == nil {
		t.Fatal("download should fail after exhausting retries")
	}
	if n := calls.Load(); n != int32(Attempts) {
		t.Fatalf("made %d attempts, want %d", n, Attempts)
	}
}

func TestRetryStopsWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	if err := Retry(ctx, func(int) error {
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
	var calls atomic.Int32
	rangeCh := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
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

	data, err := tp(srv.Client(), nil).Download(context.Background(), srv.URL, 0, nil)
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
	var fbCalls atomic.Int32
	fallback := &http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) {
		fbCalls.Add(1)
		return &http.Response{
			StatusCode:    http.StatusOK,
			Body:          io.NopCloser(strings.NewReader(body)),
			ContentLength: int64(len(body)),
			Header:        make(http.Header),
		}, nil
	})}

	data, err := tp(primary, fallback).Download(context.Background(), "http://example.invalid/x", 0, nil)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if string(data) != body {
		t.Fatalf("got %q, want %q", data, body)
	}
	if fbCalls.Load() == 0 {
		t.Fatal("fallback client was never used after the primary failed")
	}
}

func TestDownloadRejectsBodyShorterThanManifestSize(t *testing.T) {
	fastRetry(t)
	body := []byte("short")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	if _, err := tp(srv.Client(), nil).Download(context.Background(), srv.URL, int64(len(body)+1), nil); err == nil {
		t.Fatal("download accepted fewer bytes than the manifest declared")
	}
}

func TestDownloadRejectsBodyLongerThanManifestSize(t *testing.T) {
	fastRetry(t)
	body := bytes.Repeat([]byte("x"), 64)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	if _, err := tp(srv.Client(), nil).Download(context.Background(), srv.URL, 8, nil); err == nil {
		t.Fatal("download accepted more bytes than the manifest declared")
	}
}

func TestDownloadRejectsAssetSizeAboveMaximum(t *testing.T) {
	if _, err := tp(&http.Client{}, nil).Download(
		context.Background(), "https://dl.reasonix.io/file", MaxAssetSize+1, nil,
	); err == nil {
		t.Fatal("download accepted an asset size above the release maximum")
	}
}

func TestFetchFallsBackToSecondClient(t *testing.T) {
	fastRetry(t)
	primary := &http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("read tcp [ipv6]: connection reset")
	})}
	fallback := &http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) {
		return okBody("manifest"), nil
	})}

	data, err := tp(primary, fallback).Fetch(context.Background(), "https://example.invalid/latest.json", MaxAssetSize)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if string(data) != "manifest" {
		t.Fatalf("got %q, want manifest", data)
	}
}

// A primary that stalls without erroring holds the whole budget, so the retry
// that would have reached the IPv4 route never runs. AttemptTimeout is what
// bounds it; Download deliberately has no such bound.
func TestFetchEscapesStalledPrimary(t *testing.T) {
	fastRetry(t)
	primary := &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	fallback := &http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) {
		return okBody("ipv4"), nil
	})}
	transport := tp(primary, fallback)
	transport.AttemptTimeout = 10 * time.Millisecond

	data, err := transport.Fetch(context.Background(), "https://example.invalid/latest.json", MaxAssetSize)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if string(data) != "ipv4" {
		t.Fatalf("got %q, want ipv4", data)
	}
}

func TestFetchDoesNotRetryPermanentHTTPStatus(t *testing.T) {
	fastRetry(t)
	var calls atomic.Int32
	client := &http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{
			StatusCode: http.StatusForbidden,
			Status:     "403 Forbidden",
			Body:       io.NopCloser(strings.NewReader("forbidden")),
			Header:     make(http.Header),
		}, nil
	})}

	if _, err := tp(client, nil).Fetch(context.Background(), "https://example.invalid/latest.json", MaxAssetSize); err == nil {
		t.Fatal("Fetch should return a permanent HTTP error")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("permanent HTTP error made %d requests, want 1", got)
	}
}

func TestFetchRejectsOversizeResponsesWithoutRetry(t *testing.T) {
	fastRetry(t)
	t.Run("declared content length", func(t *testing.T) {
		var calls atomic.Int32
		client := &http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) {
			calls.Add(1)
			resp := okBody("ignored")
			resp.ContentLength = 9
			return resp, nil
		})}
		if _, err := tp(client, nil).Fetch(context.Background(), "https://example.invalid/latest.json", 8); !errors.Is(err, ErrTooLarge) {
			t.Fatalf("declared oversize error = %v, want ErrTooLarge", err)
		}
		if got := calls.Load(); got != 1 {
			t.Fatalf("declared oversize response made %d requests, want 1", got)
		}
	})

	t.Run("chunked body", func(t *testing.T) {
		client := &http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) {
			resp := okBody("123456789")
			resp.ContentLength = -1
			return resp, nil
		})}
		if _, err := tp(client, nil).Fetch(context.Background(), "https://example.invalid/latest.json", 8); !errors.Is(err, ErrTooLarge) {
			t.Fatalf("chunked oversize error = %v, want ErrTooLarge", err)
		}
	})
}

func okBody(body string) *http.Response {
	return &http.Response{
		StatusCode:    http.StatusOK,
		Status:        "200 OK",
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)),
		Header:        make(http.Header),
	}
}

type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// A 404 or a Cloudflare 403 on the asset is a verdict, not a blip. An untyped
// error here read as transient, so every such failure burned the full retry
// schedule before surfacing.
func TestDownloadDoesNotRetryATerminalStatus(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	if _, err := tp(srv.Client(), nil).Download(context.Background(), srv.URL, 0, nil); err == nil {
		t.Fatal("a 403 must fail the download")
	}
	if got := hits.Load(); got != 1 {
		t.Errorf("server saw %d requests, want 1: a terminal status must not be retried", got)
	}
}

// An intermediary answering a resume from a different offset must not have its
// bytes appended to what we already hold. CN CDNs and proxies do this.
func TestDownloadRestartsWhenAResumeIgnoresTheOffset(t *testing.T) {
	fastRetry(t)
	payload := bytes.Repeat([]byte("ab"), 512) // 1024 bytes
	var served atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if served.Add(1) == 1 {
			// First attempt dies halfway, leaving a partial buffer to resume.
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(payload[:400])
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			if hj, ok := w.(http.Hijacker); ok {
				if conn, _, err := hj.Hijack(); err == nil {
					_ = conn.Close()
				}
			}
			return
		}
		// The retry asks to resume at 400; this server answers from 0 anyway.
		w.Header().Set("Content-Range", "bytes 0-1023/1024")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	got, err := tp(srv.Client(), nil).Download(context.Background(), srv.URL, int64(len(payload)), nil)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("resumed download is corrupt: got %d bytes, want %d intact", len(got), len(payload))
	}
}
