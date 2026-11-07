package control

import (
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/state/sessionstore"
)

// coldResumeOffer resumes a saved session through a cold cache and returns the
// one cold-resume payload it announced.
func coldResumeOffer(t *testing.T, msgs []provider.Message, fold *sessionstore.ContextProjection) event.ColdResume {
	t.Helper()
	dir := testenv.TempDir(t)
	saved := sessionstore.NewSession("sys")
	for _, m := range msgs {
		saved.Add(m)
	}
	path := sessionstore.NewSessionPath(dir, "test")
	if err := saved.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := sessionstore.EnsureBranchMeta(path); err != nil {
		t.Fatalf("meta: %v", err)
	}
	if fold != nil {
		if err := sessionstore.SaveCompactionState(path, sessionstore.CompactionState{SchemaVersion: sessionstore.CompactionStateSchemaV1, Projection: *fold}); err != nil {
			t.Fatalf("sidecar: %v", err)
		}
	}
	var got []event.ColdResume
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind != event.Notice || e.Code != event.NoticeCodeColdResume {
			return
		}
		p, ok := event.DecodeColdResume(e.Detail)
		if !ok {
			t.Fatalf("cold-resume detail %q is not a ColdResume payload", e.Detail)
		}
		got = append(got, p)
	})
	exec := agent.New(nil, nil, sessionstore.NewSession("sys"), agent.Options{ContextWindow: 1_000_000, ArchiveDir: dir}, event.Discard)
	c := New(Options{Executor: exec, SessionDir: dir, Label: "test", Sink: sink})
	defer c.Close()
	c.testCacheColdAfter = -1
	loaded, err := sessionstore.LoadSession(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	c.Resume(loaded, path)
	if len(got) != 1 {
		t.Fatalf("resume announced %d cold-resume notices, want 1", len(got))
	}
	return got[0]
}

func bigHistory() []provider.Message {
	var out []provider.Message
	for i := range 8 {
		out = append(out,
			provider.Message{Role: provider.RoleUser, Content: strings.Repeat("look at module "+string(rune('a'+i))+" and its callers ", 1500)},
			provider.Message{Role: provider.RoleAssistant, Content: strings.Repeat("the module reads its config once and caches it ", 1500)},
		)
	}
	return out
}

func TestColdResumeOffersCompactionWhenTheResendIsLarge(t *testing.T) {
	p := coldResumeOffer(t, bigHistory(), nil)
	if p.Tokens < coldResumeCompactFloor {
		t.Fatalf("fixture resends %d tokens, want at least the %d floor", p.Tokens, coldResumeCompactFloor)
	}
	if !p.Compact {
		t.Errorf("a %d-token cold resend was not offered a fold", p.Tokens)
	}
	if p.Window != 1_000_000 || p.IdleSeconds < 60 {
		t.Errorf("payload = %+v, want the executor's window and a whole-minute idle", p)
	}
}

func TestColdResumeDoesNotOfferCompactionForASmallSession(t *testing.T) {
	p := coldResumeOffer(t, []provider.Message{{Role: provider.RoleUser, Content: "task"}, {Role: provider.RoleAssistant, Content: "ok"}}, nil)
	if p.Compact {
		t.Errorf("a %d-token resend was offered a fold", p.Tokens)
	}
}

// A fold already saved beside the session is what the next request sends, so
// the offer reads that size and not the history the fold replaced.
func TestColdResumeDoesNotOfferCompactionOverAReusedFold(t *testing.T) {
	history := bigHistory()
	canonical := append([]provider.Message{{Role: provider.RoleSystem, Content: "sys"}}, history...)
	covered := len(canonical)
	p := coldResumeOffer(t, history, &sessionstore.ContextProjection{
		Messages:          []provider.Message{{Role: provider.RoleSystem, Content: "sys"}, {Role: provider.RoleUser, Content: "summary of the module review"}},
		CoveredCount:      covered,
		CoveredPrefixHash: sessionstore.CoveredPrefixHash(canonical, covered),
	})
	if p.Compact || p.Tokens >= coldResumeCompactFloor {
		t.Errorf("payload = %+v over a reused fold, want no offer and the fold's size", p)
	}
}

func TestColdResumeNoticeFloor(t *testing.T) {
	for _, tc := range []struct {
		tokens int
		want   bool
	}{{coldResumeCompactFloor - 1, false}, {coldResumeCompactFloor, true}} {
		e := coldResumeNotice(26*time.Hour, tc.tokens, 1_000_000)
		p, ok := event.DecodeColdResume(e.Detail)
		if !ok || p.Compact != tc.want {
			t.Errorf("tokens=%d: payload=%+v ok=%v, want compact=%v", tc.tokens, p, ok, tc.want)
		}
		if p.IdleSeconds != int((26 * time.Hour).Seconds()) {
			t.Errorf("idle = %d, want 26h in seconds", p.IdleSeconds)
		}
	}
}
