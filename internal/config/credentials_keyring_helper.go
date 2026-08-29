package config

import (
	"context"
	"strings"
	"time"
)

// legacyKeyringStatus classifies one keyring probe outcome for migration.
// Only absent may write a migration-done marker.
type legacyKeyringStatus string

const (
	legacyKeyringFound   legacyKeyringStatus = "found"
	legacyKeyringAbsent  legacyKeyringStatus = "absent"
	legacyKeyringError   legacyKeyringStatus = "error"
	legacyKeyringTimeout legacyKeyringStatus = "timeout"
)

// legacyKeyringOutcome is the four-state result for one env key.
// Value is populated only for found probes inside this process and is scrubbed
// before the outcome is returned to migration callers after store-if-absent.
type legacyKeyringOutcome struct {
	Status legacyKeyringStatus
	Value  string
}

// legacyKeyringGetBounded runs one uncancellable keyring lookup under ctx's
// budget. A buffered channel lets a late result be dropped without blocking
// the probe goroutine; the caller-side wait is bounded even though the OS
// call itself cannot be interrupted. On timeout the probe goroutine may stay
// blocked inside the OS call until the keyring answers (macOS may pop a GUI
// prompt) — that is inherent to an uncancellable API and only leaks a parked
// goroutine that exits on its own once the OS returns. timedOut reports that
// the budget expired before the lookup returned. A result that won the select
// race is returned as-is: err-vs-timeout classification stays in the
// per-platform probe, keeping the pre-existing semantics (a found value is
// not demoted to timeout).
func legacyKeyringGetBounded(ctx context.Context, get func() (string, error)) (value string, err error, timedOut bool) {
	type result struct {
		value string
		err   error
	}
	ch := make(chan result, 1)
	go func() {
		v, e := get()
		ch <- result{value: v, err: e}
	}()
	select {
	case <-ctx.Done():
		return "", nil, true
	case r := <-ch:
		return r.value, r.err, false
	}
}

// lookupLegacyKeyringBatch probes keys under a single shared deadline in-process.
// There is no external helper entrypoint: secrets never leave the process via
// stdout or a caller-controlled REASONIX_HOME dump path.
func lookupLegacyKeyringBatch(keys []string, budget time.Duration) map[string]legacyKeyringOutcome {
	out := make(map[string]legacyKeyringOutcome, len(keys))
	if len(keys) == 0 {
		return out
	}
	if budget <= 0 {
		budget = time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()

	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			out[key] = legacyKeyringOutcome{Status: legacyKeyringTimeout}
			continue
		}
		o := legacyKeyringProbeLookup(ctx, key)
		switch o.Status {
		case legacyKeyringFound:
			if strings.TrimSpace(o.Value) == "" {
				out[key] = legacyKeyringOutcome{Status: legacyKeyringAbsent}
				continue
			}
			stored, err := storeCredentialIfAbsentAndNotCleared(key, o.Value)
			o.Value = "" // scrub before returning
			if err != nil {
				out[key] = legacyKeyringOutcome{Status: legacyKeyringError}
				continue
			}
			if !stored {
				// Current store already has a value or tombstone; do not apply
				// the legacy keyring secret. Report found so migration does not
				// re-probe endlessly without writing an absent marker.
				out[key] = legacyKeyringOutcome{Status: legacyKeyringFound}
				continue
			}
			out[key] = legacyKeyringOutcome{Status: legacyKeyringFound}
		case legacyKeyringAbsent, legacyKeyringError, legacyKeyringTimeout:
			out[key] = legacyKeyringOutcome{Status: o.Status}
		default:
			out[key] = legacyKeyringOutcome{Status: legacyKeyringTimeout}
		}
	}
	return out
}
