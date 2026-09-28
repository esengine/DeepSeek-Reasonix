//go:build !windows

package sandbox

import (
	"os"
	"path/filepath"
	"strings"
)

// deniedAuthorityEndpoints are the existing sockets a backend must mask.
func deniedAuthorityEndpoints(s Spec) []string {
	getenv := effectiveGetenv(s.ShellEnv)
	var out []string
	for _, a := range GovernedAuthorities() {
		if s.Granted(a) {
			continue
		}
		out = append(out, existingSockets(authorityEndpoints(a, getenv))...)
	}
	return out
}

// effectiveGetenv resolves a variable the way the confined command will see it:
// the host environment with [tools.shell] env applied over it. A client inside
// that command reads the overridden value, so the socket it would connect to is
// the one this resolution has to name.
func effectiveGetenv(shellEnv map[string]string) func(string) string {
	if len(shellEnv) == 0 {
		return os.Getenv
	}
	return func(key string) string {
		if v, ok := shellEnv[key]; ok {
			return v
		}
		return os.Getenv(key)
	}
}

// authorityEndpoints resolves an authority the way its own client resolves it,
// so a non-default daemon is governed rather than quietly ungoverned.
func authorityEndpoints(a HostAuthority, getenv func(string) string) []string {
	switch a {
	case SSHAgent:
		return []string{getenv("SSH_AUTH_SOCK")}
	case Docker:
		out := []string{unixHost(getenv("DOCKER_HOST")), "/var/run/docker.sock"}
		if home, err := os.UserHomeDir(); err == nil {
			out = append(out, filepath.Join(home, ".docker", "run", "docker.sock"))
		}
		return out
	case Podman:
		out := []string{unixHost(getenv("CONTAINER_HOST")), "/run/podman/podman.sock"}
		if dir := getenv("XDG_RUNTIME_DIR"); dir != "" {
			out = append(out, filepath.Join(dir, "podman", "podman.sock"))
		}
		return out
	}
	return nil
}

// unixHost extracts the path from a unix:// endpoint; a tcp:// daemon is
// reached over the network axis instead and is not a socket to mask.
func unixHost(v string) string {
	if path, ok := strings.CutPrefix(strings.TrimSpace(v), "unix://"); ok {
		return path
	}
	return ""
}

// existingSockets keeps the paths that are sockets right now, resolved through
// symlinks so the backends match the canonical path the kernel sees. A missing
// endpoint is dropped: there is nothing to mask, and a mount destination that
// does not exist fails an otherwise valid sandbox closed.
func existingSockets(paths []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if strings.TrimSpace(p) == "" {
			continue
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		if real, err := filepath.EvalSymlinks(abs); err == nil {
			abs = real
		}
		info, err := os.Stat(abs)
		if err != nil || info.Mode()&os.ModeSocket == 0 || seen[abs] {
			continue
		}
		seen[abs] = true
		out = append(out, abs)
	}
	return out
}
