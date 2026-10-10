package market

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const listBody = `{"packages":[{"slug":"a/kit","status":"active","name":"kit"}],"limit":24,"offset":0}`

type registryStub struct {
	srv  *httptest.Server
	seen atomic.Int32
	last atomic.Value
}

func newStub(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *registryStub {
	t.Helper()
	s := &registryStub{}
	s.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.seen.Add(1)
		s.last.Store(r.Header.Clone())
		handler(w, r)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *registryStub) client(dir string) *Client {
	u, _ := url.Parse(s.srv.URL)
	c := newClient(url.URL{Scheme: "https", Host: u.Host}, s.srv.Client()).WithCache(dir)
	if c.cache != nil {
		c.cache.freshFor = 0
	}
	return c
}

func (s *registryStub) freshClient(dir string) *Client {
	c := s.client(dir)
	c.cache.freshFor = cacheFreshFor
	return c
}

func (s *registryStub) lastHeader(k string) string { return s.last.Load().(http.Header).Get(k) }

func okList(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(listBody)) }

func cacheFiles(t *testing.T, dir string) []string {
	t.Helper()
	items, _ := filepath.Glob(filepath.Join(dir, "market", "*.json"))
	return items
}

func freezeClock(t *testing.T) *time.Time {
	t.Helper()
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	old := cacheNow
	cacheNow = func() time.Time { return now }
	t.Cleanup(func() { cacheNow = old })
	return &now
}

func TestFirstReadWithNoCacheAndNoRegistryIsUnreachable(t *testing.T) {
	stub := newStub(t, okList)
	c := stub.client(t.TempDir())
	stub.srv.Close()
	page, err := c.List(context.Background(), Query{})
	if !errors.Is(err, ErrUnreachable) || page.Cache != nil {
		t.Fatalf("page = %+v err = %v", page, err)
	}
}

