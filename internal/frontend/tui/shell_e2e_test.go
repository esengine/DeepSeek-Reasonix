package tui_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"reasonix/internal/frontend/tui"
)

// A `!` command typed here runs on this machine and stays here: the output is
// on screen and the model is not asked about it.
func TestShellCommandDoesNotStartAModelTurn(t *testing.T) {
	c := inProcessKernel(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	updates := c.Subscribe(ctx)
	if err := c.RunShell(ctx, "echo tui-local-shell"); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	for {
		var u tui.Update
		select {
		case u = <-updates:
		case <-ctx.Done():
			t.Fatal("the command never finished")
		}
		if u.Event.Kind == "turn_started" {
			t.Fatalf("a shell command started a model turn: %+v", u.Event)
		}
		if u.Event.Kind == "turn_done" {
			break
		}
	}
	history, err := c.History(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range history {
		if strings.Contains(m.Content, "tui-local-shell") {
			t.Fatalf("the command reached the conversation: %+v", m)
		}
	}
}
