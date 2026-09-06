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

// Every condition the inbox store refuses reaches a frontend as an identity it
// can branch on. Three had no case and were answered as internal faults; five
// more shared one status and were told apart only by reading English, which no
// frontend does. repolint holds this list to the store's exported family; this
// holds each entry to what goes out on the wire.
func TestEveryKnownInboxRefusalCarriesItsOwnCode(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{control.ErrSteerApplied, http.StatusConflict, "steer.already_applied"},
		{sessioninbox.ErrItemTooLarge, http.StatusRequestEntityTooLarge, "inbox.item_too_large"},
		{sessioninbox.ErrCapacityItems, http.StatusConflict, "inbox.capacity_items"},
		{sessioninbox.ErrCapacityBytes, http.StatusConflict, "inbox.capacity_bytes"},
		{sessioninbox.ErrInvalidState, http.StatusConflict, "inbox.invalid_state"},
		{sessioninbox.ErrPaused, http.StatusConflict, "inbox.paused"},
		{sessioninbox.ErrNotFound, http.StatusConflict, "inbox.not_found"},
		{sessioninbox.ErrIdempotencyConflict, http.StatusConflict, "inbox.idempotency_conflict"},
		{sessioninbox.ErrSchemaReadonly, http.StatusConflict, "inbox.schema_readonly"},
		{sessioninbox.ErrClosed, http.StatusConflict, "inbox.closed"},
		{sessioninbox.ErrEmpty, http.StatusBadRequest, "inbox.empty"},
	}

	seen := map[string]error{}
	for _, tc := range cases {
		// Wrapped, because that is how one of these arrives: the store adds
		// which item it was about on the way out, and a switch reading the
		// sentence rather than the sentinel stops matching right there.
		got := refusalOf(t, fmt.Errorf("inbox item q1: %w", tc.err))
		if got.Code != tc.code {
			t.Errorf("%v: code %q, want %q", tc.err, got.Code, tc.code)
		}
		if got.Status != tc.status {
			t.Errorf("%v: status %d, want %d", tc.err, got.Status, tc.status)
		}
		if prev, dup := seen[tc.code]; dup {
			t.Errorf("%q names both %v and %v; two conditions under one code cannot be told apart", tc.code, prev, tc.err)
		}
		seen[tc.code] = tc.err
	}
}

// A condition this package cannot name keeps the diagnostic path. Giving the
// unknown one a code would be the same lie in the other direction: the frontend
// would say a sentence about a state nobody established.
func TestAnUnknownInboxFailureIsNotGivenAnIdentity(t *testing.T) {
	got := refusalOf(t, fmt.Errorf("disk went away"))
	if got.Code != "" {
		t.Errorf("an unrecognised failure was labelled %q", got.Code)
	}
	if got.Status != http.StatusInternalServerError {
		t.Errorf("status %d, want %d", got.Status, http.StatusInternalServerError)
	}
}

type inboxRefusal struct {
	Status int
	Code   string
}

func refusalOf(t *testing.T, err error) inboxRefusal {
	t.Helper()
	rec := httptest.NewRecorder()
	writeInboxError(rec, err)
	var body struct {
		Code string `json:"code"`
	}
	// A coded refusal is JSON; the fallback is plain text, and failing to parse
	// it is the answer rather than a fault of the test.
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return inboxRefusal{Status: rec.Code, Code: body.Code}
}
