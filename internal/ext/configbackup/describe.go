package configbackup

import (
	"encoding/json"
	"slices"
	"strings"

	"reasonix/internal/base/secrets"
	"reasonix/internal/contract/config"
)

// maxShownContent bounds the file body a preview carries for reading.
const maxShownContent = 16 << 10

// describe is the line a person decides on: the command that will run, the
// endpoint a key goes to, the file that will be written.
func describe(it Item) (string, []PathRef) {
	switch it.Kind {
	case KindProvider:
		var p config.ProviderEntry
		if decodeTOML(it.Data, &p) == nil {
			return strings.TrimSpace(p.Kind + " " + secrets.RedactEndpoint(firstNonEmpty(p.RequestURL, p.BaseURL, p.ChatURL))), nil
		}
	case KindMCP:
		var p config.PluginEntry
		if decodeTOML(it.Data, &p) == nil {
			if strings.TrimSpace(p.Command) == "" {
				return secrets.RedactEndpoint(p.URL), nil
			}
			args := secrets.RedactArgs(p.Args)
			line := strings.TrimSpace(p.Command + " " + strings.Join(args, " "))
			return line, absPaths(append([]string{p.Command}, args...)...)
		}
	case KindHook:
		var h hookData
		if json.Unmarshal(it.Data, &h) == nil {
			return h.Hook.Command, append(commandPaths(h.Hook.Command), absPaths(h.Hook.Cwd, h.Hook.ContextFile)...)
		}
	case KindStatusline:
		var s statuslineData
		if json.Unmarshal(it.Data, &s) == nil {
			return s.Command, commandPaths(s.Command)
		}
	case KindPlugin:
		var p pluginData
		if json.Unmarshal(it.Data, &p) == nil {
			return strings.TrimSpace(p.Source + " " + p.Version), absPaths(p.Source)
		}
	case KindGeneral:
		var g generalData
		if json.Unmarshal(it.Data, &g) == nil {
			return g.DefaultModel, nil
		}
	case KindSecret:
		return "••••••", nil
	case KindMemory:
		var m memoryData
		if json.Unmarshal(it.Data, &m) == nil {
			return m.Path, nil
		}
	}
	return "", nil
}

// details is everything else the item will write that changes what runs or
// where data goes, so consent is given to the whole of it, not its first line.
func details(it Item) ([]string, string) {
	switch it.Kind {
	case KindProvider:
		var p config.ProviderEntry
		if decodeTOML(it.Data, &p) != nil {
			return nil, ""
		}
		out := labelled("base_url", stripURLSecrets(p.BaseURL), "request_url", stripURLSecrets(p.RequestURL), "chat_url", stripURLSecrets(p.ChatURL),
			"models_url", stripURLSecrets(p.ModelsURL), "balance_url", stripURLSecrets(p.BalanceURL), "api_key_env", p.APIKeyEnv)
		return append(out, pairs("header", p.Headers)...), ""
	case KindMCP:
		var p config.PluginEntry
		if decodeTOML(it.Data, &p) != nil {
			return nil, ""
		}
		out := labelled("type", p.Type, "url", secrets.RedactEndpoint(p.URL))
		return append(append(out, pairs("env", p.Env)...), pairs("header", p.Headers)...), ""
	case KindHook:
		var h hookData
		if json.Unmarshal(it.Data, &h) != nil {
			return nil, ""
		}
		out := labelled("event", h.Event, "match", h.Hook.Match, "cwd", h.Hook.Cwd, "contextFile", h.Hook.ContextFile)
		return append(out, pairs("env", h.Hook.Env)...), ""
	case KindMemory:
		var m memoryData
		if json.Unmarshal(it.Data, &m) != nil {
			return nil, ""
		}
		body := string(m.Data)
		if len(body) > maxShownContent {
			body = body[:maxShownContent]
		}
		return nil, body
	}
	return nil, ""
}

func labelled(kv ...string) []string {
	var out []string
	for i := 0; i+1 < len(kv); i += 2 {
		if strings.TrimSpace(kv[i+1]) != "" {
			out = append(out, kv[i]+": "+kv[i+1])
		}
	}
	return out
}

func pairs(label string, m map[string]string) []string {
	m = secrets.RedactConfigMap(m)
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, label+": "+k+"="+m[k])
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
