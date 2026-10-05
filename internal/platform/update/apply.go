package update

import (
	"context"
	"fmt"
)

// VersionedInstaller publishes a release into its own directory and swaps the
// pointer last, so an install that dies partway leaves the running version
// untouched. A rollback lands the same way — going back is publishing an older
// directory, not undoing a newer one — so nothing here asks the direction.
type VersionedInstaller struct {
	Layout  Layout
	Staging string // update cache dir; the Windows helper copy is placed here
	Current string // running version, recorded in the macOS handoff transaction
	Line    Line   // which product line's members this archive carries
	App     Application
}

// Application is what an update replaces where the unit is a bundle rather than
// a set of files. Both halves are stated because neither is this process's to
// answer when the shell is not the application: the Go binary can be a resource
// inside the bundle being swapped, and what holds that bundle open is the
// framework that spawned it. LocalApplication is the case where it is.
type Application struct {
	Bundle string // the macOS .app to swap
	PID    int    // the process that must exit before it can be
}

// Install applies a verified artifact. It returns only if the handover failed:
// on success the caller is expected to shut down so the new build can take over.
func (v VersionedInstaller) Install(ctx context.Context, c Cached) error {
	if v.Layout.Root == "" || v.Layout.Executable == "" {
		return fmt.Errorf("update: cannot resolve where this build is installed")
	}
	if c.Kind == KindDeb {
		// A deb belongs to dpkg, and writing its files behind the package
		// manager leaves the two disagreeing about what is installed.
		return fmt.Errorf("update: a %s artifact installs through the system package manager", c.Kind)
	}
	if c.Path == "" || c.Version == "" {
		return fmt.Errorf("update: cached artifact is incomplete")
	}
	return v.apply(ctx, c)
}
