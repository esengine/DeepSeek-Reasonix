package serve

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/model/providerhealth"
)

func TestProviderHealthAuthorizationAndOptIn(t *testing.T) {
	s := newProviderEditServer(t)
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer private-health-key" {
			t.Error("unexpected credential or billable request")
		}
		fmt.Fprint(w, `{"data":[],"is_available":false,"balance_infos":[{"currency":"CNY","total_balance":"0"}]}`)
	}))
	defer upstream.Close()
	if _, err := config.SetCredential("HEALTH_KEY", "private-health-key"); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf("[[providers]]\nname = 'existing'\nkind = 'openai'\nbase_url = %q\nmodels = ['model-a']\napi_key_env = 'HEALTH_KEY'\nbalance_url = %q\nbalance_format = 'deepseek'\n", upstream.URL, upstream.URL+"/balance")
	if err := os.WriteFile(config.UserConfigPath(), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	t.Chdir(project)
	if err := os.WriteFile(filepath.Join(project, "reasonix.toml"), []byte("[desktop]\nprovider_health_checks = true\nprovider_access = ['project']\n"+strings.ReplaceAll(body, "'existing'", "'project'")), 0600); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()
	call := func(path, body string, status int) []byte {
		t.Helper()
		resp := postProvider(t, srv.URL, path, body)
		defer resp.Body.Close()
		var raw json.RawMessage
		if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != status {
			t.Fatalf("%s: %d %s", path, resp.StatusCode, raw)
		}
		return raw
	}
	call("/providers/health", `{"name":"existing"}`, http.StatusForbidden)
	call("/providers/health/settings", `{"enabled":true}`, http.StatusForbidden)
	s.AllowProviderEdit()
	if got := string(call("/providers/health", `{"name":"existing"}`, http.StatusOK)); !strings.Contains(got, `"disabled"`) || hits.Load() != 0 {
		t.Fatalf("default/project enabled probe: %s", got)
	}
	call("/providers/health/settings", `{"enabled":true}`, http.StatusOK)
	call("/providers/health", `{"name":"project"}`, http.StatusNotFound)
	if hits.Load() != 0 {
		t.Fatal("project provider contacted")
	}
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	call("/providers/health", `{"name":"existing","baseUrl":"https://attacker.invalid"}`, http.StatusBadRequest)
	for range 2 {
		var got providerhealth.Reading
		raw := call("/providers/health", `{"name":"existing"}`, http.StatusOK)
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		if got.Connection != "reachable" || got.Wallet != "insufficient" || bytes.Contains(raw, []byte("private-health-key")) {
			t.Fatalf("health: %+v", got)
		}
	}
	if hits.Load() != 2 || strings.Contains(logs.String(), "private-health-key") {
		t.Fatal("duplicate calls or credential log")
	}
	call("/providers/health/settings", `{"enabled":false}`, http.StatusOK)
	call("/providers/health", `{"name":"existing"}`, http.StatusOK)
	if hits.Load() != 2 {
		t.Fatal("disabled check reused cached authorization")
	}
	cfg, err := config.LoadForEditReadOnlyStrict(config.UserConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Desktop.ProviderHealthChecks = true
	cfg.Desktop.ProviderAccess = []string{}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	call("/providers/health", `{"name":"existing"}`, http.StatusNotFound)
	if hits.Load() != 2 {
		t.Fatal("denied account contacted")
	}
}

func TestHealthSettingsRoundTripAndMalformedConfig(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	if err := s.ctl().SaveProviderHealthSettings(true); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadForEditReadOnlyStrict(config.UserConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Desktop.Theme = "light"
	if err = cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	got, err := s.ctl().ProviderHealthSettings()
	if err != nil || !got.Enabled {
		t.Fatalf("round trip: %+v %v", got, err)
	}
	if err = os.WriteFile(config.UserConfigPath(), []byte("[broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = s.ctl().SaveProviderHealthSettings(false); err == nil {
		t.Fatal("overwrote broken config")
	}
	if _, err = s.ctl().ProviderHealth(t.Context(), "existing"); err == nil {
		t.Fatal("used stale consent after unreadable config")
	}
}
