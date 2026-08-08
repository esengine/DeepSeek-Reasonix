package tabhost

import (
	"context"
	"fmt"

	"reasonix/internal/boot"
	"reasonix/internal/control"
	"reasonix/internal/event"
)

// BootBuilderOptions configures DefaultBuilder (production path).
type BootBuilderOptions struct {
	// Model is the provider model ref; empty uses config default.
	Model string
	// StatsSource labels usage (e.g. "serve", "electron"). Empty disables stats.
	StatsSource string
	// RequireKey fails build when API key is missing. Electron/serve UI usually false.
	RequireKey bool
	// Context bounds boot; nil uses Background.
	Context context.Context
}

// DefaultBuilder returns a Builder that constructs a real control.Controller via boot.Build.
// Each tab gets WorkspaceRoot isolation and the tab's event sink.
func DefaultBuilder(opts BootBuilderOptions) Builder {
	return func(create CreateTabOpts, sink event.Sink) (control.SessionAPI, error) {
		ctx := opts.Context
		if ctx == nil {
			ctx = context.Background()
		}
		ctrl, err := boot.Build(ctx, boot.Options{
			Model:         opts.Model,
			RequireKey:    opts.RequireKey,
			Sink:          sink,
			WorkspaceRoot: create.WorkspaceRoot,
			StatsSource:   opts.StatsSource,
			// Interactive serve/electron: leave approval open until user answers.
		})
		if err != nil {
			return nil, fmt.Errorf("tabhost boot.Build: %w", err)
		}
		if create.SessionPath != "" {
			// Resume is caller-driven after Create; Builder only constructs fresh by default.
			// Production open-topic paths can Resume after CreateTab when needed.
			_ = create.SessionPath
		}
		ctrl.EnableInteractiveApproval()
		ctrl.EnsureSessionPath()
		return ctrl, nil
	}
}
