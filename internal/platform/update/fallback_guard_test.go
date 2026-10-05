package update

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// signedAt is a manifest whose asset is served at primary with fallback as
// its second address, signed as the release server signs it.
func signedAt(rs *releaseServer, body []byte, primary, fallback string) *Manifest {
	name := "Reasonix-" + CurrentPlatform() + ".tar.gz"
	good := rs.URL + "/v2.0.0/" + name
	return &Manifest{Version: "v2.0.0", Platforms: map[string]Asset{CurrentPlatform(): {
		URL: primary, Sig: good + ".minisig", Fallback: fallback, FallbackSig: good + ".minisig",
		Size: int64(len(body)), SHA256: sha256Hex(body),
	}}}
}

// Bytes the primary served that the signature refuses are refused outright:
// the fallback is an address for an unreachable mirror, not a second opinion
// on a forged artifact.
func TestTamperedBytesFromThePrimaryDoNotFallThrough(t *testing.T) {
	fastRetry(t)
	body := []byte("the v2.0.0 build")
	rs := serveReleases(t, release{"v2.0.0", body})
	var fallbackHits atomic.Int32
	c := hostRoutes(map[string]func(*http.Request) (*http.Response, error){
		"mirror.test": func(*http.Request) (*http.Response, error) {
			return okBody(strings.Repeat("X", len(body))), nil
		},
		"github.test": func(*http.Request) (*http.Response, error) {
			fallbackHits.Add(1)
			return okBody(string(body)), nil
		},
	})
	u := New(Options{Current: "v1.0.0", HTTP: &http.Client{Transport: routeOr(c, rs.Client())}, CacheDir: t.TempDir()})
	m := signedAt(rs, body, "https://mirror.test/a", "https://github.test/a")
	if _, err := u.DownloadManifest(context.Background(), m, Report{}); !errors.Is(err, ErrVerify) {
		t.Fatalf("err = %v, want ErrVerify", err)
	}
	if n := fallbackHits.Load(); n != 0 {
		t.Fatalf("the fallback was asked %d times after the primary's bytes failed verification", n)
	}
}

func TestTamperedBytesFromTheFallbackAreRefused(t *testing.T) {
	fastRetry(t)
	body := []byte("the v2.0.0 build")
	rs := serveReleases(t, release{"v2.0.0", body})
	c := hostRoutes(map[string]func(*http.Request) (*http.Response, error){
		"mirror.test": func(*http.Request) (*http.Response, error) { return statusOnly(http.StatusNotFound) },
		"github.test": func(*http.Request) (*http.Response, error) {
			return okBody(strings.Repeat("X", len(body))), nil
		},
	})
	u := New(Options{Current: "v1.0.0", HTTP: &http.Client{Transport: routeOr(c, rs.Client())}, CacheDir: t.TempDir()})
	m := signedAt(rs, body, "https://mirror.test/a", "https://github.test/a")
	if _, err := u.DownloadManifest(context.Background(), m, Report{}); !errors.Is(err, ErrVerify) {
		t.Fatalf("err = %v, want ErrVerify", err)
	}
}

// A mirror answering 200 with a body the manifest's size rules out (an error
// page) is that address failing: the fallback is asked, and nothing the mirror
// sent is kept as a prefix to resume.
func TestAShortBodyFromThePrimaryMovesToTheFallback(t *testing.T) {
	fastRetry(t)
	body := []byte("the whole installer, every byte of it")
	var mirrorHits atomic.Int32
	var fallbackRange string
	c := hostRoutes(map[string]func(*http.Request) (*http.Response, error){
		"mirror.test": func(*http.Request) (*http.Response, error) {
			mirrorHits.Add(1)
			return okBody("<html>busy</html>"), nil
		},
		"github.test": func(r *http.Request) (*http.Response, error) {
			fallbackRange = r.Header.Get("Range")
			return okBody(string(body)), nil
		},
	})
	got, err := tp(c, c).DownloadFrom(context.Background(), []string{"https://mirror.test/a", "https://github.test/a"}, int64(len(body)), nil)
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("DownloadFrom = %q, %v", got, err)
	}
	if n := mirrorHits.Load(); n != 1 {
		t.Fatalf("mirror asked %d times, want once", n)
	}
	if fallbackRange != "" {
		t.Fatalf("fallback asked for %q, want the whole artifact", fallbackRange)
	}
}

// A mirror that accepts the connection and then sends nothing is dropped by
// the stall timeout and the fallback serves the download, long before the
// move's own deadline would have ended it.
func TestAHungPrimaryFallsOverToTheFallback(t *testing.T) {
	fastRetry(t)
	body := []byte("served by the fallback")
	c := hostRoutes(map[string]func(*http.Request) (*http.Response, error){
		"mirror.test": func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, ContentLength: int64(len(body)), Header: make(http.Header),
				Body: io.NopCloser(blockingReader{r.Context().Done()})}, nil
		},
		"github.test": func(*http.Request) (*http.Response, error) { return okBody(string(body)), nil },
	})
	tr := tp(c, c)
	tr.StallTimeout = 50 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	got, err := tr.DownloadFrom(ctx, []string{"https://mirror.test/a", "https://github.test/a"}, int64(len(body)), nil)
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("DownloadFrom = %q, %v", got, err)
	}
}

// blockingReader delivers nothing until its request is abandoned.
type blockingReader struct{ done <-chan struct{} }

func (b blockingReader) Read([]byte) (int, error) {
	<-b.done
	return 0, context.Canceled
}

// routeOr sends the hosts routes knows to it and everything else to fallback.
func routeOr(routes, fallback *http.Client) http.RoundTripper {
	return rtFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "mirror.test" || r.URL.Host == "github.test" {
			return routes.Transport.RoundTrip(r)
		}
		return fallback.Transport.RoundTrip(r)
	})
}
