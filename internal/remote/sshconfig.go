package remote

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	ssh_config "github.com/kevinburke/ssh_config"
)

// SSHConfigSource discovers aliases from a parsed OpenSSH client config and
// resolves their effective values through the installed `ssh -G`. The embedded
// parser remains a compatibility fallback when the OpenSSH executable is not
// available.
type SSHConfigSource struct {
	cfg             *ssh_config.Config
	path            string
	openSSHPath     string
	aliases         []string
	resolveOpenSSH  func(context.Context, string, string) ([]byte, error)
	effectiveMu     sync.Mutex
	effectiveByHost map[string]EffectiveSSHConfig
	effectiveErr    map[string]error
}

// EffectiveSSHConfig is the subset of `ssh -G` output consumed by Reasonix.
// Keeping every IdentityFile is important: OpenSSH permits the directive to be
// repeated and probes the resulting identities in order.
type EffectiveSSHConfig struct {
	HostName         string
	User             string
	Port             int
	IdentityFiles    []string
	IdentityFileNone bool
	ProxyJump        string
	IdentitiesOnly   bool
}

// LoadUserSSHConfig parses ~/.ssh/config. A missing file yields an empty
// source (all lookups return zero values), not an error.
func LoadUserSSHConfig() (*SSHConfigSource, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return newSSHConfigSource(nil, "", nil), nil
	}
	src, err := LoadSSHConfig(filepath.Join(home, ".ssh", "config"))
	if src != nil {
		// An empty -F argument means normal OpenSSH resolution: the default
		// per-user file plus the system ssh_config. Passing the default user path
		// explicitly with -F would incorrectly suppress the system configuration.
		src.openSSHPath = ""
	}
	return src, err
}

// LoadSSHConfig parses one OpenSSH client config file.
func LoadSSHConfig(path string) (*SSHConfigSource, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return newSSHConfigSource(nil, path, nil), nil
		}
		return nil, err
	}
	aliases, _ := discoverSSHAliases(path, 0, map[string]bool{})
	// The embedded parser is only a fallback. It intentionally rejects valid
	// OpenSSH constructs such as `Match exec`, while the installed OpenSSH
	// client accepts and evaluates them. Keep the discovered aliases and let
	// `ssh -G` remain authoritative even when the fallback cannot decode the
	// file.
	cfg, _ := ssh_config.Decode(strings.NewReader(string(contents)))
	return newSSHConfigSource(cfg, path, aliases), nil
}

func newSSHConfigSource(cfg *ssh_config.Config, path string, aliases []string) *SSHConfigSource {
	return &SSHConfigSource{
		cfg: cfg, path: path, openSSHPath: path, aliases: aliases,
		resolveOpenSSH:  runOpenSSHEffectiveConfig,
		effectiveByHost: map[string]EffectiveSSHConfig{},
		effectiveErr:    map[string]error{},
	}
}

// Path is the file this source was parsed from (may not exist).
func (s *SSHConfigSource) Path() string { return s.path }

func (s *SSHConfigSource) get(alias, key string) string {
	if s == nil || s.cfg == nil {
		return ""
	}
	v, err := s.cfg.Get(alias, key)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(v)
}

// Effective resolves alias through the user's installed OpenSSH client. This
// is the same source of truth used by VS Code Remote-SSH and covers Include,
// Host wildcards, Match rules, token expansion, and OpenSSH's precedence. If
// ssh is unavailable, Reasonix falls back to its embedded parser so existing
// installations without the executable keep working.
func (s *SSHConfigSource) Effective(alias string) EffectiveSSHConfig {
	effective, _ := s.EffectiveWithError(alias)
	return effective
}

