package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReasoningByteLimitEnvOverridesFile(t *testing.T) {
	t.Setenv("REASONIX_REASONING_BYTE_LIMIT", "131072")
	cfg := Default()
	applyReasoningByteLimitEnv(cfg)
	if cfg.Agent.ReasoningByteLimit != 131072 {
		t.Fatalf("env override not applied: %d", cfg.Agent.ReasoningByteLimit)
	}
}

// Negative means disabled — the same convention agent.Options.ReasoningByteLimit
// already honors, so the value flows through boot untouched.
func TestReasoningByteLimitEnvNegativeDisables(t *testing.T) {
	t.Setenv("REASONIX_REASONING_BYTE_LIMIT", "-1")
	cfg := Default()
	applyReasoningByteLimitEnv(cfg)
	if cfg.Agent.ReasoningByteLimit != -1 {
		t.Fatalf("negative override not applied: %d", cfg.Agent.ReasoningByteLimit)
	}
}

func TestReasoningByteLimitEnvUnsetKeepsFileValue(t *testing.T) {
	os.Unsetenv("REASONIX_REASONING_BYTE_LIMIT")
	cfg := Default()
	cfg.Agent.ReasoningByteLimit = 4 << 20
	applyReasoningByteLimitEnv(cfg)
	if cfg.Agent.ReasoningByteLimit != 4<<20 {
		t.Fatalf("unset env must keep the file value: %d", cfg.Agent.ReasoningByteLimit)
	}
}

func TestReasoningByteLimitEnvGarbageIgnored(t *testing.T) {
	t.Setenv("REASONIX_REASONING_BYTE_LIMIT", "not-a-number")
	cfg := Default()
	cfg.Agent.ReasoningByteLimit = 999
	applyReasoningByteLimitEnv(cfg)
	if cfg.Agent.ReasoningByteLimit != 999 {
		t.Fatalf("garbage env must not clobber the file value: %d", cfg.Agent.ReasoningByteLimit)
	}
}

// The TOML key must round-trip through the decoder as a recognized agent key.
func TestReasoningByteLimitTOMLKeyDecodes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reasonix.toml")
	if err := os.WriteFile(path, []byte("[agent]\nreasoning_byte_limit = 2097152\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := Default()
	meta, err := mergeFileSnapshot(cfg, path)
	if err != nil {
		t.Fatalf("mergeFileSnapshot: %v", err)
	}
	if !meta.IsDefined("agent", "reasoning_byte_limit") {
		t.Fatal("reasoning_byte_limit not recognized as a defined agent key")
	}
	if cfg.Agent.ReasoningByteLimit != 2097152 {
		t.Fatalf("decoded = %d, want 2097152", cfg.Agent.ReasoningByteLimit)
	}
}
