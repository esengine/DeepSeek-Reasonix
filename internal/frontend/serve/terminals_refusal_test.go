package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"reasonix/internal/platform/terminal"
)

func TestAnUnsupportedTerminalIsRefusedWithItsOwnCode(t *testing.T) {
	rec := httptest.NewRecorder()
	refuseTerminal(rec, &terminal.Error{Code: terminal.CodeUnsupported, Detail: "no pseudoconsole"})
	var reason Reason
	if err := json.Unmarshal(rec.Body.Bytes(), &reason); err != nil || rec.Code != http.StatusNotImplemented || reason.Code != "terminal.unsupported" {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}
