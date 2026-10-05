package config

import (
	"bytes"
	"strings"

	"github.com/BurntSushi/toml"
)

// renderPassthroughTable writes back a table this build decodes but does not
// act on. The user config is shared with installs of the 1.x line, which still
// run the IM bot gateway from [bot]; a full-file rewrite that dropped the table
// would silently switch that gateway off and erase its allowlists.
func renderPassthroughTable(b *strings.Builder, name string, table map[string]any) {
	if len(table) == 0 {
		return
	}
	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	enc.Indent = ""
	if err := enc.Encode(map[string]any{name: table}); err != nil {
		return
	}
	b.Write(buf.Bytes())
	b.WriteString("\n")
}

// passthroughCredentialEnvNames collects the credential variables a [bot]
// table names, at any depth, by the gateway schema's two credential keys. The
// gateway does not run here, but its secrets still stay out of the
// environments model-controlled subprocesses inherit.
func passthroughCredentialEnvNames(v any) []string {
	var out []string
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			if s, ok := e.(string); ok && (k == "app_secret_env" || k == "token_env") {
				out = append(out, s)
				continue
			}
			out = append(out, passthroughCredentialEnvNames(e)...)
		}
	case []map[string]any:
		for _, e := range x {
			out = append(out, passthroughCredentialEnvNames(e)...)
		}
	case []any:
		for _, e := range x {
			out = append(out, passthroughCredentialEnvNames(e)...)
		}
	}
	return out
}
