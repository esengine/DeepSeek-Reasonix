package feedback

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

const installMine = `{"items":[{"receipt":"FB-AAAA-0001","category":"bug","titleSnippet":"from here","status":"received","createdAt":"2026-10-01T08:00:00Z","updatedAt":"2026-10-01T08:00:00Z","replies":[]}]}`

const accountFixture = `{"installs":[{"id":"aaaaaaaaaaaa","linkedAt":"2026-10-02T00:00:00Z","current":true},{"id":"bbbbbbbbbbbb","linkedAt":"2026-10-03T00:00:00Z","current":false}],
"items":[
 {"receipt":"FB-AAAA-0001","category":"bug","titleSnippet":"from here","status":"received","createdAt":"2026-10-01T08:00:00Z","updatedAt":"2026-10-01T08:00:00Z","replies":[],"linkedHere":true},
 {"receipt":"FB-BBBB-0002","category":"idea","titleSnippet":"from the laptop","status":"needs_info","needsInput":true,"createdAt":"2026-10-02T08:00:00Z","updatedAt":"2026-10-02T09:00:00Z","replies":[{"id":5,"author":"maintainer","body":"Which OS?","createdAt":"2026-10-02T09:00:00Z"}],"linkedHere":false}]}`

type unionKernel struct {
	mu      sync.Mutex
	paths   []string
	account string
	acode   int
}

func (k *unionKernel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	k.mu.Lock()
	k.paths = append(k.paths, r.URL.Path)
	account, acode := k.account, k.acode
	k.mu.Unlock()
	switch r.URL.Path {
	case "/v1/feedback/mine":
		_, _ = io.WriteString(w, installMine)
	case "/v1/feedback/account/mine":
		if acode != 0 {
			w.WriteHeader(acode)
			_, _ = io.WriteString(w, `{"error":{"code":"feedback.account_required"}}`)
			return
		}
		_, _ = io.WriteString(w, account)
	default:
		_, _ = io.WriteString(w, "{}")
	}
}

func (k *unionKernel) reads() string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return strings.Join(k.paths, " ")
}

func unionService(t *testing.T, linked bool, assertion string) (*Service, *unionKernel) {
	t.Helper()
	k := &unionKernel{account: accountFixture}
	srv := httptest.NewServer(k)
	t.Cleanup(srv.Close)
	cfg := Config{Home: testenv.TempDir(t), Base: srv.URL, Backoff: []time.Duration{}}
	if assertion != "" {
		cfg.Assert = func(context.Context) (string, error) { return assertion, nil }
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.store.update(func(st *state) error {
		st.InstallID, st.InstallToken, st.Linked = "install-abcdefghij", "tok", linked
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return s, k
}

func TestAnInstallThatLinkedListsTheReportsOfItsSiblingsToo(t *testing.T) {
	s, k := unionService(t, true, "assert")
	got, err := s.ListMine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 2 {
		t.Fatalf("items = %+v, want this install's and the laptop's", got.Items)
	}
	by := map[string]Item{}
	for _, it := range got.Items {
		by[it.Receipt] = it
	}
	if by["FB-AAAA-0001"].OtherInstall || !by["FB-BBBB-0002"].OtherInstall {
		t.Fatalf("other-install marks = %v / %v", by["FB-AAAA-0001"].OtherInstall, by["FB-BBBB-0002"].OtherInstall)
	}
	if !by["FB-BBBB-0002"].NeedsInput || len(by["FB-BBBB-0002"].Replies) != 1 || got.Unread == 0 {
		t.Fatalf("the sibling's reply is not visible: %+v unread=%d", by["FB-BBBB-0002"], got.Unread)
	}
	if !strings.Contains(k.reads(), "/v1/feedback/account/mine") {
		t.Fatalf("the account view was never read: %s", k.reads())
	}
	again, _ := s.Item("FB-BBBB-0002")
	if again.Receipt == "" {
		t.Fatal("the sibling's report was not remembered here")
	}
}

func TestAnInstallThatNeverLinkedReadsNoAccountView(t *testing.T) {
	s, k := unionService(t, false, "assert")
	got, err := s.ListMine(context.Background())
	if err != nil || len(got.Items) != 1 {
		t.Fatalf("items = %+v err = %v", got.Items, err)
	}
	if strings.Contains(k.reads(), "account") {
		t.Fatalf("an unlinked install asked for the account view: %s", k.reads())
	}
}

func TestNoSignInOrAFailedAccountReadNeverCostsThePersonTheirOwnList(t *testing.T) {
	s, _ := unionService(t, true, "")
	if got, err := s.ListMine(context.Background()); err != nil || len(got.Items) != 1 {
		t.Fatalf("signed out: %+v %v", got.Items, err)
	}
	s, k := unionService(t, true, "assert")
	k.acode = http.StatusUnauthorized
	if got, err := s.ListMine(context.Background()); err != nil || len(got.Items) != 1 {
		t.Fatalf("account refused: %+v %v", got.Items, err)
	}
}

func TestTheLinkIsRememberedFromLinkAndUnlinkAndFromWhatTheServiceSays(t *testing.T) {
	s, k := unionService(t, true, "assert")
	k.account = `{"installs":[{"id":"bbbbbbbbbbbb","linkedAt":"2026-10-03T00:00:00Z","current":false}],"items":[]}`
	if _, err := s.ListMine(context.Background()); err != nil {
		t.Fatal(err)
	}
	st, _ := s.store.load()
	if st.Linked {
		t.Fatal("the service says this install is not linked, but it is still remembered as linked")
	}
	s, _ = unionService(t, false, "assert")
	if err := s.Link(context.Background()); err != nil {
		t.Fatal(err)
	}
	if st, _ := s.store.load(); !st.Linked {
		t.Fatal("linking was not remembered")
	}
	if err := s.Unlink(context.Background()); err != nil {
		t.Fatal(err)
	}
	if st, _ := s.store.load(); st.Linked {
		t.Fatal("unlinking was not remembered")
	}
}
