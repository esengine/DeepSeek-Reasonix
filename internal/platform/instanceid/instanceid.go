// Package instanceid names the Studio instance a data home may have. One home
// is one Studio: two windows over one home are two writers for every session
// file the sidebar lists, and two writers on one transcript is what forks it
// into an endless run of recovery copies. Which shell holds the window is not
// part of the identity — the home is.
package instanceid

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"strings"

	"reasonix/internal/contract/config"
)

// Prefix is the bundle's own identifier, so a Studio launch never answers the
// 1.x desktop's lock: two applications, two windows.
const Prefix = "io.reasonix.studio"

// Current names the instance this process's configured home may have.
func Current() string { return For(config.ReasonixHomeDir()) }

// For names the instance that owns one data home, canonicalized rather than
// taken as spelled: an installed build, a portable one and a canary all write
// the same sessions and have to find each other, and a home reached through a
// link is the one it points at. A different REASONIX_HOME is a different
// instance, which is what makes a second home the way to run two.
func For(home string) string {
	root := strings.TrimSpace(home)
	if root == "" {
		return Prefix
	}
	// The lease path canonicalizer, so a home below a link — or one that does
	// not exist yet — still names the physical directory.
	if marker := sessionstore.CanonicalSessionPath(filepath.Join(root, ".reasonix-home.identity")); marker != "" {
		root = filepath.Dir(marker)
	}
	sum := sha256.Sum256([]byte(filepath.Clean(root)))
	return Prefix + "." + hex.EncodeToString(sum[:8])
}
