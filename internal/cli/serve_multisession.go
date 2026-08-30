package cli

import (
	"context"
	"flag"

	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/remote/bootstrap"
	"reasonix/internal/serve"
)

func registerServeCapabilityFlags(fs *flag.FlagSet) {
	_ = fs.Bool("session-events", false, "tag session events and finish switched-away turns in background ("+bootstrap.ServeCapsToken+")")
	_ = fs.Bool("detached-heal", false, "retire background sessions after provider credential-channel repair")
}

func newServeBootstrap() (*serve.Broadcaster, *serve.SessionTagSink, *config.Config) {
	bc := serve.NewBroadcaster()
	cfg, _ := config.Load()
	return bc, serve.NewSessionTagSink(bc), cfg
}

func setupCLIMultiSessionProfile(ctx context.Context, model string, maxSteps int, preset string, tag *serve.SessionTagSink, leases *control.SessionLeaseKeeper) (*control.Controller, boot.Options, error) {
	migrateMCPConfigForCLIWorkspace()
	opts := cliProfileBuildOptions(model, maxSteps, false, tag, cliBuildOverrides{
		Preset: preset, OnSessionRecovered: cliSessionRecoveredHandler(leases),
	})
	ctrl, err := boot.Build(ctx, opts)
	return ctrl, opts, err
}

func newCLIMultiSessionServer(ctrl *control.Controller, bc *serve.Broadcaster, tag *serve.SessionTagSink, cfg config.ServeConfig, leases *control.SessionLeaseKeeper, buildOpts boot.Options) *serve.Server {
	tag.SetPath(ctrl.SessionPath())
	srv := serve.New(ctrl, bc, cfg)
	srv.SetControllerBuildOptions(buildOpts)
	srv.RegisterSessionTag(ctrl, tag)
	_ = srv.SetSessionLeases(leases)
	return srv
}
