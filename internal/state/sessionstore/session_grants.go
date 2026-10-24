package sessionstore

import (
	"os"
	"slices"
)

// MaxSessionGrants bounds what a sidecar can hand back as grants.
const MaxSessionGrants = 256

// LoadSessionGrants returns the answers remembered for the conversation at
// path; a missing or unreadable sidecar remembers nothing.
func LoadSessionGrants(path string) []string {
	if path == "" {
		return nil
	}
	m, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		return nil
	}
	out := make([]string, 0, len(m.SessionGrants))
	for _, g := range m.SessionGrants {
		if g == "" || len(g) > 512 || len(out) >= MaxSessionGrants {
			continue
		}
		out = append(out, g)
	}
	return out
}

// SaveSessionGrants records the answers for the conversation at path, without
// touching its activity time. A conversation that was never saved has no
// sidecar to put them in and is left alone; the next save carries them.
func SaveSessionGrants(path string, grants []string) error {
	if path == "" {
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	grants = slices.Clone(grants)
	slices.Sort(grants)
	if len(grants) > MaxSessionGrants {
		grants = grants[:MaxSessionGrants]
	}
	if slices.Equal(LoadSessionGrants(path), grants) {
		return nil
	}
	return UpdateBranchMeta(path, false, func(m *BranchMeta) error {
		m.SessionGrants = grants
		return nil
	})
}
