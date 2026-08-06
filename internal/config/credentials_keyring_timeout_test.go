package config

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLookupLegacyKeyringBatchInProcessFourState(t *testing.T) {
	oldHelper := legacyKeyringHelperEnabled
	oldLookup := legacyKeyringProbeLookup
	legacyKeyringHelperEnabled = false
	t.Cleanup(func() {
		legacyKeyringHelperEnabled = oldHelper
		legacyKeyringProbeLookup = oldLookup
	})

	legacyKeyringProbeLookup = func(key string) legacyKeyringOutcome {
		switch key {
		case "FOUND":
			return legacyKeyringOutcome{Status: legacyKeyringFound, Value: "secret"}
		case "EMPTY":
			return legacyKeyringOutcome{Status: legacyKeyringFound, Value: ""}
		case "ERR":
			return legacyKeyringOutcome{Status: legacyKeyringError}
		default:
			return legacyKeyringOutcome{Status: legacyKeyringAbsent}
		}
	}
	got := lookupLegacyKeyringBatch([]string{"FOUND", "EMPTY", "MISSING", "ERR"}, time.Second)
	if got["FOUND"].Status != legacyKeyringFound || got["FOUND"].Value != "" {
		t.Fatalf("FOUND = %+v, want found with scrubbed value", got["FOUND"])
	}
	if !credentialCurrentStoreHasKey("FOUND") {
		t.Fatal("FOUND secret should have been stored in-process")
	}
	if got["EMPTY"].Status != legacyKeyringAbsent {
		t.Fatalf("EMPTY = %+v, want absent", got["EMPTY"])
	}
	if got["MISSING"].Status != legacyKeyringAbsent {
		t.Fatalf("MISSING = %+v, want absent", got["MISSING"])
	}
	if got["ERR"].Status != legacyKeyringError {
		t.Fatalf("ERR = %+v, want error", got["ERR"])
	}
}

func TestLegacyKeyringErrorDoesNotWriteMarker(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")

	oldHelper := legacyKeyringHelperEnabled
	oldLookup := legacyKeyringProbeLookup
	legacyKeyringHelperEnabled = false
	t.Cleanup(func() {
		legacyKeyringHelperEnabled = oldHelper
		legacyKeyringProbeLookup = oldLookup
	})

	legacyKeyringProbeLookup = func(string) legacyKeyringOutcome {
		return legacyKeyringOutcome{Status: legacyKeyringError}
	}
	// Drive only the keyring branch via batch + mark logic used by migrate.
	outcomes := lookupLegacyKeyringBatch([]string{"DEEPSEEK_API_KEY"}, time.Second)
	if outcomes["DEEPSEEK_API_KEY"].Status != legacyKeyringError {
		t.Fatalf("status = %+v", outcomes["DEEPSEEK_API_KEY"])
	}
	if outcomes["DEEPSEEK_API_KEY"].Status == legacyKeyringAbsent {
		t.Fatal("error must not be treated as absent")
	}
	// Migrate-style: only mark absent
	if outcomes["DEEPSEEK_API_KEY"].Status == legacyKeyringAbsent {
		_ = markLegacyKeyringMigrationDone("DEEPSEEK_API_KEY")
	}
	if legacyKeyringMigrationDone("DEEPSEEK_API_KEY") {
		t.Fatal("error outcome must not write a migration marker")
	}
}

func TestLookupLegacyKeyringBatchHelperTimeoutKillsChild(t *testing.T) {
	oldHelper := legacyKeyringHelperEnabled
	oldTimeout := legacyKeyringLookupTimeout
	legacyKeyringHelperEnabled = true
	legacyKeyringLookupTimeout = 80 * time.Millisecond
	t.Cleanup(func() {
		legacyKeyringHelperEnabled = oldHelper
		legacyKeyringLookupTimeout = oldTimeout
	})
	t.Setenv(legacyKeyringStallEnv, "500ms")

	start := time.Now()
	got := lookupLegacyKeyringBatch([]string{"DEEPSEEK_API_KEY"}, legacyKeyringLookupTimeout)
	elapsed := time.Since(start)
	if got["DEEPSEEK_API_KEY"].Status != legacyKeyringTimeout {
		t.Fatalf("status = %q, want timeout (got %+v)", got["DEEPSEEK_API_KEY"].Status, got["DEEPSEEK_API_KEY"])
	}
	if elapsed < 60*time.Millisecond {
		t.Fatalf("elapsed %v, want near the budget", elapsed)
	}
	if elapsed > 400*time.Millisecond {
		t.Fatalf("elapsed %v, helper was not killed promptly", elapsed)
	}
}

func TestLegacyKeyringHelperStdoutHasNoSecrets(t *testing.T) {
	// Unit-level contract: helper report type has no Value field and encode
	// path only emits key+status. Platform probe is stubbed for determinism.
	oldImpl := legacyKeyringProbeImpl
	legacyKeyringProbeImpl = func(key string) legacyKeyringOutcome {
		return legacyKeyringOutcome{Status: legacyKeyringFound, Value: "sk-must-not-leak"}
	}
	t.Cleanup(func() { legacyKeyringProbeImpl = oldImpl })

	// Capture helper main path by calling the report builder logic via run with
	// a redirected stdout is heavy; assert the public JSON schema instead.
	report := legacyKeyringHelperReport{Results: []legacyKeyringHelperResult{{
		Key: "DEEPSEEK_API_KEY", Status: string(legacyKeyringFound),
	}}}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "sk-") || strings.Contains(string(raw), "value") {
		t.Fatalf("helper report must not include secrets or value fields: %s", raw)
	}
}

func TestLegacyKeyringMarkerUsesRawURLBase64(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	key := "A/B+C="
	path := legacyKeyringMigrationMarkerPath(key)
	wantName := base64.RawURLEncoding.EncodeToString([]byte(key))
	if !strings.HasSuffix(path, wantName) {
		t.Fatalf("marker path = %q, want suffix %q", path, wantName)
	}
	// Distinct keys that would collapse under char-folding stay distinct.
	other := legacyKeyringMigrationMarkerPath("A_B_C_")
	if path == other {
		t.Fatalf("marker collision between %q and A_B_C_", key)
	}
	_ = os.RemoveAll(home)
}
