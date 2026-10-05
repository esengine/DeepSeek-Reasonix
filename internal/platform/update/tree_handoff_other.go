//go:build !windows

package update

import "errors"

// ErrTreeHandoffUnsupported is a platform whose update channel replaces a whole
// install (a package, a bundle) rather than swapping a staged tree.
var ErrTreeHandoffUnsupported = errors.New("update: this platform does not swap staged trees")

// StartTreeHandoff is Windows-only; see ErrTreeHandoffUnsupported.
func StartTreeHandoff(TreeHandoff, string) error { return ErrTreeHandoffUnsupported }

func waitForExit([]int)              {}
func relaunch(string) error          { return nil }
func afterTreeInstalled(TreeHandoff) {}

// TreeHandoffSupported reports that this platform does not swap staged trees.
func TreeHandoffSupported() bool { return false }
