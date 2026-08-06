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

// TestLookupLegacyKeyringBatchDoesNotClobberUserWrite forces the production
// probe→store window: the batch has already decided the key needs import
// (probe returns found), then the user writes a new value before store-if-absent.
func TestLookupLegacyKeyringBatchDoesNotClobberUserWrite(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")

	oldLookup := legacyKeyringProbeLookup
	t.Cleanup(func() { legacyKeyringProbeLookup = oldLookup })

	probeReached := make(chan struct{})
	releaseProbe := make(chan struct{})
	var once sync.Once
	legacyKeyringProbeLookup = func(ctx context.Context, key string) legacyKeyringOutcome {
		if key != "DEEPSEEK_API_KEY" {
			return legacyKeyringOutcome{Status: legacyKeyringAbsent}
		}
		once.Do(func() { close(probeReached) })
		select {
		case <-releaseProbe:
		case <-ctx.Done():
			return legacyKeyringOutcome{Status: legacyKeyringTimeout}
		}
		return legacyKeyringOutcome{Status: legacyKeyringFound, Value: "sk-old-keyring"}
	}

	done := make(chan map[string]legacyKeyringOutcome, 1)
	go func() {
		done <- lookupLegacyKeyringBatch([]string{"DEEPSEEK_API_KEY"}, time.Second)
	}()

	select {
	case <-probeReached:
	case <-time.After(2 * time.Second):
		t.Fatal("probe did not reach the interleaving checkpoint")
	}
	if _, err := SetCredential("DEEPSEEK_API_KEY", "sk-user-new"); err != nil {
		t.Fatal(err)
	}
	close(releaseProbe)

	got := <-done
	if got["DEEPSEEK_API_KEY"].Status != legacyKeyringFound {
		t.Fatalf("batch status = %+v, want found (store skipped)", got["DEEPSEEK_API_KEY"])
	}
	val, ok := envFileValue(UserCredentialsPath(), "DEEPSEEK_API_KEY")
	if !ok || val != "sk-user-new" {
		t.Fatalf("credential = (%q, %v), want sk-user-new (keyring must not clobber)", val, ok)
	}
}

// TestLookupLegacyKeyringBatchDoesNotReviveTombstone forces the same production
// probe→store window against a cleared tombstone written after the probe starts.
func TestLookupLegacyKeyringBatchDoesNotReviveTombstone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")

	// Start empty: no value and no tombstone so the batch will probe.
	oldLookup := legacyKeyringProbeLookup
	t.Cleanup(func() { legacyKeyringProbeLookup = oldLookup })

	probeReached := make(chan struct{})
	releaseProbe := make(chan struct{})
	var once sync.Once
	legacyKeyringProbeLookup = func(ctx context.Context, key string) legacyKeyringOutcome {
		if key != "DEEPSEEK_API_KEY" {
			return legacyKeyringOutcome{Status: legacyKeyringAbsent}
		}
		once.Do(func() { close(probeReached) })
		select {
		case <-releaseProbe:
		case <-ctx.Done():
			return legacyKeyringOutcome{Status: legacyKeyringTimeout}
		}
		return legacyKeyringOutcome{Status: legacyKeyringFound, Value: "sk-old-keyring"}
	}

	done := make(chan map[string]legacyKeyringOutcome, 1)
	go func() {
		done <- lookupLegacyKeyringBatch([]string{"DEEPSEEK_API_KEY"}, time.Second)
	}()

	select {
	case <-probeReached:
	case <-time.After(2 * time.Second):
		t.Fatal("probe did not reach the interleaving checkpoint")
	}
	// Write then clear while probe is blocked: tombstone must win.
	if _, err := SetCredential("DEEPSEEK_API_KEY", "sk-temp"); err != nil {
		t.Fatal(err)
	}
	if err := RemoveCredential("DEEPSEEK_API_KEY"); err != nil {
		t.Fatal(err)
	}
	if !credentialCurrentStoreClearedKey("DEEPSEEK_API_KEY") {
		t.Fatal("expected cleared tombstone before releasing probe")
	}
	close(releaseProbe)

	got := <-done
	if got["DEEPSEEK_API_KEY"].Status != legacyKeyringFound {
		// store-if-absent skipped → still reported found (already handled)
		t.Fatalf("batch status = %+v", got["DEEPSEEK_API_KEY"])
	}
	if credentialCurrentStoreHasKey("DEEPSEEK_API_KEY") {
		t.Fatal("keyring import revived a tombstoned credential")
	}
	if !credentialCurrentStoreClearedKey("DEEPSEEK_API_KEY") {
		t.Fatal("tombstone missing after batch")
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
