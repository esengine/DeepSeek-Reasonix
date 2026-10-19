package shellrun

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// LoginEnvironment merges the user's login-shell PATH into a desktop process
// environment, where shell startup files are not otherwise loaded.
func LoginEnvironment(ctx context.Context, env []string, shell string) []string {
	if runtime.GOOS == "windows" || strings.TrimSpace(shell) == "" {
		return env
	}
	current := envValue(env, "PATH")
	if current == "" {
		return env
	}
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, shell, "-l", "-i", "-c", "printf '\\n__REASONIX_LOGIN_PATH__=%s\\n' \"$PATH\"")
	cmd.Env = env
	cmd.Stdin = strings.NewReader("")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return env
	}
	const marker = "__REASONIX_LOGIN_PATH__="
	var login string
	for _, line := range strings.Split(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n") {
		if rest, ok := strings.CutPrefix(line, marker); ok {
			login = strings.TrimSpace(rest)
		}
	}
	if login == "" || login == current {
		return env
	}
	return setEnvValue(env, "PATH", mergePath(login, current))
}

func envValue(env []string, key string) string {
	for _, item := range env {
		if k, v, ok := strings.Cut(item, "="); ok && k == key {
			return v
		}
	}
	return ""
}

func setEnvValue(env []string, key, value string) []string {
	out := append([]string(nil), env...)
	for i, item := range out {
		if strings.HasPrefix(item, key+"=") {
			out[i] = key + "=" + value
			return out
		}
	}
	return append(out, key+"="+value)
}

func mergePath(login, current string) string {
	seen := map[string]bool{}
	var parts []string
	for _, list := range []string{login, current} {
		for _, part := range strings.Split(list, string(os.PathListSeparator)) {
			if part != "" && !seen[part] {
				seen[part] = true
				parts = append(parts, part)
			}
		}
	}
	return strings.Join(parts, string(os.PathListSeparator))
}
