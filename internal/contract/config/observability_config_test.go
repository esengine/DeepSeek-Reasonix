package config

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestOTLPExportIsOffUnlessTheUserFileSwitchesItOnAndNamesACollector(t *testing.T) {
	if Default().OTLPActive() {
		t.Fatal("trace export is on by default")
	}
	cfg, _ := loadScoped(t, "[observability.otlp]\nenabled = true\n", "")
	if cfg.OTLPActive() {
		t.Fatal("switched on with no collector is active")
	}
	cfg, _ = loadScoped(t, "[observability.otlp]\nendpoint = \"http://localhost:4318\"\n", "")
	if cfg.OTLPActive() {
		t.Fatal("an endpoint alone switched the export on")
	}
	cfg, _ = loadScoped(t, "[observability.otlp]\nenabled = true\nendpoint = \"http://localhost:4318\"\n", "")
	if !cfg.OTLPActive() {
		t.Fatal("the user's own switch and endpoint were not honoured")
	}
}

// A cloned repository must not be able to point a session's traces anywhere,
// nor undo the user's own choice.
func TestAProjectFileCannotTurnOnOrRedirectTraceExport(t *testing.T) {
	cfg, _ := loadScoped(t, "", "[observability.otlp]\nenabled = true\nendpoint = \"https://collector.attacker.example\"\n")
	if cfg.OTLPActive() || cfg.Observability.OTLP.Endpoint != "" {
		t.Fatalf("the project file set the export: %+v", cfg.Observability)
	}
	if !slices.Contains(ignoredKeys(cfg), "observability.otlp") {
		t.Fatalf("ignored = %v, want observability.otlp reported", ignoredKeys(cfg))
	}
	cfg, _ = loadScoped(t, "[observability.otlp]\nenabled = true\nendpoint = \"http://localhost:4318\"\n", "[observability.otlp]\nenabled = false\nendpoint = \"https://elsewhere.example\"\n")
	if !cfg.OTLPActive() || cfg.Observability.OTLP.Endpoint != "http://localhost:4318" {
		t.Fatalf("the project file overrode the user's choice: %+v", cfg.Observability)
	}
}

func TestSavingTheUserFileKeepsTheTraceExportSettings(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	path := UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	body := "[observability.otlp]\nenabled = true\nendpoint = \"https://otel.example\"\nheader_name = \"Authorization\"\nheader_env = \"OTEL_AUTH\"\nservice_name = \"me\"\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := LoadForEdit(path)
	cfg.Agent.MaxSideSessions = 2
	if err := cfg.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	if got := LoadForEdit(path).Observability.OTLP; got != (OTLPConfig{Enabled: true, Endpoint: "https://otel.example", HeaderName: "Authorization", HeaderEnv: "OTEL_AUTH", ServiceName: "me"}) {
		t.Fatalf("after an unrelated save = %+v", got)
	}
}

// The credential is read from a variable made for it; no setting can point the
// export at an unrelated secret.
func TestTheCredentialVariableMustBeOneMadeForTheExport(t *testing.T) {
	for _, ok := range []string{"", "OTEL_EXPORTER_OTLP_HEADERS", "REASONIX_OTEL_TOKEN"} {
		if !OTLPHeaderEnvOK(ok) {
			t.Errorf("%q refused", ok)
		}
	}
	for _, bad := range []string{"AWS_SECRET_ACCESS_KEY", "HOME", "OTEL", "otel_token", "OTEL_X Y", "REASONIX_API_KEY", "OTEL_" + string(make([]byte, 70))} {
		if OTLPHeaderEnvOK(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
	var cfg Config
	if err := cfg.SetOTLPHeaderEnv("AWS_SECRET_ACCESS_KEY"); !errors.Is(err, ErrOTLPHeaderEnvInvalid) {
		t.Fatalf("setter err = %v", err)
	}
}

func TestEndpointSettersRefuseWhatCouldCarryASecretOrNotBeACollector(t *testing.T) {
	var cfg Config
	for _, bad := range []string{"collector:4318", "ftp://x", "https://u:pw@x", "https://x/?k=1", "https://x/#f"} {
		if err := cfg.SetOTLPEndpoint(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if err := cfg.SetOTLPEndpoint("http://localhost:4318"); err != nil || cfg.Observability.OTLP.Endpoint != "http://localhost:4318" {
		t.Fatalf("a plain collector refused: %v", err)
	}
	if err := cfg.SetOTLPEndpoint(""); err != nil || cfg.Observability.OTLP.Endpoint != "" {
		t.Fatalf("clearing refused: %v", err)
	}
	if err := cfg.SetOTLPHeaderName("Host"); !errors.Is(err, ErrOTLPHeaderInvalid) {
		t.Fatalf("a reserved header name accepted: %v", err)
	}
}
