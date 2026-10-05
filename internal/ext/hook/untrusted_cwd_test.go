package hook

import (
	"context"
	"testing"
)

func TestWithoutCwdExeSearchKeepsHookEnvAndCwd(t *testing.T) {
	var got SpawnInput
	spawn := WithoutCwdExeSearch(func(_ context.Context, in SpawnInput) SpawnResult {
		got = in
		return SpawnResult{}
	})
	hookEnv := map[string]string{"GUARD_MODE": "strict", NoCwdExeSearchEnv: "0"}
	spawn(context.Background(), SpawnInput{Cwd: "/checkout", Env: hookEnv})

	if got.Cwd != "/checkout" {
		t.Fatalf("cwd = %q, want it unchanged", got.Cwd)
	}
	if got.Env["GUARD_MODE"] != "strict" || got.Env[NoCwdExeSearchEnv] != "1" {
		t.Fatalf("env = %v, want the hook's own env plus %s=1", got.Env, NoCwdExeSearchEnv)
	}
	if hookEnv[NoCwdExeSearchEnv] != "0" {
		t.Fatalf("the hook's configured env map was modified: %v", hookEnv)
	}
}
