package config

import (
	"testing"
	"time"
)

func TestLegacyKeyringCredentialValueWithTimeout(t *testing.T) {
	oldImpl := legacyKeyringCredentialValueImpl
	oldTimeout := legacyKeyringLookupTimeout
	t.Cleanup(func() {
		legacyKeyringCredentialValueImpl = oldImpl
		legacyKeyringLookupTimeout = oldTimeout
	})

	t.Run("returns value", func(t *testing.T) {
		legacyKeyringCredentialValueImpl = func(key string) (string, bool) {
			return "secret-" + key, true
		}
		value, ok := legacyKeyringCredentialValueWithTimeout("DEEPSEEK_API_KEY")
		if !ok || value != "secret-DEEPSEEK_API_KEY" {
			t.Fatalf("got (%q, %v), want (secret-DEEPSEEK_API_KEY, true)", value, ok)
		}
	})

	t.Run("times out on stuck keyring", func(t *testing.T) {
		legacyKeyringLookupTimeout = 40 * time.Millisecond
		started := make(chan struct{})
		legacyKeyringCredentialValueImpl = func(string) (string, bool) {
			close(started)
			time.Sleep(500 * time.Millisecond)
			return "late", true
		}
		start := time.Now()
		value, ok := legacyKeyringCredentialValueWithTimeout("DEEPSEEK_API_KEY")
		elapsed := time.Since(start)
		if ok || value != "" {
			t.Fatalf("got (%q, %v), want fail-closed timeout", value, ok)
		}
		if elapsed < 40*time.Millisecond {
			t.Fatalf("elapsed %v, want at least the timeout", elapsed)
		}
		if elapsed > 250*time.Millisecond {
			t.Fatalf("elapsed %v, want fail-fast near the timeout", elapsed)
		}
		select {
		case <-started:
		case <-time.After(100 * time.Millisecond):
			t.Fatal("stuck keyring probe was never started")
		}
	})
}

func TestLookupLegacyKeyringBatchSharedBudget(t *testing.T) {
	oldImpl := legacyKeyringCredentialValueImpl
	t.Cleanup(func() { legacyKeyringCredentialValueImpl = oldImpl })

	legacyKeyringCredentialValueImpl = func(key string) (string, bool) {
		if key == "FAST" {
			return "ok", true
		}
		time.Sleep(200 * time.Millisecond)
		return "slow", true
	}
	// Drive the batch through the package lookup hook.
	oldLookup := legacyKeyringCredentialValueLookup
	legacyKeyringCredentialValueLookup = legacyKeyringCredentialValueImpl
	t.Cleanup(func() { legacyKeyringCredentialValueLookup = oldLookup })

	values, completed, timedOut := lookupLegacyKeyringBatch([]string{"FAST", "SLOW"}, 50*time.Millisecond)
	if !timedOut {
		t.Fatal("expected shared budget timeout")
	}
	if !completed["FAST"] || values["FAST"] != "ok" {
		t.Fatalf("partial FAST = (%q, completed=%v), want ok", values["FAST"], completed["FAST"])
	}
	if completed["SLOW"] {
		t.Fatal("SLOW should not complete within the shared budget")
	}
}
