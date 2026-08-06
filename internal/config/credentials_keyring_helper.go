package config

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	legacyKeyringHelperEnv  = "REASONIX_LEGACY_KEYRING_HELPER"
	legacyKeyringKeysEnv    = "REASONIX_LEGACY_KEYRING_KEYS"
	legacyKeyringStallEnv   = "REASONIX_LEGACY_KEYRING_HELPER_STALL" // test-only sleep before probes
	legacyKeyringHelperFlag = "1"
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
// Value is only populated inside the helper (or in-process tests) before
// secure storage; it is never returned to the parent process over stdout.
type legacyKeyringOutcome struct {
	Status legacyKeyringStatus
	Value  string
}

type legacyKeyringHelperReport struct {
	Results []legacyKeyringHelperResult `json:"results"`
}

// legacyKeyringHelperResult is parent-visible metadata only — no secret fields.
type legacyKeyringHelperResult struct {
	Key    string `json:"key"`
	Status string `json:"status"`
}

// legacyKeyringHelperEnabled controls whether batch probes spawn an isolated
// child. Tests disable it and drive legacyKeyringProbeLookup instead.
var legacyKeyringHelperEnabled = true

// MaybeRunLegacyKeyringHelper reports whether this process is the isolated
// keyring helper. When handled is true, the caller must exit with code.
func MaybeRunLegacyKeyringHelper() (code int, handled bool) {
	if os.Getenv(legacyKeyringHelperEnv) != legacyKeyringHelperFlag {
		return 0, false
	}
	return runLegacyKeyringHelper(), true
}

func runLegacyKeyringHelper() int {
	// Optional stall lets unit tests prove Kill+Wait without a real stuck bus.
	if raw := strings.TrimSpace(os.Getenv(legacyKeyringStallEnv)); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil && d > 0 {
			time.Sleep(d)
		}
	}
	keys := splitLegacyKeyringKeys(os.Getenv(legacyKeyringKeysEnv))
	report := legacyKeyringHelperReport{Results: make([]legacyKeyringHelperResult, 0, len(keys))}
	for _, key := range keys {
		o := legacyKeyringProbeImpl(key)
		status := o.Status
		// Store found secrets inside the helper so stdout never carries plaintext
		// credentials. A store failure is reported as error (no marker).
		if status == legacyKeyringFound {
			if strings.TrimSpace(o.Value) == "" {
				status = legacyKeyringAbsent
			} else if _, err := StoreCredentialLines([]string{key + "=" + o.Value}); err != nil {
				status = legacyKeyringError
			}
		}
		// Scrub before any further handling; never encode Value.
		o.Value = ""
		report.Results = append(report.Results, legacyKeyringHelperResult{
			Key:    key,
			Status: string(status),
		})
	}
	if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
		fmt.Fprintln(os.Stderr, "legacy keyring helper encode:", err)
		return 2
	}
	return 0
}

func splitLegacyKeyringKeys(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, "\n")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// lookupLegacyKeyringBatch probes keys under a shared deadline.
// Production uses one helper subprocess (Kill+Wait on timeout). Tests disable
// the helper and resolve via legacyKeyringProbeLookup in-process.
// Returned outcomes never include secret Values for the helper path; in-process
// tests store found secrets before scrubbing Value.
func lookupLegacyKeyringBatch(keys []string, budget time.Duration) map[string]legacyKeyringOutcome {
	out := make(map[string]legacyKeyringOutcome, len(keys))
	if len(keys) == 0 {
		return out
	}
	if budget <= 0 {
		budget = time.Second
	}
	if !legacyKeyringHelperEnabled {
		return lookupLegacyKeyringBatchInProcess(keys)
	}
	return lookupLegacyKeyringBatchViaHelper(keys, budget)
}

func lookupLegacyKeyringBatchInProcess(keys []string) map[string]legacyKeyringOutcome {
	out := make(map[string]legacyKeyringOutcome, len(keys))
	for _, key := range keys {
		o := legacyKeyringProbeLookup(key)
		if o.Status == legacyKeyringFound {
			if strings.TrimSpace(o.Value) == "" {
				o = legacyKeyringOutcome{Status: legacyKeyringAbsent}
			} else if _, err := StoreCredentialLines([]string{key + "=" + o.Value}); err != nil {
				o = legacyKeyringOutcome{Status: legacyKeyringError}
			} else {
				o.Value = "" // never leave secrets in the returned map
			}
		}
		out[key] = o
	}
	return out
}

func lookupLegacyKeyringBatchViaHelper(keys []string, budget time.Duration) map[string]legacyKeyringOutcome {
	out := make(map[string]legacyKeyringOutcome, len(keys))
	for _, key := range keys {
		out[key] = legacyKeyringOutcome{Status: legacyKeyringTimeout}
	}

	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()

	cmd, err := legacyKeyringHelperCommand(ctx, keys)
	if err != nil {
		for _, key := range keys {
			out[key] = legacyKeyringOutcome{Status: legacyKeyringError}
		}
		return out
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	// Always reap: context timeout kills the process; Wait is inside Run.
	if ctx.Err() == context.DeadlineExceeded {
		if stdout.Len() == 0 {
			return out
		}
		// Partial metadata is rare; parse what arrived, leave others as timeout.
	} else if runErr != nil {
		for _, key := range keys {
			out[key] = legacyKeyringOutcome{Status: legacyKeyringError}
		}
		return out
	}

	var report legacyKeyringHelperReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return out
		}
		for _, key := range keys {
			out[key] = legacyKeyringOutcome{Status: legacyKeyringError}
		}
		return out
	}
	for _, res := range report.Results {
		key := strings.TrimSpace(res.Key)
		if key == "" {
			continue
		}
		switch legacyKeyringStatus(res.Status) {
		case legacyKeyringFound:
			// Helper already persisted any secret; parent only sees metadata.
			out[key] = legacyKeyringOutcome{Status: legacyKeyringFound}
		case legacyKeyringAbsent:
			out[key] = legacyKeyringOutcome{Status: legacyKeyringAbsent}
		case legacyKeyringError:
			out[key] = legacyKeyringOutcome{Status: legacyKeyringError}
		default:
			out[key] = legacyKeyringOutcome{Status: legacyKeyringTimeout}
		}
	}
	return out
}

func legacyKeyringHelperCommand(ctx context.Context, keys []string) (*exec.Cmd, error) {
	exe, err := os.Executable()
	if err != nil {
		exe = os.Args[0]
	}
	args := helperProcessArgs()
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Env = append(os.Environ(),
		legacyKeyringHelperEnv+"="+legacyKeyringHelperFlag,
		legacyKeyringKeysEnv+"="+strings.Join(keys, "\n"),
	)
	cmd.Stdin = nil
	return cmd, nil
}

// helperProcessArgs avoids re-entering the full test suite when os.Executable
// is a `go test` binary. Production reasonix has no -test.* flags.
func helperProcessArgs() []string {
	name := filepath.Base(os.Args[0])
	if strings.HasSuffix(name, ".test") || strings.HasSuffix(name, ".test.exe") ||
		strings.Contains(os.Args[0], string(filepath.Separator)+"_test"+string(filepath.Separator)) {
		return []string{"-test.run=^$", "-test.v=false"}
	}
	return nil
}
