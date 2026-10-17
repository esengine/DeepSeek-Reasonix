package agentpreset

import (
	"fmt"
	"maps"
	"sort"
	"strings"

	"reasonix/internal/base/secrets"
	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/contract/config"
)

type MCPSnapshot struct {
	Entry               config.PluginEntry
	EnvKeys, HeaderKeys []string
	ArgReferences       map[int]string
	URLReference        bool
	URLShape            string
	AuthorizedCommand   string
}

func CaptureMCP(entry config.PluginEntry) MCPSnapshot {
	s := MCPSnapshot{Entry: agentcomposition.Clone(entry), ArgReferences: map[int]string{}}
	s.Entry.Env, s.EnvKeys = CaptureEnvironment(entry.Env)
	s.Entry.Headers, s.HeaderKeys = credentialMap(entry.Headers)
	redacted := secrets.RedactArgs(entry.Args)
	for i, arg := range entry.Args {
		_, value, inline := strings.Cut(arg, "=")
		if redacted[i] == arg || reference(arg) || inline && reference(value) {
			continue
		}
		prefix := ""
		if key, _, inline := strings.Cut(arg, "="); inline {
			prefix = key + "="
		}
		s.ArgReferences[i] = prefix
		s.Entry.Args[i] = prefix
	}
	if entry.URL != "" && !reference(entry.URL) && secrets.RedactEndpoint(entry.URL) != entry.URL {
		s.URLReference = true
		s.URLShape = secrets.RedactEndpoint(entry.URL)
		s.Entry.URL = ""
	}
	return s
}

func reference(value string) bool {
	if !strings.HasPrefix(value, "${") || !strings.HasSuffix(value, "}") {
		return false
	}
	name := value[2 : len(value)-1]
	return name != "" && !strings.ContainsAny(name, "${} :-/\\")
}

func credentialMap(values map[string]string) (map[string]string, []string) {
	if values == nil {
		return nil, nil
	}
	refs := map[string]string{}
	keys := []string{}
	for key, value := range values {
		if reference(value) {
			refs[key] = value
		} else {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return refs, keys
}

func resolveCredentialMap(refs map[string]string, keys []string, live map[string]string) (map[string]string, error) {
	out := maps.Clone(refs)
	if out == nil && len(keys) > 0 {
		out = map[string]string{}
	}
	for _, key := range keys {
		value, ok := live[key]
		if !ok {
			return nil, fmt.Errorf("%w: MCP credential reference missing", agentcomposition.ErrUnavailable)
		}
		out[key] = value
	}
	return out, nil
}

func (s MCPSnapshot) Resolve(live config.PluginEntry) (config.PluginEntry, error) {
	entry := agentcomposition.Clone(s.Entry)
	if entry.Name != live.Name || entry.Command != live.Command && (s.AuthorizedCommand == "" || s.AuthorizedCommand != live.Command) || entry.Type != live.Type {
		return entry, fmt.Errorf("%w: MCP entry authorization changed", agentcomposition.ErrUnavailable)
	}
	var err error
	if entry.Env, err = resolveCredentialMap(entry.Env, s.EnvKeys, live.Env); err != nil {
		return entry, err
	}
	if entry.Headers, err = resolveCredentialMap(entry.Headers, s.HeaderKeys, live.Headers); err != nil {
		return entry, err
	}
	for index, prefix := range s.ArgReferences {
		if index < 0 || index >= len(entry.Args) || index >= len(live.Args) || !strings.HasPrefix(live.Args[index], prefix) {
			return entry, agentcomposition.ErrUnavailable
		}
		entry.Args[index] = live.Args[index]
	}
	if s.URLReference {
		if secrets.RedactEndpoint(live.URL) != s.URLShape {
			return entry, fmt.Errorf("%w: MCP endpoint changed", agentcomposition.ErrUnavailable)
		}
		entry.URL = live.URL
	}
	entry = entry.WithCredentialContextFrom(live)
	return entry, nil
}
