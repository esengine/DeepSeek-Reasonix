package terminal

import "strings"

// PersonEnv is the environment a person's own shell is started with: the host's,
// minus what only the host is meant to hold. The shell is not a tool the model
// drives, so the provider keys and the host's own settings stay behind.
func PersonEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		key, _, _ := strings.Cut(kv, "=")
		key = strings.ToUpper(key)
		if strings.HasPrefix(key, "REASONIX_") || strings.HasSuffix(key, "_API_KEY") {
			continue
		}
		out = append(out, kv)
	}
	return out
}
