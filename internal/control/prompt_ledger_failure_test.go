package control

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/mcpinteraction"
	"reasonix/internal/turnevent"
)

func blockPromptTestLedger(t *testing.T, c *Controller, root string) {
	t.Helper()
	ledger := c.turnEventLedger()
	if ledger == nil {
		t.Fatal("controller did not open a turn ledger")
	}
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root, []byte("block future WAL opens"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func awaitPromptLedgerTest[T any](t *testing.T, ch <-chan T, description string) T {
	t.Helper()
	select {
	case result := <-ch:
		return result
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
		var zero T
		return zero
	}
}

func TestCancelLedgerFailureReturnsAndCancelsTurn(t *testing.T) {
	root := filepath.Join(t.TempDir(), "session-dir")
	started := make(chan context.Context, 1)
	finished := make(chan error, 1)
	c := New(Options{SessionDir: root, SessionPath: filepath.Join(root, "session.jsonl")})
	t.Cleanup(c.Close)
	c.runGuarded(func(ctx context.Context) error {
		started <- ctx
		<-ctx.Done()
		finished <- ctx.Err()
		return ctx.Err()
	})
	ctx := awaitPromptLedgerTest(t, started, "turn start")
	blockPromptTestLedger(t, c, root)
	returned := make(chan struct{})
	go func() {
		c.Cancel()
		close(returned)
	}()
	awaitPromptLedgerTest(t, returned, "Cancel to return after WAL failure")
	awaitPromptLedgerTest(t, ctx.Done(), "turn context cancellation")
	if err := awaitPromptLedgerTest(t, finished, "cancelled turn body"); !errors.Is(err, context.Canceled) {
		t.Fatalf("turn error = %v, want context cancellation", err)
	}
	if err := c.turnEventLedgerError(); !errors.Is(err, turnevent.ErrTurnLedgerUnavailable) {
		t.Fatalf("ledger error = %v, want storage failure", err)
	}
	waitIdle(t, c)
}

func TestResolvePromptExactLedgerFailureCancelsWithoutAnswer(t *testing.T) {
	tests := []struct {
		name   string
		kind   PromptKind
		answer PromptAnswer
		wait   func(context.Context, *Controller) (bool, error)
	}{
		{
			name: "ask", kind: PromptAsk,
			answer: PromptAnswer{Questions: []event.AskAnswer{{QuestionID: "q1", Selected: []string{"A"}}}},
			wait: func(ctx context.Context, c *Controller) (bool, error) {
				answers, err := c.Ask(ctx, askProbeQuestions())
				return len(answers) != 0, err
			},
		},
		{
			name: "approval", kind: PromptApproval, answer: PromptAnswer{Allow: true},
			wait: func(ctx context.Context, c *Controller) (bool, error) {
				allow, _, err := c.requestApprovalWithReason(ctx, "bash", "echo test", nil, "test")
				return allow, err
			},
		},
		{
			name: "mcp", kind: PromptMCP, answer: PromptAnswer{Action: mcpinteraction.ActionAccept},
			wait: func(ctx context.Context, c *Controller) (bool, error) {
				result, err := c.Interact(ctx, mcpinteraction.Request{Server: "test", Mode: "form", Message: "Confirm?"})
				return result.Action == mcpinteraction.ActionAccept, err
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "session-dir")
			requests := make(chan event.Event, 1)
			started := make(chan context.Context, 1)
			type outcome struct {
				answered bool
				err      error
			}
			finished := make(chan outcome, 1)
			c := New(Options{
				SessionDir: root, SessionPath: filepath.Join(root, "session.jsonl"),
				Sink: event.FuncSink(func(e event.Event) {
					switch e.Kind {
					case event.AskRequest, event.ApprovalRequest, event.MCPInteractionRequest:
						requests <- e
					}
				}),
			})
			t.Cleanup(c.Close)
			c.SetTurnEventRoutingMetadata("ledger-failure-test", "")
			c.runGuarded(func(ctx context.Context) error {
				started <- ctx
				answered, err := tt.wait(ctx, c)
				finished <- outcome{answered: answered, err: err}
				return err
			})
			ctx := awaitPromptLedgerTest(t, started, "turn start")
			request := awaitPromptLedgerTest(t, requests, "prompt publication")
			identity := PromptIdentity{PromptID: request.ItemID, TurnID: request.TurnID, RuntimeEpoch: "ledger-failure-test", Kind: tt.kind}
			blockPromptTestLedger(t, c, root)
			resolved := make(chan error, 1)
			go func() { resolved <- c.ResolvePromptExact(identity, tt.answer) }()
			if err := awaitPromptLedgerTest(t, resolved, "failed resolution to return"); !errors.Is(err, turnevent.ErrTurnLedgerUnavailable) {
				t.Fatalf("ResolvePromptExact error = %v, want storage failure", err)
			}
			awaitPromptLedgerTest(t, ctx.Done(), "turn context cancellation")
			result := awaitPromptLedgerTest(t, finished, "prompt waiter cancellation")
			if result.answered || !errors.Is(result.err, context.Canceled) {
				t.Fatalf("prompt outcome = %+v, want cancellation without an answer", result)
			}
			if pending := c.PendingPromptIdentities(); len(pending) != 0 {
				t.Fatalf("failed turn left pending identities: %+v", pending)
			}
			if c.PendingPrompt() {
				t.Fatal("failed turn left a pending approval or question")
			}
			if err := c.ResolvePromptExact(identity, tt.answer); err == nil {
				t.Fatal("prompt accepted a later answer after ledger failure")
			}
			waitIdle(t, c)
		})
	}
}
