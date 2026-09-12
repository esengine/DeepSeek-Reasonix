package sandbox

import "runtime"

// SessionTempEnvKeys are the standard temporary-directory environment variables
// Reasonix overrides for session-private temporary directories.
var SessionTempEnvKeys = []string{
	"TMPDIR", "TMP", "TEMP",
	// Build tools commonly write to host-global caches even when their inputs
	// and outputs stay inside the workspace. Keep those writes session-private
	// so normal builds do not need an approval or broaden the sandbox.
	"XDG_CACHE_HOME", "NPM_CONFIG_CACHE", "npm_config_cache", "GOCACHE",
}

// SessionTempEnv returns KEY=value overrides for the session-private temporary
// directory. When linuxSandboxed is true (Linux bwrap with SessionTemp bound at
// /tmp), the variables point at the virtual /tmp path so scripts using $TMPDIR
// and /tmp agree. Otherwise they point at the host private directory.
//
// An empty sessionTemp yields nil (no overrides).
func SessionTempEnv(sessionTemp string, linuxSandboxed bool) []string {
	if sessionTemp == "" {
		return nil
	}
	value := sessionTemp
	if linuxSandboxed && runtime.GOOS == "linux" {
		value = "/tmp"
	}
	out := make([]string, 0, len(SessionTempEnvKeys))
	for _, key := range SessionTempEnvKeys {
		out = append(out, key+"="+value)
	}
	return out
}

// SessionTempEnvMap is SessionTempEnv as a name→value map for ACP terminal/create.
func SessionTempEnvMap(sessionTemp string, linuxSandboxed bool) map[string]string {
	pairs := SessionTempEnv(sessionTemp, linuxSandboxed)
	if len(pairs) == 0 {
		return nil
	}
	out := make(map[string]string, len(pairs))
	for _, kv := range pairs {
		for i := range len(kv) {
			if kv[i] == '=' {
				out[kv[:i]] = kv[i+1:]
				break
			}
		}
	}
	return out
}