func TestListFallsBackToTheLastGoodCopyAndSaysSo(t *testing.T) {
	now := freezeClock(t)
	stub := newStub(t, okList)
	c := stub.client(t.TempDir())
	first, err := c.List(context.Background(), Query{})
	if err != nil || first.Cache != nil {
		t.Fatalf("first = %+v err = %v", first, err)
	}
	*now = now.Add(time.Hour)
	stub.srv.Close()
	page, err := c.List(context.Background(), Query{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Cache == nil || page.Cache.Cause != CacheCauseUnreachable || page.Cache.CachedAt != "2026-10-06T12:00:00Z" {
		t.Fatalf("cache note = %+v", page.Cache)
	}
	if len(page.Packages) != 1 || page.Packages[0].Slug != "a/kit" {
		t.Fatalf("packages = %+v", page.Packages)
	}
}

func TestServerErrorsAndUnreadableAnswersFallBackToo(t *testing.T) {
	for name, bad := range map[string]func(http.ResponseWriter){
		"5xx":     func(w http.ResponseWriter) { w.WriteHeader(http.StatusBadGateway) },
		"garbled": func(w http.ResponseWriter) { _, _ = w.Write([]byte("<html>captive portal</html>")) },
	} {
		t.Run(name, func(t *testing.T) {
			var broken atomic.Bool
			stub := newStub(t, func(w http.ResponseWriter, r *http.Request) {
				if broken.Load() {
					bad(w)
					return
				}
				okList(w, r)
			})
			c := stub.client(t.TempDir())
			if _, err := c.List(context.Background(), Query{}); err != nil {
				t.Fatal(err)
			}
			broken.Store(true)
			page, err := c.List(context.Background(), Query{})
			if err != nil || page.Cache == nil || page.Cache.Cause != CacheCauseBadResponse || len(page.Packages) != 1 {
				t.Fatalf("page = %+v err = %v", page, err)
			}
		})
	}
}

func TestClientErrorsAreNotMaskedByTheCache(t *testing.T) {
	var status atomic.Int32
	stub := newStub(t, func(w http.ResponseWriter, r *http.Request) {
		if s := int(status.Load()); s != 0 {
			w.WriteHeader(s)
			return
		}
		okList(w, r)
	})
	c := stub.client(t.TempDir())
	if _, err := c.List(context.Background(), Query{}); err != nil {
		t.Fatal(err)
	}
	status.Store(http.StatusTooManyRequests)
	if _, err := c.List(context.Background(), Query{}); !errors.Is(err, ErrBadResponse) {
		t.Fatalf("err = %v", err)
	}
}

func TestNotModifiedServesTheStoredBodyWithoutANote(t *testing.T) {
	now := freezeClock(t)
	stub := newStub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"v1"` && r.Header.Get("If-Modified-Since") == "Mon, 05 Oct 2026 00:00:00 GMT" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Last-Modified", "Mon, 05 Oct 2026 00:00:00 GMT")
		okList(w, r)
	})
	dir := t.TempDir()
	c := stub.client(dir)
	if _, err := c.List(context.Background(), Query{}); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(48 * time.Hour)
	page, err := c.List(context.Background(), Query{})
	if err != nil || page.Cache != nil || len(page.Packages) != 1 {
		t.Fatalf("page = %+v err = %v", page, err)
	}
	if stub.lastHeader("If-None-Match") != `"v1"` {
		t.Fatalf("no conditional request: %v", stub.last.Load())
	}
	entry, ok := c.cache.load(c.cache.key(c.base.Host, "/v1/packages", listQuery(Query{})))
	if !ok || !entry.FetchedAt.Equal(*now) {
		t.Fatalf("a 304 must renew the copy: %+v ok=%v", entry, ok)
	}
}

func listQuery(q Query) url.Values {
	v := url.Values{}
	v.Set("limit", "24")
	v.Set("offset", "0")
	if q.Q != "" {
		v.Set("q", q.Q)
	}
	return v
}

func TestACopyOlderThanTheLimitIsNotServed(t *testing.T) {
	now := freezeClock(t)
	stub := newStub(t, okList)
	c := stub.client(t.TempDir())
	if _, err := c.List(context.Background(), Query{}); err != nil {
		t.Fatal(err)
	}
	stub.srv.Close()
	*now = now.Add(cacheMaxAge + time.Minute)
	if _, err := c.List(context.Background(), Query{}); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("err = %v", err)
	}
}

func TestCorruptFilesAreIgnored(t *testing.T) {
	freezeClock(t)
	stub := newStub(t, okList)
	dir := t.TempDir()
	c := stub.client(dir)
	if _, err := c.List(context.Background(), Query{}); err != nil {
		t.Fatal(err)
	}
	files := cacheFiles(t, dir)
	if len(files) != 1 {
		t.Fatalf("files = %v", files)
	}
	for _, junk := range []string{"{not json", "", `{"v":9,"key":"x","body":{}}`, `{"v":1,"key":"other","fetchedAt":"2026-10-06T12:00:00Z","body":{}}`, `{"v":1,"fetchedAt":"2026-10-06T12:00:00Z","body":"x"`} {
		if err := os.WriteFile(files[0], []byte(junk), 0o600); err != nil {
			t.Fatal(err)
		}
		saved := stub.srv.Config.Handler
		stub.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) })
		if _, err := c.List(context.Background(), Query{}); !errors.Is(err, ErrBadResponse) {
			t.Fatalf("junk %q: err = %v", junk, err)
		}
		stub.srv.Config.Handler = saved
		page, err := c.List(context.Background(), Query{})
		if err != nil || page.Cache != nil || len(page.Packages) != 1 {
			t.Fatalf("junk %q: page = %+v err = %v", junk, page, err)
		}
	}
}

func TestQueriesDoNotShareACopy(t *testing.T) {
	stub := newStub(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"packages":[{"slug":"a/` + r.URL.Query().Get("q") + `","status":"active"}],"limit":24}`))
	})
	c := stub.client(t.TempDir())
	for _, q := range []string{"alpha", "beta"} {
		if _, err := c.List(context.Background(), Query{Q: q}); err != nil {
			t.Fatal(err)
		}
	}
	stub.srv.Close()
	for _, q := range []string{"alpha", "beta"} {
		page, err := c.List(context.Background(), Query{Q: q})
		if err != nil || page.Cache == nil || page.Packages[0].Slug != "a/"+q {
			t.Fatalf("%s: page = %+v err = %v", q, page, err)
		}
	}
	if _, err := c.List(context.Background(), Query{Q: "gamma"}); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("an unseen query must not borrow another's copy: %v", err)
	}
	if _, err := c.List(context.Background(), Query{Q: "alpha", Offset: 24}); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("a later page must not borrow the first: %v", err)
	}
}

