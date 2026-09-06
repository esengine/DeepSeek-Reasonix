package serve

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"reasonix/internal/control"
	"reasonix/internal/sessioninbox"
)

// Deleting a queued line has two answers a frontend has to tell apart: it is
// gone, or the turn read it a moment before you asked. Only one of them is
// worth a word to the person waiting, and neither is a state name. This pinned
// the second as coded and the first as not, which is the half that was wrong:
// telling them apart is what the pair is for.
func TestQueueRemovalRefusalsCarryTheirOwnCodes(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{control.ErrSteerApplied, http.StatusConflict, "steer.already_applied"},
		{fmt.Errorf("wrapped: %w", control.ErrSteerApplied), http.StatusConflict, "steer.already_applied"},
		{sessioninbox.ErrNotFound, http.StatusConflict, "inbox.not_found"},
	} {
		rec := httptest.NewRecorder()
		writeInboxError(rec, tc.err)
		if rec.Code != tc.status {
			t.Fatalf("%v = %d, want %d", tc.err, rec.Code, tc.status)
		}
		var body Reason
		_ = json.NewDecoder(rec.Body).Decode(&body)
		if body.Code != tc.code {
			t.Fatalf("%v carried code %q, want %q", tc.err, body.Code, tc.code)
		}
	}
}