// EffectiveWithError resolves alias without hiding an installed OpenSSH
// client's timeout or configuration error. The embedded parser is used only
// when ssh is genuinely unavailable (or a test explicitly disables it).
func (s *SSHConfigSource) EffectiveWithError(alias string) (EffectiveSSHConfig, error) {
	if s == nil || strings.TrimSpace(alias) == "" {
		return EffectiveSSHConfig{}, nil
	}
	alias = strings.TrimSpace(alias)
	s.effectiveMu.Lock()
	if s.effectiveByHost == nil {
		s.effectiveByHost = map[string]EffectiveSSHConfig{}
	}
	if s.effectiveErr == nil {
		s.effectiveErr = map[string]error{}
	}
	if cfg, ok := s.effectiveByHost[alias]; ok {
		err := s.effectiveErr[alias]
		s.effectiveMu.Unlock()
		return cloneEffectiveSSHConfig(cfg), err
	}
	s.effectiveMu.Unlock()

	var effective EffectiveSSHConfig
	var resolveErr error
	if s.resolveOpenSSH != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		output, err := s.resolveOpenSSH(ctx, s.openSSHPath, alias)
		cancel()
		if err == nil {
			effective, err = parseOpenSSHEffectiveConfig(output, alias)
			if err != nil {
				resolveErr = fmt.Errorf("parse OpenSSH config for %q: %w", alias, err)
			}
		} else if !errors.Is(err, exec.ErrNotFound) {
			resolveErr = fmt.Errorf("resolve OpenSSH config for %q: %w", alias, err)
		}
	}
	if resolveErr == nil && effective.HostName == "" {
		effective = s.parserEffective(alias)
	}

	s.effectiveMu.Lock()
	s.effectiveByHost[alias] = cloneEffectiveSSHConfig(effective)
	s.effectiveErr[alias] = resolveErr
	s.effectiveMu.Unlock()
	return cloneEffectiveSSHConfig(effective), resolveErr
}

// HasAlias reports whether alias was declared as a concrete Host entry. It is
// intentionally stricter than `ssh -G`: OpenSSH returns defaults for arbitrary
// host names, which must not make a user-facing label override an older saved
// Host lookup key.
func (s *SSHConfigSource) HasAlias(alias string) bool {
	alias = strings.TrimSpace(alias)
	if s == nil || alias == "" {
		return false
	}
	for _, candidate := range s.aliases {
		if candidate == alias && !strings.ContainsAny(candidate, "*?!") {
			return true
		}
	}
	return false
}

func runOpenSSHEffectiveConfig(ctx context.Context, path, alias string) ([]byte, error) {
	args := []string{"-G"}
	if strings.TrimSpace(path) != "" {
		args = append(args, "-F", path)
	}
	args = append(args, "--", alias)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ssh -G %q: %w", alias, err)
	}
	return output, nil
}

func parseOpenSSHEffectiveConfig(output []byte, alias string) (EffectiveSSHConfig, error) {
	var effective EffectiveSSHConfig
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.ToLower(key) {
		case "hostname":
			effective.HostName = value
		case "user":
			effective.User = value
		case "port":
			port, err := strconv.Atoi(value)
			if err == nil && port > 0 && port <= 65535 {
				effective.Port = port
			}
		case "identityfile":
			if strings.EqualFold(value, "none") {
				effective.IdentityFileNone = true
			} else if value != "" {
				effective.IdentityFiles = append(effective.IdentityFiles, expandHome(value))
			}
		case "proxyjump":
			if !strings.EqualFold(value, "none") {
				effective.ProxyJump = value
			}
		case "identitiesonly":
			effective.IdentitiesOnly = strings.EqualFold(value, "yes")
		}
	}
	if err := scanner.Err(); err != nil {
		return EffectiveSSHConfig{}, err
	}
	if effective.HostName == "" {
		effective.HostName = alias
	}
	return effective, nil
}

func (s *SSHConfigSource) parserEffective(alias string) EffectiveSSHConfig {
	if s == nil || s.cfg == nil {
		return EffectiveSSHConfig{HostName: alias}
	}
	hostName := s.get(alias, "HostName")
	if hostName == "" {
		hostName = alias
	}
	var identities []string
	identityFileNone := false
	if vals, err := s.cfg.GetAll(alias, "IdentityFile"); err == nil {
		for _, value := range vals {
			value = strings.TrimSpace(value)
			if strings.EqualFold(value, "none") {
				identityFileNone = true
				continue
			}
			if value == "" || value == ssh_config.Default("IdentityFile") {
				continue
			}
			identities = append(identities, expandHome(value))
		}
	}
	port := 0
	if value := s.get(alias, "Port"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 && parsed <= 65535 {
			port = parsed
		}
	}
	return EffectiveSSHConfig{
		HostName: hostName, User: s.get(alias, "User"), Port: port,
		IdentityFiles: identities, IdentityFileNone: identityFileNone, ProxyJump: s.get(alias, "ProxyJump"),
		IdentitiesOnly: strings.EqualFold(s.get(alias, "IdentitiesOnly"), "yes"),
	}
}

