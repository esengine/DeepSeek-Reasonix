package control

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"reasonix/internal/contract/config"
)

var (
	// ErrPeerProjectInvalid is a release project that is not an absolute path.
	ErrPeerProjectInvalid = errors.New("a released project must be an absolute folder path")
	// ErrPeerCeilingInvalid is a dispatch ceiling below zero; 0 is how a ceiling is removed.
	ErrPeerCeilingInvalid = errors.New("a dispatch ceiling is a count of 0 or more")
	// ErrPeerRememberDenied is a "remember" attached to a denial; only an approval
	// can release a project.
	ErrPeerRememberDenied = errors.New("only an approval can be remembered")
	// ErrPeerSettingsUnwritable is a user file that could not be read back or saved.
	ErrPeerSettingsUnwritable = errors.New("could not save the messaging settings")
)

func settingsWriteError(err error) error {
	if err == nil || errors.Is(err, ErrPeerProjectInvalid) || errors.Is(err, ErrPeerCeilingInvalid) {
		return err
	}
	return fmt.Errorf("%w: %w", ErrPeerSettingsUnwritable, err)
}

// PeerReleased is a project that takes messages from the user's other sessions
// without a card.
type PeerReleased struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

// PeersSettings is the user file's [peers] table. Enabled and Dispatch move the
// tool block, so they are applied between turns: Pending is true from the save
// until then. Available is false where the session has no spool to open.
type PeersSettings struct {
	Enabled         bool           `json:"enabled"`
	Dispatch        bool           `json:"dispatch"`
	ReleaseProjects []PeerReleased `json:"release_projects"`
	// MaxDispatchDepth and MaxDispatchedSessions are the user's ceilings on chained
	// and simultaneous dispatches; 0 removes a ceiling.
	MaxDispatchDepth      int `json:"max_dispatch_depth"`
	MaxDispatchedSessions int `json:"max_dispatched_sessions"`
	// OpenNested is the user's standing yes for open_projects to add folders inside
	// the session's own workspace without a card; it takes effect at the next call.
	OpenNested bool   `json:"open_nested"`
	Path       string `json:"path"`
	Applies    string `json:"applies"`
	Pending    bool   `json:"pending"`
	Available  bool   `json:"available"`
}

// PeersPatch is a partial update: a nil field is left as it is, and a non-nil
// ReleaseProjects replaces the whole list.
type PeersPatch struct {
	Enabled         *bool
	Dispatch        *bool
	ReleaseProjects *[]string
	// Negative values are refused; 0 removes the ceiling.
	MaxDispatchDepth      *int
	MaxDispatchedSessions *int
	OpenNested            *bool
}

// PeerRemember says what a decision on a held message also writes down.
type PeerRemember string

const (
	PeerRememberNone    PeerRemember = ""
	PeerRememberProject PeerRemember = "project"
)

// PeersSettings reads the switches from the user file and says whether this
// session already runs them.
func (c *Controller) PeersSettings() PeersSettings {
	return c.peersSettingsFrom(config.UserPeers())
}

func (c *Controller) peersSettingsFrom(cfg config.PeersConfig) PeersSettings {
	p := c.peers
	out := PeersSettings{
		Enabled: cfg.Enabled, Dispatch: cfg.Enabled && cfg.Dispatch, Path: config.UserConfigPath(),
		MaxDispatchDepth: max(cfg.MaxDispatchDepth, 0), MaxDispatchedSessions: max(cfg.MaxDispatchedSessions, 0), OpenNested: cfg.OpenNested,
		Applies: "next_turn", Available: p != nil && p.root != "", ReleaseProjects: []PeerReleased{},
	}
	for _, path := range cfg.ReleaseProjects {
		out.ReleaseProjects = append(out.ReleaseProjects, PeerReleased{Path: path, Name: filepath.Base(path)})
	}
	if out.Available {
		on, dispatch := p.state()
		out.Pending = on != out.Enabled || (on && dispatch != out.Dispatch)
	}
	return out
}