func TestDetailIsCachedPerPackage(t *testing.T) {
	var down atomic.Bool
	stub := newStub(t, func(w http.ResponseWriter, r *http.Request) {
		if down.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/v1/packages/a/")
		_, _ = w.Write([]byte(`{"package":{"slug":"a/` + name + `","status":"active","latestVersion":"1"},"versions":[{"version":"1","source":"s","content_hash":"sha256:ab"}]}`))
	})
	c := stub.client(t.TempDir())
	if _, err := c.Detail(context.Background(), "a/one"); err != nil {
		t.Fatal(err)
	}
	down.Store(true)
	d, err := c.Detail(context.Background(), "a/one")
	if err != nil || d.Cache == nil || d.Approved == nil || d.Approved.ContentHash != "sha256:ab" {
		t.Fatalf("detail = %+v err = %v", d, err)
	}
	if _, err := c.Detail(context.Background(), "a/two"); !errors.Is(err, ErrBadResponse) {
		t.Fatalf("err = %v", err)
	}
}

func TestAGoneEntryDropsItsCopy(t *testing.T) {
	var gone atomic.Bool
	stub := newStub(t, func(w http.ResponseWriter, r *http.Request) {
		if gone.Load() {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		okList(w, r)
	})
	dir := t.TempDir()
	c := stub.client(dir)
	_, _ = c.List(context.Background(), Query{})
	gone.Store(true)
	if _, err := c.List(context.Background(), Query{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	if n := len(cacheFiles(t, dir)); n != 0 {
		t.Fatalf("%d copies left", n)
	}
}

func TestACancelledRequestIsNotAnsweredFromTheCache(t *testing.T) {
	stub := newStub(t, okList)
	c := stub.client(t.TempDir())
	_, _ = c.List(context.Background(), Query{})
	stub.srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if page, err := c.List(ctx, Query{}); err == nil || page.Cache != nil {
		t.Fatalf("page = %+v err = %v", page, err)
	}
}

func TestACopySurvivesARestart(t *testing.T) {
	stub := newStub(t, okList)
	dir := t.TempDir()
	if _, err := stub.client(dir).List(context.Background(), Query{}); err != nil {
		t.Fatal(err)
	}
	stub.srv.Close()
	page, err := stub.client(dir).List(context.Background(), Query{})
	if err != nil || page.Cache == nil {
		t.Fatalf("page = %+v err = %v", page, err)
	}
}

func TestOwnAndTokenRequestsNeverTouchTheCache(t *testing.T) {
	stub := newStub(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"package":{"slug":"a/kit","status":"pending"},"versions":[]}`))
	})
	dir := t.TempDir()
	c := stub.client(dir)
	if _, err := c.Owned(context.Background(), "secret-token", "a/kit"); err != nil {
		t.Fatal(err)
	}
	if n := len(cacheFiles(t, dir)); n != 0 {
		t.Fatalf("an account's own view was cached: %d files", n)
	}
	if _, err := c.Detail(context.Background(), "a/kit"); err == nil {
		t.Fatal("pending package must not read as a detail")
	}
	if stub.lastHeader("Authorization") != "" {
		t.Fatal("a browse request carried a credential")
	}
	for _, f := range cacheFiles(t, dir) {
		raw, _ := os.ReadFile(f)
		if strings.Contains(string(raw), "secret-token") {
			t.Fatal("token reached the cache")
		}
	}
}

func TestConcurrentReadersAndWritersAgree(t *testing.T) {
	stub := newStub(t, okList)
	c := stub.client(t.TempDir())
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Go(func() {
			for j := range 10 {
				page, err := c.List(context.Background(), Query{Q: string(rune('a' + (i+j)%4))})
				if err != nil || len(page.Packages) != 1 {
					t.Errorf("page = %+v err = %v", page, err)
				}
			}
		})
	}
	wg.Wait()
}

func TestTheCacheStaysWithinItsLimits(t *testing.T) {
	now := freezeClock(t)
	stub := newStub(t, okList)
	dir := t.TempDir()
	c := stub.client(dir)
	c.cache.maxEntries = 3
	for i, q := range []string{"a", "b", "c", "d", "e"} {
		*now = now.Add(time.Minute)
		if _, err := c.List(context.Background(), Query{Q: q}); err != nil {
			t.Fatal(err)
		}
		bump := time.Date(2026, 10, 6, 12, i, 0, 0, time.UTC)
		for _, f := range cacheFiles(t, dir) {
			_ = os.Chtimes(f, bump, bump)
		}
	}
	if n := len(cacheFiles(t, dir)); n != 3 {
		t.Fatalf("entries = %d, want 3", n)
	}
	c.cache.maxEntries = 100
	var one int64
	if info, err := os.Stat(cacheFiles(t, dir)[0]); err == nil {
		one = info.Size()
	}
	c.cache.maxBytes = one*2 + one/2
	*now = now.Add(time.Hour)
	if _, err := c.List(context.Background(), Query{Q: "f"}); err != nil {
		t.Fatal(err)
	}
	if n := len(cacheFiles(t, dir)); n != 2 {
		t.Fatalf("entries = %d under a two-entry byte budget", n)
	}
	c.cache.maxBytes = 10
	if _, err := c.List(context.Background(), Query{Q: "g"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.cache.load(c.cache.key(c.base.Host, "/v1/packages", listQuery(Query{Q: "g"}))); ok {
		t.Fatal("a body over the byte cap must not be kept")
	}
}

func TestExpiredFilesArePrunedAndForeignFilesLeftAlone(t *testing.T) {
	freezeClock(t)
	stub := newStub(t, okList)
	dir := t.TempDir()
	c := stub.client(dir)
	if _, err := c.List(context.Background(), Query{Q: "a"}); err != nil {
		t.Fatal(err)
	}
	old := cacheNow().Add(-cacheMaxAge - time.Hour)
	for _, f := range cacheFiles(t, dir) {
		_ = os.Chtimes(f, old, old)
	}
	foreign := filepath.Join(dir, "market", "notes.txt")
	_ = os.WriteFile(foreign, []byte("mine"), 0o600)
	if _, err := c.List(context.Background(), Query{Q: "b"}); err != nil {
		t.Fatal(err)
	}
	if n := len(cacheFiles(t, dir)); n != 1 {
		t.Fatalf("entries = %d, want 1", n)
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Fatal("pruning removed a file it did not write")
	}
}

func TestFileNamesAreSafeOnEveryPlatform(t *testing.T) {
	stub := newStub(t, okList)
	dir := t.TempDir()
	c := stub.client(dir)
	for _, q := range []string{`../../x`, `a:b*c?"<>|`, "日本語 検索", strings.Repeat("长", 300)} {
		if _, err := c.List(context.Background(), Query{Q: q}); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range cacheFiles(t, dir) {
		if !cacheFile.MatchString(filepath.Base(f)) || filepath.Dir(f) != filepath.Join(dir, "market") {
			t.Fatalf("unexpected file %s", f)
		}
	}
	if n := len(cacheFiles(t, dir)); n != 4 {
		t.Fatalf("files = %d", n)
	}
}

func TestAnUnwritableCacheNeverBreaksARead(t *testing.T) {
	stub := newStub(t, okList)
	root := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(root, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	page, err := stub.client(root).List(context.Background(), Query{})
	if err != nil || len(page.Packages) != 1 {
		t.Fatalf("page = %+v err = %v", page, err)
	}
}

func TestNoRootMeansNoCache(t *testing.T) {
	stub := newStub(t, okList)
	c := stub.client("")
	if _, err := c.List(context.Background(), Query{}); err != nil {
		t.Fatal(err)
	}
	stub.srv.Close()
	if _, err := c.List(context.Background(), Query{}); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("err = %v", err)
	}
}

func TestTheKeyIgnoresParameterOrderAndSeparatesHosts(t *testing.T) {
	d := newDiskCache(t.TempDir())
	a := url.Values{"limit": {"24"}, "q": {"x"}}
	b := url.Values{}
	b.Set("q", "x")
	b.Set("limit", "24")
	if d.key("h", "/p", a) != d.key("h", "/p", b) || d.key("h", "/p", a) == d.key("g", "/p", a) {
		t.Fatal("key must be canonical per host, path and parameters")
	}
}

func TestAFreshCopyIsServedWithoutTheNetworkAndWithoutALabel(t *testing.T) {
	now := freezeClock(t)
	stub := newStub(t, okList)
	c := stub.freshClient(t.TempDir())
	if _, err := c.List(context.Background(), Query{}); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(cacheFreshFor - time.Second)
	stub.srv.Close()
	page, err := c.List(context.Background(), Query{})
	if err != nil || page.Cache != nil || len(page.Packages) != 1 || stub.seen.Load() != 1 {
		t.Fatalf("page = %+v err = %v requests = %d", page, err, stub.seen.Load())
	}
}

func TestPastTheFreshWindowTheRegistryIsAskedWithValidators(t *testing.T) {
	now := freezeClock(t)
	stub := newStub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `W/"a"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `W/"a"`)
		okList(w, r)
	})
	c := stub.freshClient(t.TempDir())
	_, _ = c.List(context.Background(), Query{})
	*now = now.Add(cacheFreshFor + time.Second)
	page, err := c.List(context.Background(), Query{})
	if err != nil || page.Cache != nil || stub.seen.Load() != 2 || stub.lastHeader("If-None-Match") != `W/"a"` {
		t.Fatalf("page = %+v err = %v requests = %d", page, err, stub.seen.Load())
	}
}

func TestARepeatedReadSkipsTheFreshWindow(t *testing.T) {
	freezeClock(t)
	var down atomic.Bool
	stub := newStub(t, func(w http.ResponseWriter, r *http.Request) {
		if down.Load() {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		okList(w, r)
	})
	c := stub.freshClient(t.TempDir())
	_, _ = c.List(context.Background(), Query{})
	down.Store(true)
	page, err := c.List(WithRefresh(context.Background()), Query{})
	if err != nil || page.Cache == nil || stub.seen.Load() != 2 {
		t.Fatalf("a repeated read must reach the registry: page = %+v err = %v requests = %d", page, err, stub.seen.Load())
	}
}

func TestAnswersMarkedPrivateOrNoStoreAreNotKept(t *testing.T) {
	for _, header := range []string{"private, no-store", "no-store", "private", "Public, No-Store", "private=\"x\""} {
		var mark atomic.Value
		mark.Store("public, max-age=60")
		stub := newStub(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", mark.Load().(string))
			okList(w, r)
		})
		dir := t.TempDir()
		c := stub.client(dir)
		if _, err := c.List(context.Background(), Query{}); err != nil || len(cacheFiles(t, dir)) != 1 {
			t.Fatalf("a public answer must be kept: %v", err)
		}
		mark.Store(header)
		if _, err := c.List(context.Background(), Query{}); err != nil {
			t.Fatal(err)
		}
		if n := len(cacheFiles(t, dir)); n != 0 {
			t.Fatalf("%q: %d copies kept, want the old one dropped", header, n)
		}
	}
}
