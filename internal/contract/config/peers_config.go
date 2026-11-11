package config

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// PeersConfig governs messages between sessions of one user on one machine.
type PeersConfig struct {
	// Enabled publishes this session to its peers and reads its own spool.
	// Only the user file may override the default.
	Enabled bool `toml:"enabled"`
	// Dispatch gives the model a tool that hands a task to a session in another of
	// the user's projects. Needs Enabled; the user file only.
	Dispatch bool `toml:"dispatch"`
	// ReleaseProjects lists projects whose sessions take a message from the user's
	// other sessions without the approval card. Only the asking is skipped: what
	// the message starts runs under that session's own approvals. User file only.
	ReleaseProjects []string `toml:"release_projects"`
	// MaxDispatchDepth is how many sessions deep a dispatch may chain: 1 means a
	// dispatched session cannot dispatch further, 0 removes the ceiling. It stops a
	// task from handing itself on without end. User file only.
	MaxDispatchDepth int `toml:"max_dispatch_depth"`
	// MaxDispatchedSessions is how many sessions one session may have open from its
	// dispatches at once, 0 for no ceiling. Each is a kernel and a lease, so it
	// bounds a loop that keeps opening panes. User file only.
	MaxDispatchedSessions int `toml:"max_dispatched_sessions"`
	// OpenNested lets open_projects add folders inside the asking session's own
	// workspace without the approval card. A folder outside it still asks. Off
	// unless the user turns it on; user file only.
	OpenNested bool `toml:"open_nested"`
}

func defaultPeers() PeersConfig {
	return PeersConfig{Enabled: true, Dispatch: true, MaxDispatchDepth: DefaultMaxDispatchDepth, MaxDispatchedSessions: DefaultMaxDispatchedSessions}
}

// Defaults of the two ceilings above.
const (
	DefaultMaxDispatchDepth      = 1
	DefaultMaxDispatchedSessions = 8
)

// PeersEnabled reports whether this session takes part in messages between
// sessions.
func (c *Config) PeersEnabled() bool { return c != nil && c.Peers.Enabled }

// PeersDispatch reports whether the model may dispatch a task to another project.
func (c *Config) PeersDispatch() bool { return c.PeersEnabled() && c.Peers.Dispatch }

// PeersOpenNested reports whether open_projects may add folders inside the
// asking session's workspace without a card.
func (c *Config) PeersOpenNested() bool { return c != nil && c.Peers.OpenNested }

// PeersMaxDispatchDepth is the user's depth ceiling for chained dispatches, 0 for none.
func (c *Config) PeersMaxDispatchDepth() int {
	if c == nil {
		return DefaultMaxDispatchDepth
	}
	return max(c.Peers.MaxDispatchDepth, 0)
}

// PeersMaxDispatchedSessions is the user's ceiling on sessions one session keeps
// open from its dispatches, 0 for none.
func (c *Config) PeersMaxDispatchedSessions() int {
	if c == nil {
		return DefaultMaxDispatchedSessions
	}
	return max(c.Peers.MaxDispatchedSessions, 0)
}

// PeersReleasedProject reports whether root is a project the user lets take
// messages from their other sessions without a card.
func (c *Config) PeersReleasedProject(root string) bool {
	if c == nil || root == "" {
		return false
	}
	want := cleanProjectPath(root)
	return slices.ContainsFunc(c.Peers.ReleaseProjects, func(p string) bool { return cleanProjectPath(p) == want })
}

func cleanProjectPath(p string) string {
	p = filepath.Clean(p)
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		p = resolved
	}
	return p
}

func renderServeAndPeers(b *strings.Builder, c *Config) {
	renderServeSection(b, c)
	renderPeersSection(b, c)
	renderModsSection(b, c)
}

// Explicit opt-outs must survive unrelated saves and later default changes.
func renderPeersSection(b *strings.Builder, c *Config) {
	p := c.Peers
	customCeilings := p.MaxDispatchDepth != DefaultMaxDispatchDepth || p.MaxDispatchedSessions != DefaultMaxDispatchedSessions
	if p.Enabled && p.Dispatch && len(p.ReleaseProjects) == 0 && !customCeilings && !p.OpenNested {
		return
	}
	b.WriteString("[peers]\n")
	fmt.Fprintf(b, "enabled = %v   # messages between your sessions on this machine; user file only\n", p.Enabled)
	fmt.Fprintf(b, "dispatch = %v   # the model may hand a task to a session in another of your projects; needs enabled\n", p.Dispatch)
	if len(p.ReleaseProjects) > 0 {
		fmt.Fprintf(b, "release_projects = %s   # projects that take messages from your other sessions without a card\n", renderStringArray(p.ReleaseProjects))
	}
	if p.OpenNested {
		b.WriteString("open_nested = true   # open_projects adds folders inside the asking session's workspace without a card; user file only\n")
	}
	if customCeilings {
		fmt.Fprintf(b, "max_dispatch_depth = %d   # how many sessions deep a dispatch may chain; 0 for no ceiling\n", p.MaxDispatchDepth)
		fmt.Fprintf(b, "max_dispatched_sessions = %d   # sessions one session may keep open from its dispatches; 0 for no ceiling\n", p.MaxDispatchedSessions)
	}
	b.WriteString("\n")
}
