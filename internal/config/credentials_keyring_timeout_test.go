package config

import (
	"context"
	"encoding/base64"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLookupLegacyKeyringBatchInProcessFourState(t *testing.T) {
	oldLookup := legacyKeyringProbeLookup
	t.Cleanup(func() { legacyKeyringProbeLookup = oldLookup })

	legacyKeyringProbeLookup = func(_ context.Context, key string) legacyKeyringOutcome {
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
		t.Fatal("FOUND secret should have been stored via store-if-absent")
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

	oldLookup := legacyKeyringProbeLookup
	t.Cleanup(func() { legacyKeyringProbeLookup = oldLookup })

	legacyKeyringProbeLookup = func(context.Context, string) legacyKeyringOutcome {
		return legacyKeyringOutcome{Status: legacyKeyringError}
	}
	outcomes := lookupLegacyKeyringBatch([]string{"DEEPSEEK_API_KEY"}, time.Second)
	if outcomes["DEEPSEEK_API_KEY"].Status != legacyKeyringError {
		t.Fatalf("status = %+v", outcomes["DEEPSEEK_API_KEY"])
	}
	if outcomes["DEEPSEEK_API_KEY"].Status == legacyKeyringAbsent {
		_ = markLegacyKeyringMigrationDone("DEEPSEEK_API_KEY")
	}
	if legacyKeyringMigrationDone("DEEPSEEK_API_KEY") {
		t.Fatal("error outcome must not write a migration marker")
	}
}

func TestLookupLegacyKeyringBatchSharedContextTimeout(t *testing.T) {
	oldLookup := legacyKeyringProbeLookup
	oldTimeout := legacyKeyringLookupTimeout
	legacyKeyringLookupTimeout = 40 * time.Millisecond
	t.Cleanup(func() {
		legacyKeyringProbeLookup = oldLookup
		legacyKeyringLookupTimeout = oldTimeout
	})

	legacyKeyringProbeLookup = func(ctx context.Context, key string) legacyKeyringOutcome {
		if key == "FAST" {
			return legacyKeyringOutcome{Status: legacyKeyringAbsent}
		}
		// Block until the shared budget cancels; must not leave a hung goroutine
		// after the batch returns (probe returns when ctx is done).
		<-ctx.Done()
		return legacyKeyringOutcome{Status: legacyKeyringTimeout}
	}

	start := time.Now()
	got := lookupLegacyKeyringBatch([]string{"FAST", "SLOW"}, legacyKeyringLookupTimeout)
	elapsed := time.Since(start)
	if got["FAST"].Status != legacyKeyringAbsent {
		t.Fatalf("FAST = %+v", got["FAST"])
	}
	if got["SLOW"].Status != legacyKeyringTimeout {
		t.Fatalf("SLOW = %+v, want timeout", got["SLOW"])
	}
	if elapsed > 250*time.Millisecond {
		t.Fatalf("elapsed %v, shared budget did not bound the scan", elapsed)
	}
}

func TestStoreCredentialIfAbsentDoesNotClobberNewerValue(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")

	// Force interleaving without sleep:
	// 1) parent path has already decided the key is missing (outside lock)
	// 2) user writes a new value
	// 3) store-if-absent re-checks under lock and skips the stale keyring value
	parentChecked := make(chan struct{})
	userDone := make(chan struct{})
	result := make(chan bool, 1)

	go func() {
		<-parentChecked
		<-userDone
		stored, err := storeCredentialIfAbsentAndNotCleared("DEEPSEEK_API_KEY", "sk-old-keyring")
		if err != nil {
			t.Errorf("store-if-absent: %v", err)
		}
		result <- stored
	}()

	close(parentChecked)
	if _, err := SetCredential("DEEPSEEK_API_KEY", "sk-user-new"); err != nil {
		t.Fatal(err)
	}
	close(userDone)

	if stored := <-result; stored {
		t.Fatal("store-if-absent must not overwrite a concurrent user write")
	}
	if !credentialCurrentStoreHasKey("DEEPSEEK_API_KEY") {
		t.Fatal("user value missing")
	}
	// Ensure the stored value is the user write, not the keyring secret.
	val, ok := envFileValue(UserCredentialsPath(), "DEEPSEEK_API_KEY")
	if !ok || val != "sk-user-new" {
		t.Fatalf("credential = (%q, %v), want sk-user-new", val, ok)
	}
}

func TestStoreCredentialIfAbsentDoesNotClobberTombstone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")

	if _, err := SetCredential("DEEPSEEK_API_KEY", "sk-temp"); err != nil {
		t.Fatal(err)
	}
	if err := RemoveCredential("DEEPSEEK_API_KEY"); err != nil {
		t.Fatal(err)
	}
	if !credentialCurrentStoreClearedKey("DEEPSEEK_API_KEY") {
		t.Fatal("expected cleared tombstone")
	}

	parentChecked := make(chan struct{})
	userDone := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	var stored bool
	var storeErr error
	go func() {
		defer wg.Done()
		<-parentChecked
		<-userDone
		stored, storeErr = storeCredentialIfAbsentAndNotCleared("DEEPSEEK_API_KEY", "sk-old-keyring")
	}()

	close(parentChecked)
	// Tombstone already present; signal and join.
	close(userDone)
	wg.Wait()
	if storeErr != nil {
		t.Fatal(storeErr)
	}
	if stored {
		t.Fatal("store-if-absent must not revive a cleared key from keyring")
	}
	if credentialCurrentStoreHasKey("DEEPSEEK_API_KEY") {
		t.Fatal("tombstone was overwritten with a keyring value")
	}
	if !credentialCurrentStoreClearedKey("DEEPSEEK_API_KEY") {
		t.Fatal("tombstone missing after store-if-absent")
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
	other := legacyKeyringMigrationMarkerPath("A_B_C_")
	if path == other {
		t.Fatalf("marker collision between %q and A_B_C_", key)
	}
}
