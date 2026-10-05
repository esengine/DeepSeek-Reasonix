package hook

import (
	"context"
	"maps"
)

// NoCwdExeSearchEnv is the variable Windows reads through
// NeedCurrentDirectoryForExePath; cmd.exe honours it, so a bare command name
// resolves through PATH alone instead of the working directory first.
const NoCwdExeSearchEnv = "NoDefaultCurrentDirectoryInExePath"

// WithoutCwdExeSearch runs hooks whose working directory holds content the
// user does not trust: the hook still starts there and reads it, but a bare
// `python` or `node` no longer resolves to an executable shipped in it. Set on
// every platform; no POSIX shell reads it.
func WithoutCwdExeSearch(spawner Spawner) Spawner {
	if spawner == nil {
		spawner = DefaultSpawner
	}
	return func(ctx context.Context, in SpawnInput) SpawnResult {
		env := make(map[string]string, len(in.Env)+1)
		maps.Copy(env, in.Env)
		env[NoCwdExeSearchEnv] = "1"
		in.Env = env
		return spawner(ctx, in)
	}
}
