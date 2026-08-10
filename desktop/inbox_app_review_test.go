package main

import (
	"path/filepath"
	"testing"

	"reasonix/internal/control"
	"reasonix/internal/event"
)

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
