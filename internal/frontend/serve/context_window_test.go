package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

func declareWindow(t *testing.T, base, body string) *http.Response {
	t.Helper()
	return postProvider(t, base, "/context/window", body)
}

// The window is a fact about one model, not about the endpoint serving it: a
// relay forwards several, and writing the number endpoint-wide would answer for
// models it was never typed against. The rebuilt gauge rides the same response
// because nothing else would tell the panel the denominator had arrived.
func TestSetContextWindowDeclaresItAgainstTheModelInUse(t *testing.T) {
	srv := newRichProviderServer(t)

	resp := declareWindow(t, srv.URL, `{"window":200000}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := readAllString(resp)
		t.Fatalf("POST /context/window = %d: %s", resp.StatusCode, b)
	}
	var got struct {
		Window int `json:"window"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Window != 200000 {
		t.Fatalf("the rebuilt gauge reports window %d, want the declared 200000", got.Window)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := cfg.Provider("rich")
	if !ok {
		t.Fatal("the provider disappeared")
	}
	if got := entry.ModelOverrides["alpha"].ContextWindow; got != 200000 {
		t.Fatalf("alpha's window = %d, want 200000", got)
	}
	if entry.ContextWindow != 131072 {
		t.Fatalf("the endpoint-wide window was overwritten: %d", entry.ContextWindow)
	}
	if got := entry.ModelOverrides["beta"]; got.ContextWindow != 0 || len(got.SupportedEfforts) != 2 {
		t.Fatalf("the sibling model's override was disturbed: %+v", got)
	}
}

// Zero is the stored "inherit", so clearing the field falls back to whatever the
// entry or the catalogue says rather than to no window at all.
func TestSetContextWindowZeroFallsBackToTheEndpoint(t *testing.T) {
	srv := newRichProviderServer(t)

	resp := declareWindow(t, srv.URL, `{"window":200000}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("first declaration = %d", resp.StatusCode)
	}
	cleared := declareWindow(t, srv.URL, `{"window":0}`)
	defer cleared.Body.Close()
	if cleared.StatusCode != http.StatusOK {
		b, _ := readAllString(cleared)
		t.Fatalf("clearing = %d: %s", cleared.StatusCode, b)
	}
	var got struct {
		Window int `json:"window"`
	}
	if err := json.NewDecoder(cleared.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Window != 131072 {
		t.Fatalf("window after clearing = %d, want the endpoint's 131072", got.Window)
	}
}

// A negative window is the one answer that is not a setting, and it must not
// reach the config: the agent reads any positive number as a ceiling and zero
// as "nobody said", leaving nothing for a negative one to mean.
func TestSetContextWindowRefusesANegativeOne(t *testing.T) {
	srv := newRichProviderServer(t)

	resp := declareWindow(t, srv.URL, `{"window":-1}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("POST /context/window with a negative window = %d, want 400", resp.StatusCode)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	entry, _ := cfg.Provider("rich")
	if got := entry.ModelOverrides["alpha"].ContextWindow; got != 0 {
		t.Fatalf("the refused window was written anyway: %d", got)
	}
}

// midTurn answers the one question the rebuild asks before it starts. A turn
// running is the ordinary state of this panel — it is what fills the gauge the
// reader is looking at — so the mechanism is what needs pinning, not whether
// production reaches it.
type midTurn struct{ control.SessionAPI }

func (midTurn) RuntimeStatus() control.RuntimeStatus { return control.RuntimeStatus{Running: true} }

// The write lands and only the rebuild is refused, so this is not a failed
// save: it is a window that starts counting a turn later. Reporting it as
// runtime.rebuild_failed would send the reader looking for a malfunction, and
// as busy.switch_model would tell them to stop and switch models — neither is
// what happened.
func TestSetContextWindowMidTurnSaysItAppliesAfterTheTurn(t *testing.T) {
	srv := newRichProviderServerAs(t, func(c control.SessionAPI) control.SessionAPI { return midTurn{c} })

	resp := declareWindow(t, srv.URL, `{"window":200000}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		b, _ := readAllString(resp)
		t.Fatalf("POST /context/window mid-turn = %d, want 409: %s", resp.StatusCode, b)
	}
	var got struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Code != "context.window_after_this_turn" {
		t.Fatalf("code = %q, want context.window_after_this_turn", got.Code)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	entry, _ := cfg.Provider("rich")
	if got := entry.ModelOverrides["alpha"].ContextWindow; got != 200000 {
		t.Fatalf("alpha's window = %d — the refusal said it was saved, so it has to be", got)
	}
}

// Declaring a window writes the config file and rebuilds the runtime, so it
// rides the same grant as every other route that edits a source.
func TestSetContextWindowRefusedWithoutGrant(t *testing.T) {
	srv := httptest.NewServer(operatorHandler(newProviderEditServer(t)))
	defer srv.Close()

	resp := declareWindow(t, srv.URL, `{"window":200000}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("POST /context/window without the grant = %d, want 403", resp.StatusCode)
	}
}
