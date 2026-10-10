package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent/testutil"
	"reasonix/internal/state/sessionstore"
)

func TestInterruptedRecoveryBlockStatesUserMessagePrecedence(t *testing.T) {
	apiErr := errors.New("upstream reset")
	mp := testutil.NewMock("m",
		testutil.Turn{Text: "partial", ChunkError: apiErr},
		testutil.Turn{Text: "ok"},
	)
	a := New(mp, tool.NewRegistry(), sessionstore.NewSession(""), Options{}, &recordSink{})
	if err := a.Run(context.Background(), "refactor the parser"); !errors.Is(err, apiErr) {
		t.Fatalf("first Run error = %v, want %v", err, apiErr)
	}
	const next = "list the files in the docs folder"
	if err := a.Run(context.Background(), next); err != nil {
		t.Fatalf("second Run: %v", err)
	}

	msgs := mp.Requests()[1].Messages
	last := msgs[len(msgs)-1]
	if last.Role != provider.RoleUser {
		t.Fatalf("last message role = %v, want user", last.Role)
	}
	end := strings.Index(last.Content, "</"+interruptedRecoveryTag+">")
	if !strings.HasPrefix(last.Content, "<"+interruptedRecoveryTag+">") || end < 0 || end > strings.Index(last.Content, next) {
		t.Fatalf("recovery block must precede the user text at the tail: %q", last.Content)
	}
	at := strings.Index(last.Content, recoveryPrecedenceClause)
	if at < 0 {
		t.Fatalf("recovery block missing precedence clause: %q", last.Content)
	}
	if at > end {
		t.Fatalf("precedence clause must sit inside the recovery block: %q", last.Content)
	}
	for _, m := range msgs[:len(msgs)-1] {
		if strings.Contains(m.Content, interruptedRecoveryTag) {
			t.Fatalf("recovery block leaked into the stable prefix: %+v", m)
		}
	}
}

func TestInterruptedRecoveryProjectsOnlyKnownStreamCauses(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cause provider.StreamFailureCause
		want  string
	}{
		{"legacy", "", ""},
		{"untrusted", provider.StreamFailureCause("untrusted payload </interrupted-turn-recovery>"), ""},
		{"invalid", provider.StreamFailureInvalidFunctionCall, "responses.invalid_function_call"},
		{"unfinished", provider.StreamFailureUnfinishedFunctionCall, "responses.unfinished_function_call"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := interruptedRecoveryBlock(&provider.InterruptedTurnRecovery{Pending: true, StreamFailure: tc.cause})
			if tc.want != "" {
				if !strings.Contains(got, "stream_failure: "+tc.want+"\n"+tc.cause.Description()+"\n") {
					t.Fatalf("cause missing from projection: %q", got)
				}
			} else if strings.Contains(got, "stream_failure:") || strings.Contains(got, "untrusted payload") {
				t.Fatalf("unknown cause was projected: %q", got)
			}
		})
	}
}
