package builtin

import (
	"maps"
	"slices"
	"strings"
)

// shellEnvOverrides renders the configured [tools.shell] env as sorted KEY=value
// pairs. Sorted so identical config yields identical environment bytes; a key
// that cannot appear in an environment entry is dropped rather than mangled.
func shellEnvOverrides(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		if k == "" || strings.ContainsAny(k, "=\x00") {
			continue
		}
		keys = append(keys, k)
	}
	slices.Sort(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+env[k])
	}
	return out
}

// shellEnvMap folds the configured shell env under the host-owned session
// variables, which win, so a preset value cannot displace TMPDIR.
func shellEnvMap(shellEnv, sessionTemp map[string]string) map[string]string {
	if len(shellEnv) == 0 {
		return sessionTemp
	}
	merged := maps.Clone(shellEnv)
	maps.Copy(merged, sessionTemp)
	return merged
}
