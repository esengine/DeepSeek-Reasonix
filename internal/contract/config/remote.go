package config

import (
	"fmt"
	"net"
	"slices"
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
	Name string `toml:"name"`
	// Kind is how this machine is reached: "ssh" (the default) or "wsl", a WSL
	// distribution on this Windows machine named by Distro.
	Kind          string `toml:"kind,omitempty"`
	Distro        string `toml:"distro,omitempty"`
	Host          string `toml:"host"`
	Port          int    `toml:"port"` // 0 => 22 (or ssh_config value)
	User          string `toml:"user"`
	IdentityFile  string `toml:"identity_file"`
	PassphraseEnv string `toml:"passphrase_env"`
	PasswordEnv   string `toml:"password_env"`
	ProxyJump     string `toml:"proxy_jump"` // OpenSSH ProxyJump syntax, comma-separated chain
	Workspace     string `toml:"workspace"`  // default remote workspace dir
	// Workspaces are the other folders worked in on this machine. One machine
	// holds several projects, and the default is only the one a bare connect
	// lands in — read them together through WorkspaceList.
	Workspaces       []string             `toml:"workspaces,omitempty"`
	HiddenWorkspaces []string             `toml:"hidden_workspaces,omitempty"`
	ServeInstall     string               `toml:"serve_install"`  // remote CLI: auto|npm|upload|never
	Provider         string               `toml:"provider"`       // model credentials: local (this machine, over the tunnel) | remote (that host's own)
	UseSSHConfig     bool                 `toml:"use_ssh_config"` // layer ~/.ssh/config values under unset fields
	Forwards         []RemoteForwardEntry `toml:"forwards"`
	// Disabled keeps the row in the book but stops this machine from being
	// dialed. Written as "disabled" rather than "enabled" so an entry written
	// before the field existed still means what it always did.
	Disabled bool `toml:"disabled"`
}

// IsEnabled reports whether this machine may be dialed. The zero value is on.
func (e RemoteHostEntry) IsEnabled() bool { return !e.Disabled }

// RemoteForwardEntry is a persisted port-forward rule applied on connect.
type RemoteForwardEntry struct {
	Type   string `toml:"type"`   // "local" (-L) | "remote" (-R)
	Bind   string `toml:"bind"`   // "127.0.0.1:8080" or bare port => 127.0.0.1:<port>
	Target string `toml:"target"` // host:port on the other side
}

// RemoteServeInstallModes are the accepted serve_install values.
var RemoteServeInstallModes = []string{"auto", "npm", "upload", "never"}

// RemoteProviderModes are the accepted provider values.
var RemoteProviderModes = []string{"local", "remote"}

// RemoteProviderLocal resolves a remote session's models on this machine, over
// a forward the connection already carries, so that host needs neither an API
// key nor egress of its own. It is the default: needing the credentials
// configured twice is what made a first connect a setup task.
const RemoteProviderLocal = "local"

// RemoteProviderRemote leaves a host resolving models from its own config —
// for a machine whose providers are deliberately not this one's.
const RemoteProviderRemote = "remote"

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
			h.Workspaces = append([]string(nil), h.Workspaces...)
			h.HiddenWorkspaces = slices.Clone(h.HiddenWorkspaces)
			out.Hosts[i] = h
		}
	}
	return out
}

// Remote host kinds.
const (
	RemoteKindSSH = "ssh"
	RemoteKindWSL = "wsl"
)

// HostKind is the normalized kind, defaulting to ssh. An unrecognized value is
// kept as written so a validator can name it, never read as ssh.
func (e RemoteHostEntry) HostKind() string {
	k := strings.ToLower(strings.TrimSpace(e.Kind))
	if k == "" {
		return RemoteKindSSH
	}
	return k
}

// Target spells the machine the way a person would type it: user@host[:port]
// for ssh, with the port shown only when it is not 22, and wsl:<distro> for a
// WSL distribution.
func (e RemoteHostEntry) Target() string {
	if e.HostKind() == RemoteKindWSL {
		return "wsl:" + e.Distro
	}
	target := e.Host
	if e.User != "" {
		target = e.User + "@" + target
	}
	if e.Port > 0 && e.Port != 22 {
		target += ":" + strconv.Itoa(e.Port)
	}
	return target
}

