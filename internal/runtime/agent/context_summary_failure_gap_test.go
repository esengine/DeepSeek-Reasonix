package agent

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent/testutil"
	"reasonix/internal/state/sessionstore"
)

// A summary that fails under pressure is silent and suppresses retries until the
// input grows by compactTrigger/8. With the failure recorded near the trigger,
// that release point sits at or past the window, so the next turn's prompt goes
// out uncompacted and a relay that reports overflow in prose only is not
// recovered either.
func TestPressureSummaryFailureLeavesNextTurnOverWindow(t *testing.T) {
	const window = 20000
	var sent []int
	var summaries int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		tok := charsOf(decodeMessages(body)) / 4
		if isSummarizeRequest(body) {
			summaries++
			http.Error(w, `{"error":{"message":"summarizer unavailable"}}`, http.StatusBadRequest)
			return
		}
		sent = append(sent, tok)
		if tok > window {
			http.Error(w, `{"error":{"message":"maximum context length exceeded"}}`, http.StatusBadRequest)
			return
		}
		writeSSE(w, t, streamChunk(deltaText("ok")), finishChunk("stop"), usageChunk(tok, 5, 0, tok))
	}))
	defer srv.Close()

	a, _ := newAgent(t, srv.URL, tool.NewRegistry(), window, 4)
	var maint []string
	a.svc.sink = event.FuncSink(func(e event.Event) {
		if e.Kind == event.ContextMaintenanceEvent && e.Maintenance != nil {
			maint = append(maint, e.Maintenance.Status+"/"+e.Maintenance.Code)
		}
	})
	for range 14 {
		a.sess.conversation.Add(provider.Message{Role: provider.RoleUser, Content: "old question"})
		a.sess.conversation.Add(provider.Message{Role: provider.RoleAssistant, Content: strings.Repeat("answer words ", 400)})
	}
	t.Logf("trigger=%d window=%d prefilled=%d", a.CompactTrigger(), window, a.ContextUsedTokens())

	if err := a.Run(context.Background(), "first turn"); err != nil {
		t.Fatalf("turn 1: %v", err)
	}
	t.Logf("after turn 1: est=%d summaries=%d maint=%v sent=%v", a.ContextUsedTokens(), summaries, maint, sent)

	err := a.Run(context.Background(), strings.Repeat("pasted log line ", 400))
	t.Logf("after turn 2: err=%v summaries=%d maint=%v sent=%v", err, summaries, maint, sent)
	for _, tok := range sent {
		if tok > window {
			t.Errorf("a request of ~%d tokens reached a %d-token window with no fold attempted after the first failure (summaries=%d)", tok, window, summaries)
		}
	}
	if err != nil {
		t.Errorf("turn 2 failed: %v", err)
	}
}

func TestRetryReleaseStaysShortOfTheHardCeiling(t *testing.T) {
	a := newPressureAgent(t, &failingSummaryProvider{}).window()
	trigger, hard := a.compactTrigger(), a.hardInputCeiling()
	for failedAt := trigger; failedAt <= hard+500; failedAt += 250 {
		step := a.retryGrowthStep(failedAt)
		if step < 1 || step > trigger/8 {
			t.Fatalf("step %d at %d outside [1, trigger/8=%d]", step, failedAt, trigger/8)
		}
		if failedAt < hard-1 && failedAt+step >= hard {
			t.Fatalf("failure at %d releases at %d, not short of the ceiling %d", failedAt, failedAt+step, hard)
		}
	}
}

func TestPressureHoldLiftsAtTheHardCeiling(t *testing.T) {
	a := newPressureAgent(t, &failingSummaryProvider{}).window()
	hard := a.hardInputCeiling()
	failedAt := hard - 400
	r := &sessionstore.ContextMaintenanceReceipt{Status: "failed", Action: "summary", Code: string(FailSummaryFailed), InputTokens: failedAt}
	if !a.blockedReceiptHolds(r, failedAt) {
		t.Fatal("an unchanged input must stay held")
	}
	if a.blockedReceiptHolds(r, hard) {
		t.Fatalf("a hold survived an input at the hard ceiling %d", hard)
	}
}

func TestRefusedOverWindowReadsStatusAndSizeNotWords(t *testing.T) {
	a := newPressureAgent(t, &failingSummaryProvider{}).window()
	over := &samplingRequest{req: provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: strings.Repeat("x", 4*a.effectiveContextWindow()+400)}}}}
	under := &samplingRequest{req: provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "short"}}}}
	refusal := func(status int, body string) error {
		return &provider.APIError{Provider: "relay", Status: status, Body: body}
	}
	cases := []struct {
		name string
		req  *samplingRequest
		err  error
		want bool
	}{
		{"400 over the window, no code", over, refusal(400, `{"error":{"message":"bad request"}}`), true},
		{"413 over the window", over, refusal(413, ""), true},
		{"400 under the window", under, refusal(400, `{"error":{"message":"maximum context length exceeded"}}`), false},
		{"401 over the window", over, refusal(401, ""), false},
		{"500 over the window", over, refusal(500, ""), false},
		{"transport error over the window", over, io.ErrUnexpectedEOF, false},
	}
	for _, c := range cases {
		if got := a.refusedOverWindow(c.req, c.err); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestProseOnlyOverflowRefusalFoldsAndReplays(t *testing.T) {
	prov := testutil.NewMock("mock", testutil.Turn{Text: "digest of the earlier work"})
	a := New(prov, tool.NewRegistry(), foldableSessionOverForce(20), Options{
		ContextWindow: 40_000, RecentKeep: 2, ArchiveDir: testenv.TempDir(t),
	}, event.Discard)
	w := a.window()
	refused := &samplingRequest{req: provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: strings.Repeat("x", 4*45_000)}}}}
	prose := &provider.APIError{Provider: "relay", Status: 400, Body: `{"error":{"message":"upstream rejected the request"}}`}
	if !w.recoverContextOverflow(context.Background(), refused, prose) {
		t.Fatal("a 400 over the window with no overflow code was not recovered")
	}
	if len(prov.Requests()) != 1 {
		t.Fatalf("recovery made %d provider requests, want the one summary", len(prov.Requests()))
	}
}
