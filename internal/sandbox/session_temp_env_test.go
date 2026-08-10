package sandbox

import (
	"runtime"
	"strings"
	"testing"
)

func TestSessionTempEnvLinuxSandboxedPointsAtVirtualTmp(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux sandbox mapping only")
	}
	env := SessionTempEnv("/private/session-tmp", true)
	for _, kv := range env {
		if !strings.HasSuffix(kv, "=/tmp") {
			t.Fatalf("linux sandboxed env %q should point at /tmp", kv)
		}
	}
}

func TestSessionTempEnvHostPrivate(t *testing.T) {
	dir := "/tmp/reasonix-session-tmp-test"
	env := SessionTempEnv(dir, false)
	if len(env) != 3 {
		t.Fatalf("env count = %d, want 3", len(env))
	}
	m := SessionTempEnvMap(dir, false)
	for _, key := range SessionTempEnvKeys {
		if m[key] != dir {
			t.Fatalf("%s = %q, want %q", key, m[key], dir)
		}
	}
}

func TestSessionTempEnvEmpty(t *testing.T) {
	if SessionTempEnv("", true) != nil {
		t.Fatal("empty session temp must not emit overrides")
	}
	if SessionTempEnvMap("", false) != nil {
		t.Fatal("empty session temp map must be nil")
	}
}
