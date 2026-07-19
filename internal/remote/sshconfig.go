package remote

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	ssh_config "github.com/kevinburke/ssh_config"
)

// SSHConfigSource answers per-alias lookups against a parsed OpenSSH client
// config (~/.ssh/config plus Includes). Match blocks are not evaluated by the
// underlying parser; ImportedHost.HasMatchRules is unset because such stanzas
// are simply invisible here — `remote import` notes that limitation.
type SSHConfigSource struct {
	cfg     *ssh_config.Config
	path    string
	aliases []string
}

// LoadUserSSHConfig parses ~/.ssh/config. A missing file yields an empty
// source (all lookups return zero values), not an error.
func LoadUserSSHConfig() (*SSHConfigSource, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return &SSHConfigSource{}, nil
	}
	return LoadSSHConfig(filepath.Join(home, ".ssh", "config"))
}

// LoadSSHConfig parses one OpenSSH client config file.
func LoadSSHConfig(path string) (*SSHConfigSource, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &SSHConfigSource{path: path}, nil
		}
		return nil, err
	}
	defer f.Close()
	cfg, err := ssh_config.Decode(f)
	if err != nil {
		return nil, err
	}
	aliases, _ := discoverSSHAliases(path, 0, map[string]bool{})
	return &SSHConfigSource{cfg: cfg, path: path, aliases: aliases}, nil
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

// HostName returns the ssh_config HostName for alias, or "" when it would
// just echo the default/alias back.
func (s *SSHConfigSource) HostName(alias string) string {
	v := s.get(alias, "HostName")
	if v == "" || v == alias {
		return ""
	}
	return v
}

func (s *SSHConfigSource) User(alias string) string { return s.get(alias, "User") }

func (s *SSHConfigSource) Port(alias string) int {
	v := s.get(alias, "Port")
	if v == "" {
		return 0
	}
	p, err := strconv.Atoi(v)
	if err != nil || p <= 0 || p > 65535 || p == 22 {
		return 0
	}
	return p
}

// IdentityFile returns the first non-default identity file, ~-expanded.
func (s *SSHConfigSource) IdentityFile(alias string) string {
	if s == nil || s.cfg == nil {
		return ""
	}
	vals, err := s.cfg.GetAll(alias, "IdentityFile")
	if err != nil || len(vals) == 0 {
		return ""
	}
	v := strings.TrimSpace(vals[0])
	// The parser reports its built-in default (~/.ssh/identity) when the
	// config has no entry; treat it as unset so the auth chain probes the
	// modern default identities instead.
	if v == "" || v == ssh_config.Default("IdentityFile") {
		return ""
	}
	return expandHome(v)
}

func (s *SSHConfigSource) ProxyJump(alias string) string { return s.get(alias, "ProxyJump") }

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
// order, deduplicated, each resolved through the full config.
func (s *SSHConfigSource) Aliases() []ImportedHost {
	if s == nil || s.cfg == nil {
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
		out = append(out, ImportedHost{
			Alias:        alias,
			HostName:     s.HostName(alias),
			User:         s.User(alias),
			Port:         s.Port(alias),
			IdentityFile: s.IdentityFile(alias),
			ProxyJump:    s.ProxyJump(alias),
		})
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
