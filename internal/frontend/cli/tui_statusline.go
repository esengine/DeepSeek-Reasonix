package cli

import (
	"context"
	"strings"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/ext/hook"
)

// statuslineTimeout bounds the footer's wait on a user script between turns.
const statuslineTimeout = 2 * time.Second

// statuslineRunner runs [statusline].command with the footer's context on
// stdin and keeps the first line it prints; any failure reads as no line.
func statuslineRunner(cfg *config.Config) func(context.Context, string) string {
	if cfg == nil || strings.TrimSpace(cfg.Statusline.Command) == "" {
		return nil
	}
	command := cfg.Statusline.Command
	spawn := hook.WithoutCwdExeSearch(hook.DefaultSpawner)
	return func(ctx context.Context, stdin string) string {
		res := spawn(ctx, hook.SpawnInput{Command: command, Stdin: stdin + "\n", Timeout: statuslineTimeout})
		out := strings.TrimSpace(res.Stdout)
		if i := strings.IndexByte(out, '\n'); i >= 0 {
			out = strings.TrimSpace(out[:i])
		}
		return out
	}
}
