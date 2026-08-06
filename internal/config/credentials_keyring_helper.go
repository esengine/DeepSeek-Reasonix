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
// Only StatusAbsent may write a migration-done marker.
type legacyKeyringStatus string

const (
	legacyKeyringFound   legacyKeyringStatus = "found"
	legacyKeyringAbsent  legacyKeyringStatus = "absent"
	legacyKeyringError   legacyKeyringStatus = "error"
	legacyKeyringTimeout legacyKeyringStatus = "timeout"
)

// legacyKeyringOutcome is the four-state result for one env key.
type legacyKeyringOutcome struct {
	Status legacyKeyringStatus
	Value  string
}

type legacyKeyringHelperReport struct {
	Results []legacyKeyringHelperResult `json:"results"`
}

type legacyKeyringHelperResult struct {
	Key    string `json:"key"`
	Status string `json:"status"`
	Value  string `json:"value,omitempty"`
}

// legacyKeyringHelperEnabled controls whether batch probes spawn an isolated
// child. Tests disable it and drive legacyKeyringCredentialValueLookup instead.
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
		value, ok := legacyKeyringCredentialValueImpl(key)
		res := legacyKeyringHelperResult{Key: key, Status: string(legacyKeyringAbsent)}
		if ok {
			if strings.TrimSpace(value) != "" {
				res.Status = string(legacyKeyringFound)
				res.Value = value
			} else {
				// Explicit empty secret is still a successful lookup with no usable value.
				res.Status = string(legacyKeyringAbsent)
			}
		}
		// Platform impl collapses transport errors into !ok; treat as absent so
		// a healthy empty keyring can mark done. Process-level failures are
		// reported by the parent when the helper exits non-zero or times out.
		report.Results = append(report.Results, res)
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
// the helper and resolve via legacyKeyringCredentialValueLookup in-process.
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
		value, ok := legacyKeyringCredentialValueLookup(key)
		if ok && strings.TrimSpace(value) != "" {
			out[key] = legacyKeyringOutcome{Status: legacyKeyringFound, Value: value}
			continue
		}
		out[key] = legacyKeyringOutcome{Status: legacyKeyringAbsent}
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
		// Kill may race with natural exit; leave timeout status for unfinished keys.
		if stdout.Len() == 0 {
			return out
		}
		// Partial stdout is unusual but parse what we got; unparsed stay timeout.
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
			if strings.TrimSpace(res.Value) == "" {
				out[key] = legacyKeyringOutcome{Status: legacyKeyringAbsent}
			} else {
				out[key] = legacyKeyringOutcome{Status: legacyKeyringFound, Value: res.Value}
			}
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
	// Prevent the child from inheriting a half-built terminal state.
	cmd.Stdin = nil
	return cmd, nil
}

// helperProcessArgs avoids re-entering the full test suite when os.Executable
// is a `go test` binary. Production reasonix has no -test.* flags.
func helperProcessArgs() []string {
	name := filepath.Base(os.Args[0])
	if strings.HasSuffix(name, ".test") || strings.HasSuffix(name, ".test.exe") ||
		strings.Contains(os.Args[0], string(filepath.Separator)+"_test"+string(filepath.Separator)) {
		// Match no tests; TestMain still runs and dispatches the helper env.
		return []string{"-test.run=^$", "-test.v=false"}
	}
	return nil
}
