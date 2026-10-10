package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/state/sessioninbox"
)

func TestSteerIntoAPausedQueueAnswersHeldNotRefused(t *testing.T) {
	rig := newChipRig(t)
	if err := rig.s.ctrl.SetInboxPaused(true); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	rig.s.inboxEnqueue(rec, httptest.NewRequest(http.MethodPost, "/inbox/items", strings.NewReader(`{"input":"mid turn","intent":"steer"}`)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("POST /inbox/items = %d %s, want 202", rec.Code, rec.Body.String())
	}
	var receipt sessioninbox.InboxReceipt
	if err := json.Unmarshal(rec.Body.Bytes(), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Disposition != sessioninbox.DispositionQueuedFollowup || !receipt.Paused {
		t.Fatalf("receipt = %+v, want a held follow-up that says the queue is paused", receipt)
	}
	if n := len(rig.s.ctrl.InboxSnapshot().Items); n != 1 {
		t.Fatalf("queue holds %d items, want 1", n)
	}
}
