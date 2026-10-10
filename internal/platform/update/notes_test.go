package update

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

// mirror answers every request from handler, whatever address it was aimed at,
// and counts them: the address is a constant, so a test changes the route only.
func mirror(t *testing.T, handler http.HandlerFunc) (*http.Client, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	target, _ := url.Parse(srv.URL)
	return &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		r = r.Clone(r.Context())
		r.URL.Scheme, r.URL.Host = target.Scheme, target.Host
		return http.DefaultTransport.RoundTrip(r)
	})}, &hits
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func notesReq(t *testing.T, c *http.Client) notesRequest {
	return notesRequest{dir: filepath.Join(t.TempDir(), "release-notes"), client: c, userAgent: "test"}
}

func TestNotesURLIsBuiltFromAReleaseVersionOnly(t *testing.T) {
	v, err := notesVersion("v2.31.0")
	if got := notesURL(v); err != nil || got != "https://dl.reasonix.io/studio/notes/2.31.0.md" {
		t.Fatalf("notesURL(v2.31.0) = %q, %v", got, err)
	}
	for _, bad := range []string{"", "latest", "../../x", "2.31", "2.31.0/../../x", "2.31.0?x=1", "https://evil.test/a"} {
		if _, err := notesVersion(bad); !errors.Is(err, ErrNotesBadVersion) {
			t.Errorf("NotesURL(%q) err = %v, want ErrNotesBadVersion", bad, err)
		}
	}
}

func TestSecondReadComesFromDiskWithoutTheNetwork(t *testing.T) {
	c, hits := mirror(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/studio/notes/2.31.0.md" {
			t.Errorf("fetched %s", r.URL.Path)
		}
		_, _ = w.Write([]byte("# notes\n- a\n"))
	})
	rq, st := notesReq(t, c), &notesState{}
	first, err := st.read(context.Background(), rq, "v2.31.0")
	if err != nil || first.Cached || first.Markdown != "# notes\n- a\n" {
		t.Fatalf("first read = %+v, %v", first, err)
	}
	second, err := (&notesState{}).read(context.Background(), rq, "2.31.0")
	if err != nil || !second.Cached || second.Markdown != first.Markdown {
		t.Fatalf("second read = %+v, %v", second, err)
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("the mirror was hit %d times, want 1", n)
	}
	if b, err := os.ReadFile(filepath.Join(rq.dir, "2.31.0.md")); err != nil || string(b) != first.Markdown {
		t.Fatalf("cache file = %q, %v", b, err)
	}
}

func TestConcurrentReadsShareOneRequest(t *testing.T) {
	release := make(chan struct{})
	c, hits := mirror(t, func(w http.ResponseWriter, r *http.Request) {
		<-release
		_, _ = w.Write([]byte("notes"))
	})
	rq, st := notesReq(t, c), &notesState{}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if got, err := st.read(context.Background(), rq, "2.31.0"); err != nil || got.Markdown != "notes" {
				t.Errorf("read = %+v, %v", got, err)
			}
		})
	}
	time.Sleep(100 * time.Millisecond)
	close(release)
	wg.Wait()
	if n := hits.Load(); n != 1 {
		t.Fatalf("the mirror was hit %d times for 8 readers, want 1", n)
	}
}

func TestFailuresAreNotWrittenAndAreNotRepeatedWithinTheWindow(t *testing.T) {
	c, hits := mirror(t, func(w http.ResponseWriter, r *http.Request) { http.Error(w, "boom", http.StatusBadGateway) })
	rq, st := notesReq(t, c), &notesState{}
	for range 3 {
		if _, err := st.read(context.Background(), rq, "2.31.0"); !errors.Is(err, ErrNotesUnreachable) {
			t.Fatalf("err = %v, want ErrNotesUnreachable", err)
		}
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("three reads cost %d requests, want 1", n)
	}
	if _, err := os.Stat(filepath.Join(rq.dir, "2.31.0.md")); !os.IsNotExist(err) {
		t.Fatalf("a failure left a cache file: %v", err)
	}
	rq.retry = true
	if _, err := st.read(context.Background(), rq, "2.31.0"); err == nil || hits.Load() != 2 {
		t.Fatalf("retry did not reach the mirror again (hits %d, err %v)", hits.Load(), err)
	}
}

func TestAFailureIsForgottenAfterTheWindowAndASuccessThenSticks(t *testing.T) {
	var ok atomic.Bool
	c, _ := mirror(t, func(w http.ResponseWriter, r *http.Request) {
		if !ok.Load() {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("back"))
	})
	rq, st := notesReq(t, c), &notesState{}
	if _, err := st.read(context.Background(), rq, "2.31.0"); err == nil {
		t.Fatal("want a failure")
	}
	ok.Store(true)
	st.mu.Lock()
	f := st.failed["2.31.0"]
	f.until = time.Now().Add(-time.Second)
	st.failed["2.31.0"] = f
	st.mu.Unlock()
	if got, err := st.read(context.Background(), rq, "2.31.0"); err != nil || got.Markdown != "back" {
		t.Fatalf("read = %+v, %v", got, err)
	}
}

