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
