package boot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

func TestEffectLineReferenceSnapshotAtSendTime(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	rec := &effectRecordingProvider{}
	provider.Register("boot-effect-line-reference", func(provider.Config) (provider.Provider, error) { return rec, nil })
	writeFile(t, dir, "reasonix.toml", `default_model = "test-model"
[agent]
system_prompt = "BASE"
[[providers]]
name = "test-model"
kind = "boot-effect-line-reference"
model = "x"
`)
	writeFile(t, dir, "source.txt", "EXCLUDED-FIRST\nOLD-SELECTED\nEXCLUDED-LAST\n")
	approveWorkspace(t, dir)
	done := make(chan struct{}, 8)
	ctrl, err := Build(t.Context(), Options{Sink: event.FuncSink(func(e event.Event) {
		if e.Kind == event.TurnDone {
			done <- struct{}{}
		}
	})})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()
	if !ctrl.HasRefs("inspect @source.txt:2-2") {
		t.Fatal("range not recognized")
	}
	if err := os.WriteFile(filepath.Join(dir, "source.txt"), []byte("EXCLUDED-FIRST\nNEW-SELECTED\nEXCLUDED-LAST\n"), 0600); err != nil {
		t.Fatal(err)
	}
	line := "inspect @source.txt:2-2 hi @alice:1-2 @10-12 @missing.txt:0-2"
	ctrl.SubmitHTTP(line)
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("range turn did not finish")
	}
	requests := agentRequests(rec.requests())
	if len(requests) == 0 {
		t.Fatal("no provider request")
	}
	text := requestText(requests[0])
	for _, want := range []string{`<file path="source.txt" start-line="2" end-line="2">`, "NEW-SELECTED", line} {
		if !strings.Contains(text, want) {
			t.Errorf("provider request missing %q: %s", want, text)
		}
	}
	for _, absent := range []string{"EXCLUDED-FIRST", "EXCLUDED-LAST", "OLD-SELECTED"} {
		if strings.Contains(text, absent) {
			t.Errorf("provider request leaked %q", absent)
		}
	}
}