func TestRefusalsKeepTheirIdentity(t *testing.T) {
	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
		want    error
	}{
		{"absent", func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) }, ErrNotesAbsent},
		{"edge refusal", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", http.StatusForbidden) }, ErrNotesUnreachable},
		{"too large declared", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "999999")
			_, _ = w.Write([]byte("x"))
		}, ErrNotesTooLarge},
		{"too large streamed", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Transfer-Encoding", "chunked")
			_, _ = w.Write([]byte(strings.Repeat("x", NotesMaxBytes+1)))
		}, ErrNotesTooLarge},
		{"empty", func(w http.ResponseWriter, r *http.Request) {}, ErrNotesUnreachable},
		{"not text", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte{0xff, 0xfe, 0x00}) }, ErrNotesUnreachable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := mirror(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/markdown")
				tc.handler(w, r)
			})
			rq := notesReq(t, c)
			if _, err := (&notesState{}).read(context.Background(), rq, "2.31.0"); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if _, err := os.Stat(rq.dir); err == nil {
				if entries, _ := os.ReadDir(rq.dir); len(entries) > 0 {
					t.Fatalf("a refusal left %d cache files", len(entries))
				}
			}
		})
	}
}

func TestACorruptCacheFileIsRefetched(t *testing.T) {
	c, hits := mirror(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("fresh")) })
	rq := notesReq(t, c)
	if err := os.MkdirAll(rq.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rq.dir, "2.31.0.md"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := (&notesState{}).read(context.Background(), rq, "2.31.0")
	if err != nil || got.Markdown != "fresh" || hits.Load() != 1 {
		t.Fatalf("read = %+v, %v (hits %d)", got, err, hits.Load())
	}
}

func TestARedirectOffTheReleaseHostsIsRefused(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("elsewhere")) }))
	t.Cleanup(other.Close)
	c, _ := mirror(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL+"/x", http.StatusFound) })
	if _, err := (&notesState{}).read(context.Background(), notesReq(t, c), "2.31.0"); !errors.Is(err, ErrNotesUnreachable) {
		t.Fatalf("err = %v, want the redirect refused as unreachable", err)
	}
}

func TestACancelledCallerDoesNotFailTheSharedFetch(t *testing.T) {
	release := make(chan struct{})
	c, _ := mirror(t, func(w http.ResponseWriter, r *http.Request) {
		<-release
		_, _ = w.Write([]byte("notes"))
	})
	rq, st := notesReq(t, c), &notesState{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := st.read(ctx, rq, "2.31.0"); done <- err }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got, err := st.read(context.Background(), rq, "2.31.0"); err == nil && got.Markdown == "notes" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the fetch did not complete after its first caller left")
}

func TestVersionRowsMarkOnlyEntriesWithNotes(t *testing.T) {
	rows := versionRows([]IndexEntry{{Version: "v2.2.0", Notes: "https://dl.reasonix.io/studio/notes/2.2.0.md"}, {Version: "v2.1.0"}}, "2.2.0")
	if !rows[0].HasNotes || rows[1].HasNotes {
		t.Fatalf("hasNotes = %v, %v; want true, false", rows[0].HasNotes, rows[1].HasNotes)
	}
}

func TestAResponseOfTheWrongTypeIsRefusedAndNeverWritten(t *testing.T) {
	for _, ct := range []string{"text/html; charset=utf-8", "application/json", "application/octet-stream", ""} {
		c, _ := mirror(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header()["Content-Type"] = []string{ct}
			if ct == "" {
				w.Header()["Content-Type"] = nil
			}
			_, _ = w.Write([]byte("<html>oops</html>"))
		})
		rq := notesReq(t, c)
		_, err := (&notesState{}).read(context.Background(), rq, "2.31.0")
		if !errors.Is(err, ErrNotesUnreachable) {
			t.Errorf("type %q: err = %v, want ErrNotesUnreachable", ct, err)
		}
		if entries, _ := os.ReadDir(rq.dir); len(entries) > 0 {
			t.Errorf("type %q left %d cache files", ct, len(entries))
		}
	}
}

func TestDeclaredMarkdownAndPlainTextAreAccepted(t *testing.T) {
	for _, ct := range []string{"text/markdown; charset=utf-8", "text/plain", "TEXT/Markdown"} {
		c, _ := mirror(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", ct)
			_, _ = w.Write([]byte("# ok"))
		})
		if got, err := (&notesState{}).read(context.Background(), notesReq(t, c), "2.31.0"); err != nil || got.Markdown != "# ok" {
			t.Errorf("type %q: %+v, %v", ct, got, err)
		}
	}
}

func TestVersionsThatCannotBeANameAreRefused(t *testing.T) {
	long := "2.0.0-" + strings.Repeat("a", notesMaxVersion)
	if _, err := notesVersion(long); !errors.Is(err, ErrNotesBadVersion) {
		t.Fatalf("over-long version err = %v", err)
	}
	if _, err := notesVersion("2.0.0-" + strings.Repeat("a", notesMaxVersion-6)); err != nil {
		t.Fatalf("a version at the limit was refused: %v", err)
	}
}

func TestCaseDifferentPrereleasesNeverShareACacheFile(t *testing.T) {
	if notesFileName("2.0.0-RC.1") == notesFileName("2.0.0-rc.1") {
		t.Fatal("two spellings of a prerelease map to one file name")
	}
	if got := notesFileName("2.0.0-RC.1"); got != strings.ToLower(got) {
		t.Fatalf("file name %q has capitals", got)
	}
	if got := notesFileName("2.31.0"); got != "2.31.0.md" {
		t.Fatalf("plain version file name = %q", got)
	}
}
