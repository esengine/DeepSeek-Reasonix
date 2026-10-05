package observation

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/state/trustedstate"
)

// Policy decides what a snapshot observes. It is host-owned: its revision and
// digest travel with every snapshot taken under it.
type Policy struct {
	Revision   int         `json:"revision"`
	Excluded   []Exclusion `json:"excluded"`
	MaxEntries int         `json:"max_entries"`
}

// Exclusion drops every directory with base name Name, or the one directory
// at absolute Path, with the reason it is outside the observation domain.
type Exclusion struct {
	Name   string `json:"name,omitempty"`
	Path   string `json:"path,omitempty"`
	Reason string `json:"reason"`
}

// DefaultPolicy excludes version-control stores and the host's own state
// roots, which the host writes while it observes. Dependency trees stay
// observed: tests read them, so a write there can change a verdict.
func DefaultPolicy(hostRoots ...string) Policy {
	var ex []Exclusion
	for _, s := range fileutil.VCSStores() {
		ex = append(ex, Exclusion{Name: s.Dir, Reason: s.Name + " rewrites its own store on reads; VCS state is an open gap of the trust domain"})
	}
	for _, root := range hostRoots {
		if abs, err := filepath.Abs(root); err == nil && strings.TrimSpace(root) != "" {
			ex = append(ex, Exclusion{Path: filepath.Clean(abs), Reason: "host state: the host writes it while observing"})
		}
	}
	return Policy{Revision: 1, Excluded: ex, MaxEntries: 250_000}
}

// Digest identifies the policy's exact content.
func (p Policy) Digest() string {
	b, _ := json.Marshal(p)
	return string(trustedstate.DigestOf(b))
}

func (p Policy) excludes(name, full string) bool {
	return slices.ContainsFunc(p.Excluded, func(e Exclusion) bool {
		return (e.Name != "" && e.Name == name) || (e.Path != "" && e.Path == full)
	})
}