// ServeInstallMode returns the normalized install strategy, defaulting to auto.
func (e RemoteHostEntry) ServeInstallMode() string {
	m := strings.ToLower(strings.TrimSpace(e.ServeInstall))
	if m == "" {
		return "auto"
	}
	return m
}

// ProviderMode returns where this host's model credentials come from,
// defaulting to local. An unrecognized value is not silently reinterpreted as
// the other one: it reads as the default, which is also what an entry written
// by a newer Reasonix looks like to an older one. A WSL distribution always
// reads its own: it cannot be handed this machine's credentials.
func (e RemoteHostEntry) ProviderMode() string {
	if e.HostKind() == RemoteKindWSL {
		return RemoteProviderRemote
	}
	if strings.EqualFold(strings.TrimSpace(e.Provider), RemoteProviderRemote) {
		return RemoteProviderRemote
	}
	return RemoteProviderLocal
}

// WorkspaceList is every folder this host is worked in, default first and no
// duplicates. A config that only ever set workspace is a one-element list, so
// no reader has to know which of the two fields a folder came from.
func (e RemoteHostEntry) WorkspaceList() []string {
	out := make([]string, 0, len(e.Workspaces)+1)
	seen := map[string]bool{}
	for _, dir := range append([]string{e.Workspace}, e.Workspaces...) {
		dir = strings.TrimSpace(dir)
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		out = append(out, dir)
	}
	return out
}

// DefaultWorkspace prefers the declared default over the order of other folders.
func (e RemoteHostEntry) DefaultWorkspace() string {
	if dir := strings.TrimSpace(e.Workspace); dir != "" {
		return dir
	}
	if dirs := e.WorkspaceList(); len(dirs) > 0 {
		return dirs[0]
	}
	return ""
}

