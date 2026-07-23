package config

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// RemoteConfig is the [remote] section: SSH hosts the remote module may
// connect to, and their default forwards/workspaces. Like [secrets] it is a
// user-global security control — LoadForRoot pins it back to the user config
// after the project merge so a cloned repo's reasonix.toml can never inject
// hosts, jump chains, or forwards.
type RemoteConfig struct {
	// ImportSSHConfig surfaces ~/.ssh/config aliases in `reasonix remote import`.
	ImportSSHConfig bool              `toml:"import_ssh_config"`
	Hosts           []RemoteHostEntry `toml:"hosts"`
}

// RemoteHostEntry describes one SSH target. Secrets follow the provider
// idiom: the entry names credential env vars (passphrase_env/password_env);
// values live in Reasonix's global .env, never in TOML. identity_file is a
// path — private key material itself is never stored by Reasonix.
type RemoteHostEntry struct {
	Name          string               `toml:"name"`
	Host          string               `toml:"host"`
	Port          int                  `toml:"port"` // 0 => 22 (or ssh_config value)
	User          string               `toml:"user"`
	IdentityFile  string               `toml:"identity_file"`
	PassphraseEnv string               `toml:"passphrase_env"`
	PasswordEnv   string               `toml:"password_env"`
	ProxyJump     string               `toml:"proxy_jump"`     // OpenSSH ProxyJump syntax, comma-separated chain
	Workspace     string               `toml:"workspace"`      // default remote workspace dir
	ServeInstall  string               `toml:"serve_install"`  // auto|npm|upload|never
	UseSSHConfig  bool                 `toml:"use_ssh_config"` // layer ~/.ssh/config values under unset fields
	Forwards      []RemoteForwardEntry `toml:"forwards"`
}

// RemoteForwardEntry is a persisted port-forward rule applied on connect.
type RemoteForwardEntry struct {
	Type   string `toml:"type"`   // "local" (-L) | "remote" (-R)
	Bind   string `toml:"bind"`   // "127.0.0.1:8080" or bare port => 127.0.0.1:<port>
	Target string `toml:"target"` // host:port on the other side
}

// RemoteServeInstallModes are the accepted serve_install values.
var RemoteServeInstallModes = []string{"auto", "npm", "upload", "never"}

// Clone returns a deep copy. The global-only pin in loadForRoot must capture
// the pre-project-merge value, but TOML decoding mutates existing slice
// backing arrays in place — a shallow struct copy would alias Hosts (and each
// host's Forwards) and let a project reasonix.toml overwrite the "restored"
// global entries.
func (r RemoteConfig) Clone() RemoteConfig {
	out := r
	if r.Hosts != nil {
		out.Hosts = make([]RemoteHostEntry, len(r.Hosts))
		for i, h := range r.Hosts {
			h.Forwards = append([]RemoteForwardEntry(nil), h.Forwards...)
			out.Hosts[i] = h
		}
	}
	return out
}

// ServeInstallMode returns the normalized install strategy, defaulting to auto.
func (e RemoteHostEntry) ServeInstallMode() string {
	m := strings.ToLower(strings.TrimSpace(e.ServeInstall))
	if m == "" {
		return "auto"
	}
	return m
}

// PortOrDefault returns the configured port, defaulting to 22.
func (e RemoteHostEntry) PortOrDefault() int {
	if e.Port > 0 {
		return e.Port
	}
	return 22
}

func validateRemoteHost(e RemoteHostEntry) error {
	if strings.TrimSpace(e.Name) == "" {
		return fmt.Errorf("remote host: name is required")
	}
	if strings.ContainsAny(e.Name, " \t/:@") {
		return fmt.Errorf("remote host %q: name must not contain spaces, '/', ':' or '@'", e.Name)
	}
	if strings.TrimSpace(e.Host) == "" {
		return fmt.Errorf("remote host %q: host is required", e.Name)
	}
	if e.Port < 0 || e.Port > 65535 {
		return fmt.Errorf("remote host %q: port %d out of range", e.Name, e.Port)
	}
	switch e.ServeInstallMode() {
	case "auto", "npm", "upload", "never":
	default:
		return fmt.Errorf("remote host %q: serve_install must be one of auto|npm|upload|never", e.Name)
	}
	seenBinds := map[string]bool{}
	for _, f := range e.Forwards {
		kind := strings.ToLower(strings.TrimSpace(f.Type))
		switch kind {
		case "local", "remote":
		default:
			return fmt.Errorf("remote host %q: forward type must be \"local\" or \"remote\"", e.Name)
		}
		if strings.TrimSpace(f.Bind) == "" || strings.TrimSpace(f.Target) == "" {
			return fmt.Errorf("remote host %q: forward needs both bind and target", e.Name)
		}
		bind, err := validateRemoteForwardAddress(f.Bind, true)
		if err != nil {
			return fmt.Errorf("remote host %q: invalid forward bind %q: %w", e.Name, f.Bind, err)
		}
		if _, err := validateRemoteForwardAddress(f.Target, false); err != nil {
			return fmt.Errorf("remote host %q: invalid forward target %q: %w", e.Name, f.Target, err)
		}
		key := kind + "\x00" + bind
		if seenBinds[key] {
			return fmt.Errorf("remote host %q: duplicate %s forward bind %q", e.Name, kind, f.Bind)
		}
		seenBinds[key] = true
	}
	return nil
}

func validateRemoteForwardAddress(addr string, bind bool) (string, error) {
	addr = strings.TrimSpace(addr)
	if bind && !strings.Contains(addr, ":") {
		addr = net.JoinHostPort("127.0.0.1", addr)
	}
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		return "", err
	}
	if !bind && strings.TrimSpace(host) == "" {
		return "", fmt.Errorf("host is required")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 0 || port > 65535 || (!bind && port == 0) {
		return "", fmt.Errorf("port out of range")
	}
	return net.JoinHostPort(host, strconv.Itoa(port)), nil
}

// RemoteHost looks up a configured host by name.
func (c *Config) RemoteHost(name string) (RemoteHostEntry, bool) {
	for _, h := range c.Remote.Hosts {
		if h.Name == name {
			return h, true
		}
	}
	return RemoteHostEntry{}, false
}

// UpsertRemoteHost adds e, or replaces the host with the same name
// (preserving position). Mirrors UpsertPlugin.
func (c *Config) UpsertRemoteHost(e RemoteHostEntry) error {
	if err := validateRemoteHost(e); err != nil {
		return err
	}
	for i := range c.Remote.Hosts {
		if c.Remote.Hosts[i].Name == e.Name {
			c.Remote.Hosts[i] = e
			return nil
		}
	}
	c.Remote.Hosts = append(c.Remote.Hosts, e)
	return nil
}

// RemoveRemoteHost deletes the named host, reporting whether it was present.
func (c *Config) RemoveRemoteHost(name string) bool {
	for i := range c.Remote.Hosts {
		if c.Remote.Hosts[i].Name == name {
			c.Remote.Hosts = append(c.Remote.Hosts[:i], c.Remote.Hosts[i+1:]...)
			return true
		}
	}
	return false
}
