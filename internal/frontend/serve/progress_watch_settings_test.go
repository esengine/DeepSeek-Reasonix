package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

func TestSaveProgressWatchPersistsAndRefusesOutOfRange(t *testing.T) {
	s := newProviderEditServer(t)
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	got := readJSON[control.ProgressWatchSettings](t, srv.URL, "/progress-watch")
	if got.Pause || got.Rounds != config.DefaultProgressWatchRounds || got.TokenMultiple != config.DefaultProgressWatchTokenMultiple {
		t.Fatalf("unset section reads %+v, want pause off at the defaults", got)
	}
	bad := postProvider(t, srv.URL, "/progress-watch", `{"pause":true,"rounds":5000,"tokenMultiple":8}`)
	var refusal struct {
		Code string `json:"code"`
	}
	_ = json.NewDecoder(bad.Body).Decode(&refusal)
	bad.Body.Close()
	if bad.StatusCode != http.StatusBadRequest || refusal.Code != "progress_watch.out_of_range" {
		t.Fatalf("out-of-range save = %d %q, want 400 progress_watch.out_of_range", bad.StatusCode, refusal.Code)
	}
	if config.LoadForEdit(config.UserConfigPath()).ProgressWatch.Pause {
		t.Fatal("a refused save still reached the file")
	}
	ok := postProvider(t, srv.URL, "/progress-watch", `{"pause":true,"rounds":6,"tokenMultiple":3}`)
	ok.Body.Close()
	if ok.StatusCode != http.StatusOK {
		t.Fatalf("save = %d", ok.StatusCode)
	}
	saved := config.LoadForEdit(config.UserConfigPath()).ProgressWatch
	if !saved.Pause || saved.Rounds != 6 || saved.TokenMultiple != 3 {
		t.Fatalf("user file holds %+v", saved)
	}
}
