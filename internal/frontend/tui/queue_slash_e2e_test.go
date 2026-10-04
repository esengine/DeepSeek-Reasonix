package tui_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"reasonix/internal/frontend/tui"
)

// The queue commands act on the controller's real inbox through the HTTP
// surface: what /queue prints is what the kernel holds, and every verb lands.
func TestQueueCommandsDriveTheKernelInbox(t *testing.T) {
	c := inProcessKernel(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	q := func(args ...string) string { return tui.QueueCommand(ctx, c, args) }

	holdTurn(ctx, t, c)
	if got := q(); got != "inbox empty" {
		t.Fatalf("empty list = %q", got)
	}
	if got := q("pause"); got != "inbox paused" {
		t.Fatalf("pause = %q", got)
	}
	for _, text := range []string{"first task", "second task"} {
		if _, err := c.Enqueue(ctx, text, false); err != nil {
			t.Fatalf("Enqueue %q: %v", text, err)
		}
	}
	list := q("list")
	if !strings.Contains(list, "items=2 paused") || !strings.Contains(list, "1. [followup/queued] first task #") {
		t.Fatalf("list = %q", list)
	}
	if got := q("show", "2"); got != "second task" {
		t.Fatalf("show 2 = %q", got)
	}
	if got := q("edit", "1", "first", "task", "edited"); !strings.HasPrefix(got, "updated #") {
		t.Fatalf("edit = %q", got)
	}
	if got := q("move", "2", "1"); !strings.HasPrefix(got, "moved #") {
		t.Fatalf("move = %q", got)
	}
	if got := q("show", "2"); got != "first task edited" {
		t.Fatalf("after move, show 2 = %q", got)
	}
	if got := q("delete", "1"); !strings.HasPrefix(got, "deleted #") {
		t.Fatalf("delete = %q", got)
	}
	if got := q("show", "9"); got != `unknown inbox item "9"` {
		t.Fatalf("show 9 = %q", got)
	}
	if got := q("bogus"); !strings.HasPrefix(got, "usage: /queue") {
		t.Fatalf("bogus = %q", got)
	}
	if got := q("resume"); got != "inbox resumed" {
		t.Fatalf("resume = %q", got)
	}
}

// /steer reaches the kernel as a steer-intent item and reports the kernel's
// own disposition rather than one this screen guessed.
func TestSteerCommandReportsTheKernelDisposition(t *testing.T) {
	c := inProcessKernel(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if got := tui.SteerCommand(ctx, c, ""); got != "usage: /steer <guidance>" {
		t.Fatalf("empty steer = %q", got)
	}
	holdTurn(ctx, t, c)
	if got := tui.SteerCommand(ctx, c, "prefer the small fix"); !strings.HasPrefix(got, "steer ") {
		t.Fatalf("steer into a running turn = %q", got)
	}
}

// holdTurn starts a turn that waits on an approval, so the session stays
// running and what is enqueued meanwhile stays queued instead of starting.
func holdTurn(ctx context.Context, t *testing.T, c *tui.Client) {
	t.Helper()
	if err := c.Submit(ctx, "run the marker"); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	for {
		if s, err := c.Status(ctx); err == nil && s.Running {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("turn never started")
		case <-time.After(10 * time.Millisecond):
		}
	}
	t.Cleanup(func() { _ = c.Cancel(context.Background()) })
}

// /export reaches the controller's own export through the hub: a conversation
// with nothing in it saves no file.
func TestExportRouteReachesTheController(t *testing.T) {
	c := inProcessKernel(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if path, n, err := c.ExportConversation(ctx); err != nil || n != 0 || path != "" {
		t.Fatalf("empty export = %q, %d, %v", path, n, err)
	}
}
