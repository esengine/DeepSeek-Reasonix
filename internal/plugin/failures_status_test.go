package plugin

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
)

// The status a failure ended at, carried as the number the host held. Reading
// it back out of Error is what the row above this used to do, over text the
// server itself wrote and this package then sanitised and truncated.
func TestAFailureCarriesTheStatusItEndedAt(t *testing.T) {
	h := &Host{}
	h.RecordFailure(Spec{Name: "remote", Type: "http"},
		fmt.Errorf("plugin %q: initialize: %w", "remote", &httpStatusError{Status: http.StatusUnauthorized, Detail: "go away"}))

	got := h.Failures()
	if len(got) != 1 {
		t.Fatalf("failures = %d, want 1", len(got))
	}
	if got[0].HTTPStatus != http.StatusUnauthorized {
		t.Errorf("HTTPStatus = %d, want 401", got[0].HTTPStatus)
	}
}

// The number and the sentence are independent. One says what the endpoint
// answered; the other is detail, and an external server has a hand in writing
// it — including, if it likes, other numbers.
func TestTheStatusIsNotReadOutOfTheMessage(t *testing.T) {
	h := &Host{}
	h.RecordFailure(Spec{Name: "a", Type: "http"},
		fmt.Errorf("plugin %q: %w", "a", &httpStatusError{Status: http.StatusForbidden, Detail: "401 unauthorized forbidden auth please login"}))
	h.RecordFailure(Spec{Name: "b", Type: "stdio"}, errors.New("401 unauthorized forbidden auth please login"))

	by := map[string]Failure{}
	for _, f := range h.Failures() {
		by[f.Name] = f
	}
	if by["a"].HTTPStatus != http.StatusForbidden {
		t.Errorf("a: HTTPStatus = %d, want 403 — the typed status, not the 401 in the text", by["a"].HTTPStatus)
	}
	// Nothing typed said a status, so there is none. Prose full of the words the
	// old regex looked for buys no classification at all.
	if by["b"].HTTPStatus != 0 {
		t.Errorf("b: HTTPStatus = %d, want 0 for a failure that was not an HTTP one", by["b"].HTTPStatus)
	}
}