func cloneEffectiveSSHConfig(in EffectiveSSHConfig) EffectiveSSHConfig {
	in.IdentityFiles = append([]string(nil), in.IdentityFiles...)
	return in
}

// HostName returns the ssh_config HostName for alias, or "" when it would
// just echo the default/alias back.
func (s *SSHConfigSource) HostName(alias string) string {
	v := s.Effective(alias).HostName
	if v == "" || v == alias {
		return ""
	}
	return v
}

func (s *SSHConfigSource) User(alias string) string { return s.Effective(alias).User }

func (s *SSHConfigSource) Port(alias string) int {
	p := s.Effective(alias).Port
	if p == 22 {
		return 0
	}
	return p
}

// IdentityFile returns the first non-default identity file, ~-expanded.
func (s *SSHConfigSource) IdentityFile(alias string) string {
	identities := s.IdentityFiles(alias)
	if len(identities) == 0 {
		return ""
	}
	return identities[0]
}

func (s *SSHConfigSource) IdentityFiles(alias string) []string {
	return append([]string(nil), s.Effective(alias).IdentityFiles...)
}

func (s *SSHConfigSource) IdentityFileNone(alias string) bool {
	return s.Effective(alias).IdentityFileNone
}

func (s *SSHConfigSource) ProxyJump(alias string) string { return s.Effective(alias).ProxyJump }

func (s *SSHConfigSource) IdentitiesOnly(alias string) bool {
	return s.Effective(alias).IdentitiesOnly
}

// ImportedHost is one concrete Host alias surfaced by `remote import`.
type ImportedHost struct {
	Alias        string
	HostName     string
	User         string
	Port         int
	IdentityFile string
	ProxyJump    string
}

// Aliases lists concrete (non-wildcard, non-negated) Host aliases in file
// order without executing ssh -G or Match exec. Effective values are resolved
// only for a selected connection target.
func (s *SSHConfigSource) Aliases() []ImportedHost {
	if s == nil {
		return nil
	}
	seen := map[string]bool{}
	// File order is meaningful to users, so it is preserved as-is.
	out := make([]ImportedHost, 0, len(s.aliases))
	for _, alias := range s.aliases {
		if alias == "" || strings.ContainsAny(alias, "*?!") || seen[alias] {
			continue
		}
		seen[alias] = true
		out = append(out, ImportedHost{Alias: alias})
	}
	return out
}

// discoverSSHAliases walks Host and Include directives in file order. The
// upstream parser resolves values through Include nodes but does not expose
// included Host declarations, so import discovery needs this small read-only
// pass to avoid hiding the common ~/.ssh/config.d/* layout.
func discoverSSHAliases(filename string, depth int, seen map[string]bool) ([]string, error) {
	if depth > 5 {
		return nil, nil
	}
	abs, err := filepath.Abs(filename)
	if err == nil {
		filename = abs
	}
	if seen[filename] {
		return nil, nil
	}
	seen[filename] = true
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(stripSSHComment(line))
		if eq := strings.IndexByte(line, '='); eq >= 0 {
			if space := strings.IndexAny(line, " \t"); space < 0 || eq < space {
				line = line[:eq] + " " + line[eq+1:]
			}
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch strings.ToLower(fields[0]) {
		case "host":
			for _, alias := range fields[1:] {
				out = append(out, strings.Trim(alias, `"'`))
			}
		case "include":
			for _, directive := range fields[1:] {
				directive = expandHome(strings.Trim(directive, `"'`))
				if !filepath.IsAbs(directive) {
					if home, homeErr := os.UserHomeDir(); homeErr == nil {
						directive = filepath.Join(home, ".ssh", directive)
					}
				}
				matches, _ := filepath.Glob(directive)
				for _, match := range matches {
					aliases, includeErr := discoverSSHAliases(match, depth+1, seen)
					if includeErr == nil {
						out = append(out, aliases...)
					}
				}
			}
		}
	}
	return out, scanner.Err()
}

func stripSSHComment(line string) string {
	var quote rune
	for i, r := range line {
		switch {
		case quote != 0 && r == quote:
			quote = 0
		case quote == 0 && (r == '\'' || r == '"'):
			quote = r
		case quote == 0 && r == '#':
			return line[:i]
		}
	}
	return line
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
		}
	}
	return p
}
