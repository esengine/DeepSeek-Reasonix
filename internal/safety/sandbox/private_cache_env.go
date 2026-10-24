package sandbox

import "path/filepath"

// PrivateProcessEnv directs default build caches into an execution's writable
// temporary tree. Explicit user environment choices remain unchanged.
func PrivateProcessEnv(spec Spec, sessionTemp string, linuxSandboxed bool) []string {
	result := SessionTempEnv(sessionTemp, linuxSandboxed)
	if spec.RequiredIntegrity == "" || sessionTemp == "" {
		return result
	}
	root := sessionTemp
	if linuxSandboxed {
		root = "/tmp"
	}
	for _, cache := range []struct{ key, path string }{
		{"XDG_CACHE_HOME", "cache"}, {"GOCACHE", "cache/go-build"}, {"GOMODCACHE", "cache/go-mod"},
		{"NPM_CONFIG_CACHE", "cache/npm"}, {"PIP_CACHE_DIR", "cache/pip"}, {"UV_CACHE_DIR", "cache/uv"},
	} {
		if _, explicit := spec.ShellEnv[cache.key]; !explicit {
			result = append(result, cache.key+"="+filepath.Join(root, filepath.FromSlash(cache.path)))
		}
	}
	return result
}
