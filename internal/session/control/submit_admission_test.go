package control

import (
	"os"
	"path/filepath"
	"testing"
)

// A submission is judged by what admission did with it, never by whether a
// turn is still running when the caller looks.
func TestSubmitHTTPAdmissionDistinguishesParkedFromDropped(t *testing.T) {
	observed := make(chan observedTurnFormat, 2)
	gate := &formatTurnDoneGate{
		firstEntered: make(chan struct{}),
		releaseFirst: make(chan struct{}),
		allDone:      make(chan struct{}),
	}
	c := New(Options{Runner: formatRecordingRunner{observed: observed}, Sink: gate})

	if got := c.SubmitHTTPFormat("first turn", ""); got != turnStarted {
		t.Fatalf("first submission = %v, want started", got)
	}
	receiveObservedTurnFormat(t, observed)
	waitForFormatTestSignal(t, gate.firstEntered, "first turn did not enter the finishing window")

	if got := c.SubmitHTTPFormat("second turn", ""); got != turnParked || got.Refused() {
		t.Fatalf("submission in the finishing window = %v, want parked and not refused", got)
	}
	close(gate.releaseFirst)
	if got := receiveObservedTurnFormat(t, observed); got.input == "" {
		t.Fatal("the parked submission never ran")
	}
	waitForFormatTestSignal(t, gate.allDone, "parked turn did not finish")
}

func TestSubmitHTTPAdmissionRefusesWhatWillNeverRun(t *testing.T) {
	t.Run("closed", func(t *testing.T) {
		c := New(Options{Runner: formatRecordingRunner{observed: make(chan observedTurnFormat, 1)}})
		c.Close()
		if got := c.SubmitHTTPFormat("hello", ""); got != turnDroppedClosed || !got.Refused() {
			t.Fatalf("submission to a closed controller = %v, want refused as closed", got)
		}
	})
	t.Run("rotating", func(t *testing.T) {
		c := New(Options{Runner: formatRecordingRunner{observed: make(chan observedTurnFormat, 1)}})
		defer c.Close()
		c.mu.Lock()
		c.gate.rotating = true
		c.mu.Unlock()
		if got := c.SubmitHTTPFormat("hello", ""); got != turnDroppedRotating || !got.Refused() {
			t.Fatalf("submission during a rotation = %v, want refused as rotating", got)
		}
	})
	t.Run("a verb is not a refusal", func(t *testing.T) {
		c := New(Options{Runner: formatRecordingRunner{observed: make(chan observedTurnFormat, 1)}})
		defer c.Close()
		if got := c.SubmitHTTPFormat("/tree", ""); got.Refused() {
			t.Fatalf("management verb = %v, want not refused", got)
		}
	})
	t.Run("a dropped file path is refused like text", func(t *testing.T) {
		c := New(Options{Runner: formatRecordingRunner{observed: make(chan observedTurnFormat, 1)}})
		c.Close()
		path := filepath.Join(t.TempDir(), "note.txt")
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := c.SubmitHTTPFormat(path, ""); got != turnDroppedClosed {
			t.Fatalf("bare file path to a closed controller = %v, want refused as closed", got)
		}
	})
}
