package remote

import (
	"fmt"
	"net"
	"os"
	"os/user"
	"strconv"
	"strings"

	"reasonix/internal/config"
)

// ResolvedHost is a fully resolved dial target: explicit [remote] TOML fields
// layered over ~/.ssh/config values (when use_ssh_config) over defaults.
type ResolvedHost struct {
	Name          string // config entry name, or the raw target for ad-hoc dials
	HostName      string // network address to dial
	Port          int
	User          string
	IdentityFile  string   // explicit key path; empty => agent/default identities
	PassphraseEnv string   // credential env var name for the key passphrase
	PasswordEnv   string   // credential env var name for password auth
	ProxyJump     []string // resolved jump chain, in dial order
	Workspace     string   // default remote workspace directory
	ServeInstall  string   // auto|npm|upload|never
	Forwards      []config.RemoteForwardEntry
}

// Addr is the host:port dial string.
func (h ResolvedHost) Addr() string {
	return net.JoinHostPort(h.HostName, strconv.Itoa(h.Port))
}

// Label is the display form user@host:port.
func (h ResolvedHost) Label() string {
	label := h.HostName
	if h.User != "" {
		label = h.User + "@" + label
	}
	if h.Port != 0 && h.Port != 22 {
		label += ":" + strconv.Itoa(h.Port)
	}
	return label
}

// ParseTarget splits an ad-hoc "[user@]host[:port]" target. IPv6 literals use
// the bracketed form "[::1]:22".
func ParseTarget(s string) (userName, host string, port int, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", 0, fmt.Errorf("empty ssh target")
	}
	if at := strings.LastIndex(s, "@"); at >= 0 {
		userName, s = s[:at], s[at+1:]
		if userName == "" || s == "" {
			return "", "", 0, fmt.Errorf("invalid ssh target %q", s)
		}
	}
	host = s
	port = 0
	if strings.HasPrefix(s, "[") {
		// Bracketed IPv6, optionally with :port.
		end := strings.Index(s, "]")
		if end < 0 {
			return "", "", 0, fmt.Errorf("invalid ssh target %q: unclosed '['", s)
		}
		host = s[1:end]
		rest := s[end+1:]
		if rest != "" {
			if !strings.HasPrefix(rest, ":") {
				return "", "", 0, fmt.Errorf("invalid ssh target %q", s)
			}
			port, err = parsePort(rest[1:])
			if err != nil {
				return "", "", 0, err
			}
		}
	} else if i := strings.LastIndex(s, ":"); i >= 0 {
		if strings.Count(s, ":") > 1 {
			// Bare IPv6 literal without a port.
			host = s
		} else {
			host = s[:i]
			port, err = parsePort(s[i+1:])
			if err != nil {
				return "", "", 0, err
			}
		}
	}
	if host == "" {
		return "", "", 0, fmt.Errorf("invalid ssh target %q: empty host", s)
	}
	return userName, host, port, nil
}

func parsePort(s string) (int, error) {
	p, err := strconv.Atoi(s)
	if err != nil || p <= 0 || p > 65535 {
		return 0, fmt.Errorf("invalid ssh port %q", s)
	}
	return p, nil
}

// ResolveHost builds the dial target for a configured host name or an ad-hoc
// "[user@]host[:port]" target. Field precedence: explicit TOML value →
// ~/.ssh/config value (only when the entry sets use_ssh_config, or for ad-hoc
// targets when sshCfg is non-nil) → default (port 22, current OS user).
func ResolveHost(cfg *config.Config, nameOrTarget string, sshCfg *SSHConfigSource) (ResolvedHost, error) {
	if cfg != nil {
		if e, ok := cfg.RemoteHost(nameOrTarget); ok {
			return resolveEntry(e, sshCfg)
		}
	}
	userName, host, port, err := ParseTarget(nameOrTarget)
	if err != nil {
		return ResolvedHost{}, err
	}
	r := ResolvedHost{Name: nameOrTarget, HostName: host, Port: port, User: userName}
	applySSHConfig(&r, host, sshCfg)
	applyHostDefaults(&r)
	return r, nil
}

// ResolveJumpHosts resolves every ProxyJump token through the same Reasonix
// host table and ~/.ssh/config layers as the final target. A jump entry's own
// ProxyJump is deliberately cleared: the caller-provided chain is already the
// complete left-to-right route, and recursively expanding nested chains would
// make ordering and credential ownership ambiguous.
func ResolveJumpHosts(cfg *config.Config, chain []string, sshCfg *SSHConfigSource) ([]ResolvedHost, error) {
	out := make([]ResolvedHost, 0, len(chain))
	for i, raw := range chain {
		hop, err := ResolveHost(cfg, raw, sshCfg)
		if err != nil {
			return nil, fmt.Errorf("proxy jump %d (%q): %w", i+1, raw, err)
		}
		hop.ProxyJump = nil
		out = append(out, hop)
	}
	return out, nil
}

func resolveEntry(e config.RemoteHostEntry, sshCfg *SSHConfigSource) (ResolvedHost, error) {
	r := ResolvedHost{
		Name:          e.Name,
		HostName:      strings.TrimSpace(e.Host),
		Port:          e.Port,
		User:          strings.TrimSpace(e.User),
		IdentityFile:  strings.TrimSpace(e.IdentityFile),
		PassphraseEnv: strings.TrimSpace(e.PassphraseEnv),
		PasswordEnv:   strings.TrimSpace(e.PasswordEnv),
		Workspace:     strings.TrimSpace(e.Workspace),
		ServeInstall:  e.ServeInstallMode(),
		Forwards:      e.Forwards,
	}
	if j := strings.TrimSpace(e.ProxyJump); j != "" {
		r.ProxyJump = splitJumpChain(j)
	}
	if e.UseSSHConfig {
		// The TOML host field doubles as the ssh_config alias lookup key; the
		// resolved HostName may differ (ssh_config HostName wins for unset).
		applySSHConfig(&r, r.HostName, sshCfg)
	}
	applyHostDefaults(&r)
	if r.HostName == "" {
		return ResolvedHost{}, fmt.Errorf("remote host %q: empty hostname after resolution", e.Name)
	}
	return r, nil
}

// applySSHConfig fills unset fields from ~/.ssh/config for alias.
func applySSHConfig(r *ResolvedHost, alias string, sshCfg *SSHConfigSource) {
	if sshCfg == nil || alias == "" {
		return
	}
	if hn := sshCfg.HostName(alias); hn != "" {
		// An explicit TOML host that matched an alias keeps the alias only as
		// the lookup key; the network target comes from ssh_config.
		r.HostName = hn
	}
	if r.Port == 0 {
		r.Port = sshCfg.Port(alias)
	}
	if r.User == "" {
		r.User = sshCfg.User(alias)
	}
	if r.IdentityFile == "" {
		r.IdentityFile = sshCfg.IdentityFile(alias)
	}
	if len(r.ProxyJump) == 0 {
		if j := sshCfg.ProxyJump(alias); j != "" {
			r.ProxyJump = splitJumpChain(j)
		}
	}
}

func applyHostDefaults(r *ResolvedHost) {
	if r.Port == 0 {
		r.Port = 22
	}
	if r.User == "" {
		if u, err := user.Current(); err == nil && u.Username != "" {
			r.User = u.Username
		} else if env := os.Getenv("USER"); env != "" {
			r.User = env
		}
	}
	if r.ServeInstall == "" {
		r.ServeInstall = "auto"
	}
}

func splitJumpChain(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" && !strings.EqualFold(p, "none") {
			out = append(out, p)
		}
	}
	return out
}