// SavePeersSettings writes the patch to the user file. The switches reach the
// session now if it is between turns, else at the start of the next one.
func (c *Controller) SavePeersSettings(patch PeersPatch) (PeersSettings, error) {
	cfg, err := config.UpdateUserPeers(func(p *config.PeersConfig) error {
		if patch.ReleaseProjects != nil {
			for _, path := range *patch.ReleaseProjects {
				if !filepath.IsAbs(path) {
					return fmt.Errorf("%w: %q", ErrPeerProjectInvalid, path)
				}
			}
			p.ReleaseProjects = slices.Clone(*patch.ReleaseProjects)
		}
		if patch.Enabled != nil {
			p.Enabled = *patch.Enabled
		}
		if patch.Dispatch != nil {
			p.Dispatch = *patch.Dispatch
		}
		for _, n := range []*int{patch.MaxDispatchDepth, patch.MaxDispatchedSessions} {
			if n != nil && *n < 0 {
				return fmt.Errorf("%w: %d", ErrPeerCeilingInvalid, *n)
			}
		}
		if patch.MaxDispatchDepth != nil {
			p.MaxDispatchDepth = *patch.MaxDispatchDepth
		}
		if patch.MaxDispatchedSessions != nil {
			p.MaxDispatchedSessions = *patch.MaxDispatchedSessions
		}
		if patch.OpenNested != nil {
			p.OpenNested = *patch.OpenNested
		}
		return nil
	})
	if err != nil {
		return c.PeersSettings(), settingsWriteError(err)
	}
	if c.Running() {
		c.peers.stale.Store(true)
	} else {
		c.syncPeerSwitches()
	}
	return c.peersSettingsFrom(cfg), nil
}

// applyPeerSwitches runs at the start of a turn and acts only on a save that
// arrived while the previous turn was still running.
func (c *Controller) applyPeerSwitches() {
	if p := c.peers; p != nil && p.stale.CompareAndSwap(true, false) {
		c.syncPeerSwitches()
	}
}

// syncPeerSwitches brings the session to what the user file says. It is called
// only between turns: the peer tools join or leave the tool block, which costs
// one cold prefix, and a session that never turns them on never pays it.
func (c *Controller) syncPeerSwitches() {
	p := c.peers
	if p == nil || p.root == "" {
		return
	}
	cfg := config.UserPeers()
	on, dispatch := p.state()
	if on == cfg.Enabled && (!on || dispatch == cfg.Dispatch) {
		return
	}
	if cfg.Enabled && !on {
		if !c.openPeers() {
			return
		}
	}
	if !cfg.Enabled && on {
		p.shutdown()
	}
	p.setDispatch(cfg.Enabled && cfg.Dispatch)
	if p.tools != nil {
		p.tools(cfg.Enabled, cfg.Enabled && cfg.Dispatch)
	}
}

func (c *Controller) openPeers() bool {
	p := c.peers
	p.mu.Lock()
	ok := p.open()
	stop := p.stop
	p.mu.Unlock()
	if !ok {
		return false
	}
	go c.peerLoop(stop)
	c.rebindPeers()
	return true
}

func (p *peerMail) state() (on, dispatch bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.dir.Load() != nil, p.dispatch
}

func (p *peerMail) setDispatch(v bool) {
	p.mu.Lock()
	p.dispatch = v
	p.mu.Unlock()
}

// rememberPeerProject adds this session's project to the released list. It runs
// only on the user's own click on an approval, never from a message.
func (c *Controller) rememberPeerProject() error {
	root := c.WorkspaceRoot()
	if !filepath.IsAbs(root) {
		return fmt.Errorf("%w: %q", ErrPeerProjectInvalid, root)
	}
	_, err := config.UpdateUserPeers(func(p *config.PeersConfig) error {
		if !slices.Contains(p.ReleaseProjects, root) {
			p.ReleaseProjects = append(p.ReleaseProjects, root)
		}
		return nil
	})
	return settingsWriteError(err)
}