// withWorkspaceList rewrites both fields from one list, so the pair can never
// disagree about which folder a bare connect lands in: the head is the default,
// and the rest is what is left.
func (e RemoteHostEntry) withWorkspaceList(dirs []string) RemoteHostEntry {
	e.Workspace, e.Workspaces = "", nil
	e.Workspaces = dirs
	normalized := e.WorkspaceList()
	e.Workspaces = nil
	if len(normalized) > 0 {
		e.Workspace, e.Workspaces = normalized[0], normalized[1:]
	}
	if len(e.Workspaces) == 0 {
		e.Workspaces = nil
	}
	return e
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
	switch e.HostKind() {
	case RemoteKindSSH:
		if strings.TrimSpace(e.Distro) != "" {
			return fmt.Errorf("remote host %q: distro belongs to kind \"wsl\"", e.Name)
		}
		if strings.TrimSpace(e.Host) == "" {
			return fmt.Errorf("remote host %q: host is required", e.Name)
		}
		if e.Port < 0 || e.Port > 65535 {
			return fmt.Errorf("remote host %q: port %d out of range", e.Name, e.Port)
		}
	case RemoteKindWSL:
		if err := validateWSLHost(e); err != nil {
			return err
		}
	default:
		return fmt.Errorf("remote host %q: kind must be \"ssh\" or \"wsl\"", e.Name)
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

// maxWSLDistroName bounds a distribution name. It is this package's own limit,
// well above any name a store image or `wsl --import` produces in practice.
const maxWSLDistroName = 64

// ValidWSLDistroName reports whether name is spelled the way a distribution is:
// ASCII letters, digits, '.', '_' and '-', never starting with '-'. A name
// handed to wsl.exe after -d must be one argument, never part of a shell string,
// and one that cannot begin with '-' cannot be read as an option.
func ValidWSLDistroName(name string) bool {
	if name == "" || len(name) > maxWSLDistroName || name[0] == '-' {
		return false
	}
	for i := range len(name) {
		c := name[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

// validateWSLHost holds a WSL entry to the fields that mean something for it: a
// distribution reached without a network hop has no address, login, key or
// tunnel, and keeping those out means no reader mistakes one for configuration
// that is in force.
func validateWSLHost(e RemoteHostEntry) error {
	if !ValidWSLDistroName(e.Distro) {
		return fmt.Errorf("remote host %q: distro must be 1 to %d letters, digits, '.', '_' or '-', not starting with '-'", e.Name, maxWSLDistroName)
	}
	ssh := e.Host != "" || e.Port != 0 || e.User != "" || e.IdentityFile != "" || e.PassphraseEnv != "" ||
		e.PasswordEnv != "" || e.ProxyJump != "" || e.UseSSHConfig || len(e.Forwards) > 0
	if ssh {
		return fmt.Errorf("remote host %q: host, port, user, identity_file, passphrase_env, password_env, proxy_jump, use_ssh_config and forwards apply to ssh hosts only", e.Name)
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
	e = e.withWorkspaceList(e.WorkspaceList())
	if previous, ok := c.RemoteHost(e.Name); ok {
		e.HiddenWorkspaces = append(slices.Clone(e.HiddenWorkspaces), previous.HiddenWorkspaces...)
	}
	e.HiddenWorkspaces = (RemoteHostEntry{Workspaces: e.HiddenWorkspaces}).WorkspaceList()
	e.HiddenWorkspaces = slices.DeleteFunc(e.HiddenWorkspaces, func(dir string) bool {
		return slices.Contains(e.WorkspaceList(), dir)
	})
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

// AddRemoteWorkspace records dir among the folders host is worked in, keeping
// whichever one is already the default at the head. A machine with no folder
// written down yet takes this one as its default. Reports whether the book
// changed, so a caller recording a folder it just opened writes nothing when it
// was already there.
func (c *Config) AddRemoteWorkspace(name, dir string) bool {
	dir = strings.TrimSpace(dir)
	entry, ok := c.RemoteHost(name)
	if !ok || dir == "" {
		return false
	}
	list := entry.WorkspaceList()
	if slices.Contains(list, dir) {
		return false
	}
	return c.UpsertRemoteHost(entry.withWorkspaceList(append(list, dir))) == nil
}

// RemoveRemoteWorkspace drops dir from host's folders. Nothing on the far
// machine is touched — the folder stays where it is and can be added back.
// Dropping the head promotes the next one, because a list with no head is a
// machine that forgot where a bare connect lands.
func (c *Config) RemoveRemoteWorkspace(name, dir string) bool {
	dir = strings.TrimSpace(dir)
	entry, ok := c.RemoteHost(name)
	if !ok || dir == "" {
		return false
	}
	list := entry.WorkspaceList()
	kept := slices.DeleteFunc(slices.Clone(list), func(x string) bool { return x == dir })
	if len(kept) == len(list) && slices.Contains(entry.HiddenWorkspaces, dir) {
		return false
	}
	entry.HiddenWorkspaces = append(slices.Clone(entry.HiddenWorkspaces), dir)
	return c.UpsertRemoteHost(entry.withWorkspaceList(kept)) == nil
}

// PlaceRemoteHost puts the named host immediately before the host named before,
// or last when before is empty: the order the sidebar lists machines in. Naming
// the neighbour rather than a step keeps the move right when the sidebar hides
// some machines between the two. Reports whether the book changed.
func (c *Config) PlaceRemoteHost(name, before string) bool {
	hosts := c.Remote.Hosts
	from := slices.IndexFunc(hosts, func(h RemoteHostEntry) bool { return h.Name == name })
	if from < 0 || name == before {
		return false
	}
	moved := hosts[from]
	rest := slices.Delete(slices.Clone(hosts), from, from+1)
	at := len(rest)
	if before != "" {
		if at = slices.IndexFunc(rest, func(h RemoteHostEntry) bool { return h.Name == before }); at < 0 {
			return false
		}
	}
	placed := slices.Insert(rest, at, moved)
	if slices.EqualFunc(placed, hosts, func(a, b RemoteHostEntry) bool { return a.Name == b.Name }) {
		return false
	}
	c.Remote.Hosts = placed
	return true
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
