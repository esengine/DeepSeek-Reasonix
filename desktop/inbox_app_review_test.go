package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/sessioninbox"
)

func TestInboxWailsErrorsUseStableCodes(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code string
	}{
		{name: "paused", err: sessioninbox.ErrPaused, code: "inbox_paused"},
		{name: "item capacity", err: sessioninbox.ErrCapacityItems, code: "inbox_capacity_items"},
		{name: "byte capacity", err: sessioninbox.ErrCapacityBytes, code: "inbox_capacity_bytes"},
		{name: "item too large", err: sessioninbox.ErrItemTooLarge, code: "inbox_item_too_large"},
		{name: "not found", err: sessioninbox.ErrNotFound, code: "inbox_item_not_found"},
		{name: "invalid state", err: sessioninbox.ErrInvalidState, code: "inbox_invalid_state"},
		{name: "newer schema", err: sessioninbox.ErrSchemaReadonly, code: "inbox_schema_readonly"},
		{name: "closed", err: sessioninbox.ErrClosed, code: "inbox_closed"},
		{name: "empty", err: sessioninbox.ErrEmpty, code: "inbox_empty"},
		{name: "idempotency conflict", err: sessioninbox.ErrIdempotencyConflict, code: "inbox_idempotency_conflict"},
		{name: "wrapped sentinel", err: fmt.Errorf("steer: %w", sessioninbox.ErrPaused), code: "inbox_paused"},
		{name: "read only channel", err: errors.New("channel session is read-only"), code: "channel_read_only"},
		{name: "workspace starting", err: errors.New("workspace is still starting"), code: "workspace_starting"},
		{name: "workspace start failed", err: errors.New("workspace failed to start: internal detail"), code: "workspace_start_failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := inboxWailsError(tt.err)
			if got.Error() != "reasonix_error:"+tt.code {
				t.Fatalf("error = %q, want stable code %q", got, tt.code)
			}
			if !errors.Is(got, tt.err) {
				t.Fatalf("wrapped error no longer preserves errors.Is for %v", tt.err)
			}
		})
	}

	unknown := errors.New("filesystem detail: /private/example")
	//nolint:errorlint // Identity is the contract: unknown diagnostics must not be wrapped.
	if got := inboxWailsError(unknown); got != unknown {
		t.Fatalf("unknown diagnostic error = %q, want original error", got)
	}
}

func TestSteerInboxItemPausedReturnsStableCode(t *testing.T) {
	dir := t.TempDir()
	ctrl := control.New(control.Options{
		SessionDir: dir, SessionPath: filepath.Join(dir, "session.jsonl"), Sink: event.Discard,
	})
	if err := ctrl.SetInboxPaused(true); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	app.setTestCtrl(ctrl, "")
	receipt, err := app.EnqueueInboxFollowup("test", "guide the turn", "guide the turn", "paused-steer")
	if err != nil {
		t.Fatal(err)
	}
	failedReceipt, err := app.SteerInboxItem("test", receipt.ItemID)
	if err == nil || err.Error() != "reasonix_error:inbox_paused" {
		t.Fatalf("SteerInboxItem error = %v, want stable paused code", err)
	}
	if failedReceipt.Error != "reasonix_error:inbox_paused" {
		t.Fatalf("SteerInboxItem receipt error = %q, want stable paused code", failedReceipt.Error)
	}
}

func TestEnqueueInboxSteerWhenPausedQueuesFollowup(t *testing.T) {
	dir := t.TempDir()
	ctrl := control.New(control.Options{
		SessionDir: dir, SessionPath: filepath.Join(dir, "session.jsonl"), Sink: event.Discard,
	})
	if err := ctrl.SetInboxPaused(true); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	app.setTestCtrl(ctrl, "")
	receipt, err := app.EnqueueInboxSteer("test", "guide later", "guide later", "paused-enqueue-steer")
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Disposition != string(sessioninbox.DispositionQueuedFollowup) || !receipt.Paused {
		t.Fatalf("EnqueueInboxSteer receipt = %+v, want queued_followup while paused", receipt)
	}
}

func TestDurableInvocationFollowupPreservesEmptyExplicitTask(t *testing.T) {
	dir := t.TempDir()
	ctrl := control.New(control.Options{
		SessionDir: dir, SessionPath: filepath.Join(dir, "session.jsonl"), Sink: event.Discard,
	})
	if err := ctrl.SetInboxPaused(true); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	app.setTestCtrl(ctrl, "")

	rec, err := app.EnqueueInboxFollowupWithInvocations(
		"test", "/init", "", []InvocationRequest{{Name: "init", Kind: "skill"}}, "desktop-submit-1",
	)
	if err != nil {
		t.Fatal(err)
	}
	_, env, err := ctrl.ReadInboxItem(rec.ItemID)
	if err != nil {
		t.Fatal(err)
	}
	if env.SubmitText != "" || env.RawText != "" {
		t.Fatalf("entity-only durable task degraded to text: submit=%q raw=%q", env.SubmitText, env.RawText)
	}
	if len(env.Invocations) != 1 || env.Invocations[0].Name != "init" || env.Invocations[0].Kind != "skill" {
		t.Fatalf("durable invocation metadata = %+v", env.Invocations)
	}
}
