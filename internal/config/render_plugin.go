package config

import (
	"fmt"
	"strings"
)

// renderPluginPolicy writes the per-server policy fields both render scopes
// share. They live together so a policy added to one scope cannot be forgotten
// in the other, which would silently drop a user's setting on the next save.
func renderPluginPolicy(b *strings.Builder, pl PluginEntry) {
	if c := strings.TrimSpace(pl.Concurrency); c != "" {
		fmt.Fprintf(b, "concurrency = %q\n", c)
	}
	if pl.AutoStart != nil {
		fmt.Fprintf(b, "auto_start = %v\n", *pl.AutoStart)
	}
}
