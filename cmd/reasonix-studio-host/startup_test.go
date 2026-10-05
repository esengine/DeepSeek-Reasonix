package main

import (
	"bytes"
	"testing"
	"time"

	"reasonix/internal/assembly/boot"
)

func TestStartupPhasesUseElapsedMilliseconds(t *testing.T) {
	var logs bytes.Buffer
	now := time.Unix(100, 0)
	p := newStartupPhases(&logs, func() time.Time { return now })
	now = now.Add(1250 * time.Millisecond)
	p.mark(startupPage)
	now = now.Add(7 * time.Millisecond)
	p.boot(boot.Phase{Name: "sessions", D: 3 * time.Millisecond})
	p.mark(startupRuntime)
	want := "reasonix-studio-host: startup phase=page elapsed_ms=1250\n" +
		"reasonix-studio-host: startup phase=boot.sessions elapsed_ms=3\n" +
		"reasonix-studio-host: startup phase=runtime elapsed_ms=7\n"
	if got := logs.String(); got != want {
		t.Fatalf("phase log = %q, want %q", got, want)
	}
}
