package serve

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSubagentHandoffHostBoundaryAndStructuredPayload(t *testing.T) {
	for _, device := range []bool{false, true} {
		src := &subagentSource{}
		req := httptest.NewRequest(http.MethodPost, "/subagents/sa_1/handoff", strings.NewReader(`{"text":"check","context":{"summary":"selected","resultRefs":["sa_result"]}}`))
		if device {
			req = req.WithContext(withDeviceReach(req.Context(), "phone", 1))
		}
		rec := httptest.NewRecorder()
		subagentMux(src).ServeHTTP(rec, req)
		if device {
			if rec.Code != http.StatusNotFound || len(src.steered) != 0 {
				t.Fatalf("device reached handoff: %d %+v", rec.Code, src.steered)
			}
		} else if rec.Code != http.StatusNoContent || len(src.handoff) != 1 || src.handoff[0].Summary != "selected" || len(src.handoff[0].ResultRefs) != 1 || src.handoff[0].ResultRefs[0] != "sa_result" {
			t.Fatalf("handoff lost structured context: %d %+v", rec.Code, src.handoff)
		}
	}
}

func TestSubagentHandoffRejectsMissingContextUnknownAuthorityAndTrailingJSON(t *testing.T) {
	for _, body := range []string{`{"text":"check"}`, `{"context":{"summary":"x","parent":"other"}}`, `{"context":{"summary":"x"},"approved":true}`, `{"context":{"summary":"x"}} {}`, `{"context":{"summary":"x"}} garbage`} {
		src := &subagentSource{}
		rec := httptest.NewRecorder()
		subagentMux(src).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/subagents/sa_1/handoff", strings.NewReader(body)))
		if rec.Code != http.StatusBadRequest || len(src.steered) != 0 {
			t.Fatalf("accepted %s: %d", body, rec.Code)
		}
	}
}
