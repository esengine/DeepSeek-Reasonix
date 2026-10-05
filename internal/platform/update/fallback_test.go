package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

// hostRoutes answers each request by its host, so one client can stand in for
// a mirror and the GitHub release at once.
func hostRoutes(routes map[string]func(*http.Request) (*http.Response, error)) *http.Client {
	return &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
		if h, ok := routes[r.URL.Host]; ok {
			return h(r)
		}
		return nil, fmt.Errorf("no route to %s", r.URL.Host)
	})}
}

func statusOnly(code int) (*http.Response, error) {
	return &http.Response{StatusCode: code, Status: http.StatusText(code), Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
}

func TestDownloadFromMovesToTheFallbackWhenTheMirrorRefuses(t *testing.T) {
	fastRetry(t)
	var mirrorCalls atomic.Int32
	c := hostRoutes(map[string]func(*http.Request) (*http.Response, error){
		"mirror.test": func(*http.Request) (*http.Response, error) {
			mirrorCalls.Add(1)
			return statusOnly(http.StatusNotFound)
		},
		"github.test": func(*http.Request) (*http.Response, error) { return okBody("installer"), nil },
	})
	a := Asset{URL: "https://mirror.test/a.exe", Fallback: "https://github.test/a.exe"}
	data, err := tp(c, c).DownloadFrom(context.Background(), a.Sources(), 0, nil)
	if err != nil || string(data) != "installer" {
		t.Fatalf("DownloadFrom = %q, %v; want the fallback's bytes", data, err)
	}
	// A 404 is not transient, so the mirror is asked once rather than burning
	// the whole retry budget on an answer that cannot change.
	if n := mirrorCalls.Load(); n != 1 {
		t.Fatalf("mirror asked %d times, want 1", n)
	}
}

// What the mirror delivered before it failed is kept: the fallback is asked
// only for the rest, since both addresses serve the same bytes.
func TestDownloadFromResumesOnTheFallback(t *testing.T) {
	fastRetry(t)
	const body = "0123456789abcdef"
	var ranges []string
	c := hostRoutes(map[string]func(*http.Request) (*http.Response, error){
		"mirror.test": func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK, ContentLength: int64(len(body)), Header: make(http.Header),
				Body: io.NopCloser(io.MultiReader(strings.NewReader(body[:6]), errReader{errors.New("connection reset")})),
			}, nil
		},
		"github.test": func(r *http.Request) (*http.Response, error) {
			ranges = append(ranges, r.Header.Get("Range"))
			h := make(http.Header)
			h.Set("Content-Range", fmt.Sprintf("bytes 6-%d/%d", len(body)-1, len(body)))
			return &http.Response{StatusCode: http.StatusPartialContent, Header: h, Body: io.NopCloser(strings.NewReader(body[6:]))}, nil
		},
	})
	data, err := tp(c, c).DownloadFrom(context.Background(), []string{"https://mirror.test/a", "https://github.test/a"}, int64(len(body)), nil)
	if err != nil || string(data) != body {
		t.Fatalf("DownloadFrom = %q, %v; want the whole body", data, err)
	}
	if len(ranges) != 1 || ranges[0] != "bytes=6-" {
		t.Fatalf("fallback ranges = %q, want one resume from byte 6", ranges)
	}
}

func TestDownloadFromReportsEveryAddressThatFailed(t *testing.T) {
	fastRetry(t)
	c := hostRoutes(map[string]func(*http.Request) (*http.Response, error){
		"mirror.test": func(*http.Request) (*http.Response, error) { return statusOnly(http.StatusForbidden) },
		"github.test": func(*http.Request) (*http.Response, error) { return statusOnly(http.StatusNotFound) },
	})
	_, err := tp(c, c).DownloadFrom(context.Background(), []string{"https://mirror.test/a", "https://github.test/a"}, 0, nil)
	var status *StatusError
	if !errors.As(err, &status) || !strings.Contains(err.Error(), "mirror.test") || !strings.Contains(err.Error(), "github.test") {
		t.Fatalf("err = %v, want both refusals, still typed", err)
	}
}

func TestFetchFromMovesToTheFallbackSignature(t *testing.T) {
	fastRetry(t)
	c := hostRoutes(map[string]func(*http.Request) (*http.Response, error){
		"mirror.test": func(*http.Request) (*http.Response, error) { return nil, errors.New("connection reset") },
		"github.test": func(*http.Request) (*http.Response, error) { return okBody("sig"), nil },
	})
	a := Asset{Sig: "https://mirror.test/a.minisig", FallbackSig: "https://github.test/a.minisig"}
	data, err := tp(c, c).FetchFrom(context.Background(), a.SigSources(), MaxSignatureSize)
	if err != nil || string(data) != "sig" {
		t.Fatalf("FetchFrom = %q, %v; want the fallback signature", data, err)
	}
}

// A manifest written before the fallback existed decodes to one address, and
// one written after reads the same to a client that only knows URL.
func TestAssetWithoutFallbackHasOneSource(t *testing.T) {
	var a Asset
	if err := json.Unmarshal([]byte(`{"url":"https://x/a","sig":"https://x/a.minisig"}`), &a); err != nil {
		t.Fatal(err)
	}
	if got := a.Sources(); len(got) != 1 || got[0] != "https://x/a" {
		t.Fatalf("Sources = %q", got)
	}
	if got := a.SigSources(); len(got) != 1 || got[0] != "https://x/a.minisig" {
		t.Fatalf("SigSources = %q", got)
	}
	var old struct {
		URL string `json:"url"`
	}
	raw, _ := json.Marshal(Asset{URL: "https://m/a", Fallback: "https://g/a"})
	if err := json.Unmarshal(raw, &old); err != nil || old.URL != "https://m/a" {
		t.Fatalf("an older reader sees %q, %v", old.URL, err)
	}
}

type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }

// The fallback earns the same checks as the primary: a mirror that is gone
// hands the download to GitHub, and what GitHub serves is still verified.
func TestDownloadManifestVerifiesWhatTheFallbackServes(t *testing.T) {
	fastRetry(t)
	body := []byte("the v2.0.0 build")
	rs := serveReleases(t, release{"v2.0.0", body})
	name := "Reasonix-" + CurrentPlatform() + ".tar.gz"
	good := rs.URL + "/v2.0.0/" + name
	m := &Manifest{Version: "v2.0.0", Platforms: map[string]Asset{CurrentPlatform(): {
		URL: rs.URL + "/gone/" + name, Sig: rs.URL + "/gone/" + name + ".minisig",
		Fallback: good, FallbackSig: good + ".minisig",
		Size: int64(len(body)), SHA256: sha256Hex(body),
	}}}
	c, err := rs.updater(t, "v1.0.0").DownloadManifest(context.Background(), m, Report{})
	if err != nil {
		t.Fatalf("DownloadManifest: %v", err)
	}
	if c.Version != "v2.0.0" {
		t.Fatalf("cached %s", c.Version)
	}

	forged := *m
	a := m.Platforms[CurrentPlatform()]
	a.FallbackSig = rs.URL + "/v2.0.0/latest.json"
	forged.Platforms = map[string]Asset{CurrentPlatform(): a}
	if _, err := rs.updater(t, "v1.0.0").DownloadManifest(context.Background(), &forged, Report{}); !errors.Is(err, ErrVerify) {
		t.Fatalf("err = %v, want ErrVerify for a fallback signature that does not sign the bytes", err)
	}
}
