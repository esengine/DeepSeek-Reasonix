// Package cosplay — automatic post-mutation verification (auto_on_mutation).
//
// The code_verify tool is manual: the model must choose to run it. With
// auto_on_mutation enabled, every successful file mutation observed by the
// agent (via the checkpoint mutation observer) asynchronously triggers a
// bounded CoSPlay round on the mutated file, and the result surfaces as a
// Notice event. The loop stays lightweight by design — one generated test,
// one repair round max — so tool-call latency is unaffected; the work runs
// in a detached goroutine with its own timeout and never blocks the run loop.
package cosplay

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// AutoConfig parameterizes the automatic post-mutation verification. Zero
// values fall back to the documented defaults.
type AutoConfig struct {
	// NumTests bounds generated tests per round (default 1 — lightweight).
	NumTests int
	// MaxRounds bounds repair iterations (default 1 — lightweight).
	MaxRounds int
	// Timeout bounds one verification run (default 20s).
	Timeout time.Duration
	// Concurrency caps simultaneous verification processes (default 1).
	Concurrency int
}

func (c AutoConfig) withDefaults() AutoConfig {
	if c.NumTests <= 0 {
		c.NumTests = 1
	}
	if c.MaxRounds <= 0 {
		c.MaxRounds = 1
	}
	if c.Timeout <= 0 {
		c.Timeout = 20 * time.Second
	}
	if c.Concurrency <= 0 {
		c.Concurrency = 1
	}
	return c
}

// supportedLanguages are the languages the offline TemplateGenerator can
// actually execute. Files outside this set are skipped (no false "verify"
// events for unknown toolchains).
var supportedLanguages = map[string]bool{
	"go":         true,
	"python":     true,
	"javascript": true,
}

// LanguageFromPath infers the co-evolution language from a file path's
// extension. Returns "" for unsupported extensions.
func LanguageFromPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go"
	case ".py", ".pyw":
		return "python"
	case ".js", ".mjs", ".cjs":
		return "javascript"
	default:
		return ""
	}
}

// VerifyFile runs one bounded CoSPlay round against the file at path and
// returns a short human-readable summary. It returns "" when the language is
// unsupported or the file cannot be read — the caller treats that as "skip".
func VerifyFile(ctx context.Context, path string, cfg AutoConfig) string {
	lang := LanguageFromPath(path)
	if lang == "" {
		return ""
	}
	code, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	cfg = cfg.withDefaults()
	v := NewVerifier(TemplateGenerator{}, &ProcessRunner{}, nil)
	v.NumTests = cfg.NumTests
	v.MaxRounds = cfg.MaxRounds
	rep, err := v.Verify(ctx, Task{
		Description: "verify the mutated code in " + filepath.Base(path),
		Language:    lang,
	}, Candidate{ID: "mutated", Code: string(code), Language: lang})
	if err != nil {
		return "code_verify auto: " + err.Error()
	}
	return "code_verify auto [" + filepath.Base(path) + "]: " + formatReport(rep)
}

// AutoVerifier asynchronously verifies mutated files and reports results via
// a callback. It is safe for concurrent use (the checkpoint mutation observer
// may fire from any hook context).
type AutoVerifier struct {
	cfg AutoConfig
	sem chan struct{}
}

// NewAutoVerifier builds an AutoVerifier. A nil config means defaults.
func NewAutoVerifier(cfg AutoConfig) *AutoVerifier {
	cfg = cfg.withDefaults()
	return &AutoVerifier{cfg: cfg, sem: make(chan struct{}, cfg.Concurrency)}
}

// MaybeVerify starts an asynchronous verification of path (if supported) and
// invokes onResult with the summary line when done. If the language is
// unsupported or the file is unreadable, onResult is not called. It never
// blocks the caller.
func (v *AutoVerifier) MaybeVerify(ctx context.Context, path string, onResult func(summary string)) {
	if v == nil || path == "" || LanguageFromPath(path) == "" || onResult == nil {
		return
	}
	select {
	case v.sem <- struct{}{}:
	default:
		// Concurrency budget exhausted — skip rather than queue unboundedly.
		return
	}
	go func() {
		defer func() { <-v.sem }()
		rctx, cancel := context.WithTimeout(ctx, v.cfg.Timeout)
		defer cancel()
		summary := VerifyFile(rctx, path, v.cfg)
		if summary != "" {
			onResult(summary)
		}
	}()
}

// HasWork reports whether path is a supported language that would actually
// trigger verification (used for diagnostics).
func (v *AutoVerifier) HasWork(path string) bool {
	return path != "" && LanguageFromPath(path) != ""
}
