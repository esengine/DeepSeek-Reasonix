package agent

import (
	"context"
	"net/http"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent/testutil"
)

func contextOverflowRejection() error {
	return &provider.APIError{
		Provider: "mock",
		Status:   http.StatusBadRequest,
		Body:     `{"error":{"message":"this model's maximum context length is 32768 tokens","type":"invalid_request_error","code":"context_length_exceeded"}}`,
	}
}

func requestChars(req provider.Request) int {
	total := 0
	for _, msg := range req.Messages {
		total += len(msg.Content)
	}
	return total
}

func TestContextWindowProbeBoundsWhatWasRejectedAndAccepted(t *testing.T) {
	var probe contextWindowProbe
	if got := probe.window(); got != 0 {
		t.Fatalf("window before any rejection = %d, want 0", got)
	}
	probe.noteAccepted(40_000)
	if got := probe.window(); got != 0 {
		t.Fatalf("window from an accepted request alone = %d, want 0", got)
	}

	probe.noteRejected(100_000)
	probe.noteRejected(80_000)
	probe.noteRejected(120_000)
	if got := probe.window(); got != 40_000 {
		t.Fatalf("window = %d, want the largest accepted size", got)
	}

	probe.noteAccepted(90_000) // above the smallest refusal: no longer a bound that fits
	if got := probe.window(); got != 80_000 {
		t.Fatalf("window = %d, want the smallest refused size", got)
	}
}

func TestContextWindowProbeIgnoresARefusalTooSmallToFold(t *testing.T) {
	var probe contextWindowProbe
	probe.noteRejected(minSummarySpanTokens - 1)
	if got := probe.window(); got != 0 {
		t.Fatalf("window = %d, want 0 for a refusal no fold could answer", got)
	}
	probe.noteRejected(0)
	probe.noteAccepted(-5)
	if got := probe.window(); got != 0 {
		t.Fatalf("window = %d, want 0 for meaningless sizes", got)
	}
}

// TestContextOverflowRejectionFoldsAndReplays is the whole point: a provider
// that refuses the request as too long gets a smaller one, not a failed turn.
func TestContextOverflowRejectionFoldsAndReplays(t *testing.T) {
	prov := testutil.NewMock("mock",
		testutil.Turn{StreamError: contextOverflowRejection()},
		testutil.Turn{Text: "digest of the earlier work"},
		testutil.Turn{Text: "recovered"},
	)
	session := foldableSessionOverForce(20)
	a := New(prov, tool.NewRegistry(), session, Options{
		ContextWindow: 40_000,
		RecentKeep:    2,
		ArchiveDir:    testenv.TempDir(t),
	}, event.Discard)

	if err := a.Run(context.Background(), "continue"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	requests := prov.Requests()
	if len(requests) != 3 {
		t.Fatalf("provider saw %d requests, want refused + summary + replay", len(requests))
	}
	if refused, replayed := requestChars(requests[0]), requestChars(requests[2]); replayed >= refused {
		t.Fatalf("replay carried %d chars against the refused %d; the fold freed nothing", replayed, refused)
	}
	messages := session.Snapshot()
	if last := messages[len(messages)-1]; last.Role != provider.RoleAssistant || last.Content != "recovered" {
		t.Fatalf("turn did not end on the replayed answer: %+v", last)
	}
}

// TestContextOverflowProbesAnUndeclaredWindow covers the endpoint that reports
// no context window at all: the refusal is the only size information there is,
// and it is enough to fold against.
func TestContextOverflowProbesAnUndeclaredWindow(t *testing.T) {
	prov := testutil.NewMock("mock",
		testutil.Turn{StreamError: contextOverflowRejection()},
		testutil.Turn{Text: "digest of the earlier work"},
		testutil.Turn{Text: "recovered"},
	)
	session := foldableSessionOverForce(60)
	a := New(prov, tool.NewRegistry(), session, Options{
		RecentKeep: 2,
		ArchiveDir: testenv.TempDir(t),
	}, event.Discard)
	if got := a.window().effectiveContextWindow(); got != 0 {
		t.Fatalf("window before the refusal = %d, want none", got)
	}

	if err := a.Run(context.Background(), "continue"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := a.window().effectiveContextWindow(); got <= 0 {
		t.Fatal("the refusal taught the agent nothing about the window")
	}
	requests := prov.Requests()
	if len(requests) != 3 {
		t.Fatalf("provider saw %d requests, want refused + summary + replay", len(requests))
	}
	if refused, replayed := requestChars(requests[0]), requestChars(requests[2]); replayed >= refused {
		t.Fatalf("replay carried %d chars against the refused %d; the fold freed nothing", replayed, refused)
	}
}

// TestContextOverflowWithNothingToFoldFailsTheTurn keeps the recovery honest:
// resending a request the provider already refused would spend a call to be
// told the same thing.
func TestContextOverflowWithNothingToFoldFailsTheTurn(t *testing.T) {
	prov := testutil.NewMock("mock", testutil.Turn{StreamError: contextOverflowRejection()})
	a := New(prov, tool.NewRegistry(), sessionstore.NewSession("system"), Options{
		ContextWindow: 40_000,
		ArchiveDir:    testenv.TempDir(t),
	}, event.Discard)

	err := a.Run(context.Background(), "hello")
	if !provider.IsContextOverflow(err) {
		t.Fatalf("Run error = %v, want the provider's overflow rejection", err)
	}
	if got := len(prov.Requests()); got != 1 {
		t.Fatalf("provider saw %d requests, want only the refused one", got)
	}
}

// TestContextOverflowRecoveryIsOneShotPerRound stops a fold that did not reach
// far enough from becoming a replay loop.
func TestContextOverflowRecoveryIsOneShotPerRound(t *testing.T) {
	prov := testutil.NewMock("mock",
		testutil.Turn{StreamError: contextOverflowRejection()},
		testutil.Turn{Text: "digest of the earlier work"},
		testutil.Turn{StreamError: contextOverflowRejection()},
	)
	session := foldableSessionOverForce(20)
	a := New(prov, tool.NewRegistry(), session, Options{
		ContextWindow: 40_000,
		RecentKeep:    2,
		ArchiveDir:    testenv.TempDir(t),
	}, event.Discard)

	err := a.Run(context.Background(), "continue")
	if !provider.IsContextOverflow(err) {
		t.Fatalf("Run error = %v, want the second rejection to end the turn", err)
	}
	if got := len(prov.Requests()); got != 3 {
		t.Fatalf("provider saw %d requests, want refused + summary + one replay", got)
	}
	if text := strings.TrimSpace(latestDigest(a.sess.win.compactionState.Projection.Messages)); text == "" {
		t.Fatal("the recovery fold left no projection behind")
	}
}
