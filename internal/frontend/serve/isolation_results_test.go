package serve

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/isolation"
	"reasonix/internal/session/control"
)

type isolationCalls struct {
	control.SessionAPI
	preview, discard, recover, root int
	request                         control.IsolatedDiscardRequest
	discardError                    error
}

func (*isolationCalls) IsolationResults(context.Context) (isolation.Catalog, error) {
	return isolation.Catalog{Enabled: true, Results: []isolation.ResultSummary{}, Diagnostics: []isolation.CatalogDiagnostic{}}, nil
}
func (s *isolationCalls) PreviewIsolatedDiscard(_ context.Context, id string) (isolation.DiscardPreview, error) {
	s.preview++
	return isolation.DiscardPreview{ID: id, Revision: strings.Repeat("a", 64), Entries: 4, CandidateRoot: "/fixture/candidate", State: isolation.Pending}, nil
}
func (s *isolationCalls) DiscardIsolated(_ context.Context, request control.IsolatedDiscardRequest) error {
	s.discard++
	s.request = request
	return s.discardError
}
func (s *isolationCalls) RecoverIsolatedIntegration(context.Context, isolation.RecoveryRequest) error {
	s.recover++
	return nil
}
func (s *isolationCalls) ManageRootScene(context.Context, isolation.RootSceneRequest) error {
	s.root++
	return nil
}

func isolationRoute(s *isolationCalls, method, path, body string, device bool) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	(&Server{ctrl: s}).registerIsolationResultRoutes(mux)
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if device {
		r = r.WithContext(withDeviceReach(r.Context(), "fixture-device", 1))
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func TestIsolationManagementRoutesPreserveRevisionAndRefusals(t *testing.T) {
	s := &isolationCalls{}
	w := isolationRoute(s, http.MethodGet, "/isolated-results", "", false)
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), ":null") {
		t.Fatalf("empty catalog wire shape: %d %s", w.Code, w.Body.String())
	}
	w = isolationRoute(s, http.MethodPost, "/isolated-results/discard-preview", `{"id":"iso_12345678"}`, false)
	if w.Code != http.StatusOK || s.preview != 1 || s.discard != 0 {
		t.Fatalf("preview performed a deletion: %d %+v", w.Code, s)
	}
	var preview isolation.DiscardPreview
	if err := json.Unmarshal(w.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	s.discardError = tool.Refusal{Code: isolation.CodeStaleDiscard, Message: "candidate changed"}
	body, _ := json.Marshal(control.IsolatedDiscardRequest{ID: preview.ID, Revision: preview.Revision})
	w = isolationRoute(s, http.MethodPost, "/isolated-results/discard", string(body), false)
	var refusal Reason
	if err := json.Unmarshal(w.Body.Bytes(), &refusal); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusConflict || refusal.Code != isolation.CodeStaleDiscard || s.request.Revision != preview.Revision {
		t.Fatalf("discard lost revision or structured conflict: %d %+v %+v", w.Code, refusal, s.request)
	}
}

func TestPairedDevicesCannotMutateRetainedResults(t *testing.T) {
	for _, path := range []string{"/isolated-results/discard-preview", "/isolated-results/discard", "/isolated-results/recover", "/isolated-results/command", "/isolated-results/root"} {
		s := &isolationCalls{}
		w := isolationRoute(s, http.MethodPost, path, `{}`, true)
		if w.Code != http.StatusNotFound || s.preview+s.discard+s.recover+s.root != 0 {
			t.Fatalf("device reached host result operation %s: %d %+v", path, w.Code, s)
		}
	}
	s := &isolationCalls{}
	w := isolationRoute(s, http.MethodPost, "/isolated-results/root", `{}`, false)
	if w.Code != http.StatusNoContent || s.root != 1 {
		t.Fatalf("host root management blocked: %d %+v", w.Code, s)
	}
}

func TestIsolationRefusalStatusFollowsItsClass(t *testing.T) {
	for code, status := range map[string]int{
		isolation.CodeUnknownID:        http.StatusNotFound,
		isolation.CodeUnavailable:      http.StatusNotImplemented,
		isolation.CodeNotGit:           http.StatusNotImplemented,
		isolation.CodeUnsupportedState: http.StatusNotImplemented,
		isolation.CodeStaleDiscard:     http.StatusConflict,
		isolation.CodeBusy:             http.StatusConflict,
	} {
		w := httptest.NewRecorder()
		writeIsolationError(w, tool.Refusal{Code: code, Message: code})
		if w.Code != status {
			t.Errorf("%s answered %d, want %d", code, w.Code, status)
		}
	}
}

func TestUnclassifiedIsolationFailureIsNotAConflict(t *testing.T) {
	w := httptest.NewRecorder()
	writeIsolationError(w, errors.New("disk full while staging the root environment"))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("an internal failure answered %d", w.Code)
	}
}
