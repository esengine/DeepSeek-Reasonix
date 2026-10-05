// Package appupdate is where owning the Studio application meets replacing it.
package appupdate

import (
	"context"

	"reasonix/internal/platform/repair"
	"reasonix/internal/platform/update"
)

// ApplicationOwner owns the running desktop application: it can bring it to a
// state where its own files may be replaced, and it knows how to start what
// takes its place. Nothing here names a shell, and whether the two halves
// belong to one process is a platform's answer — a waiting helper starts the
// successor on Windows and macOS, the departing application does on Linux.
type ApplicationOwner interface {
	// PrepareForUpdate releases what would keep the application's own files
	// from being replaced.
	PrepareForUpdate(ctx context.Context) error
	// RelaunchAfterUpdate starts what replaced it.
	RelaunchAfterUpdate(ctx context.Context) error
	// EndApplication ends it, so what replaced it can take its place. Third
	// act and not second: where a waiting helper starts the successor, ending
	// is what lets it. It does not normally return.
	EndApplication(ctx context.Context)
}

// Capability is what a hub serves updates through. It is declared here rather
// than taken from serve because a kernel package may not import a frontend, and
// Go's interfaces meet structurally: the hub's port is satisfied without the
// two packages naming each other.
type Capability interface {
	AcknowledgeLaunchHealth() error
	StartInstall(install update.Install, target string) error
	CommitInstall(target string) error
	InstallProgress() update.Progress
}

// Options is what a shell states about the application it owns. Which build is
// running and where it lives is not here: the hub carries that as one declared
// Install and hands it to each call, so there is no second copy to disagree.
type Options struct {
	Owner ApplicationOwner
	// Running is the version this launch booted as, read once at startup.
	Running string
	// Line is the product line whose artifacts this application installs.
	Line update.Line
	// Application is what a swap replaces where the unit is a bundle. A shell
	// that is its own executable fills it from update.LocalApplication.
	Application update.Application
}

// New returns the capability a hub serves updates through, and nil where
// nothing owns the application. That nil is the gate: a kernel with no owner
// registers no update routes at all, so `reasonix serve` does not acquire the
// power to replace a desktop application by sharing an engine with one. The
// return type is an interface so the nil survives the assignment.
func New(opts Options) Capability {
	if opts.Owner == nil {
		return nil
	}
	// Read now, before a concurrent update can rewrite it: what this launch may
	// retire is the transaction it booted from, and nothing later can tell that
	// apart from one written since.
	c := &capability{
		opts:    opts,
		witness: repair.CaptureUpdateHealth(opts.Running),
	}
	// This launch is the only witness to whether the last move landed, and a
	// pin naming another build holds nothing; both are settled before a panel
	// can ask.
	if dir, err := update.CacheDir(); err == nil {
		if p, ok := settleLastMove(dir, opts.Running); ok {
			c.install.progress = p
		}
	}
	_ = update.ReleaseStalePin(opts.Running)
	return c
}

type capability struct {
	opts    Options
	witness *repair.UpdateHealthWitness
	install installState
}

// AcknowledgeLaunchHealth retires the update this launch booted from, and only
// that one. The caller passes nothing because it may name nothing: the swap is
// performed by a process that cannot judge it, and a shell that could hand over
// a transaction id could commit somebody else's. A launch that did not boot
// from an update has nothing to retire and says so by succeeding.
func (c *capability) AcknowledgeLaunchHealth() error {
	return c.witness.Acknowledge(c.opts.Running)
}
