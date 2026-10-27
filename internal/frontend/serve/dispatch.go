package serve

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

// dispatchBook remembers which pane a sender opened in which project, so a
// second task to the same project goes to the session already waiting instead
// of opening another.
type dispatchBook struct {
	mu       sync.Mutex
	sessions map[dispatchKey]string
	// pending holds a pane opened in a project the user has not decided about yet;
	// it shows its own trust card, and the next dispatch reuses it.
	pending map[dispatchKey]string
	open    func(context.Context, OpenRequest, *agentcomposition.Binding) (*Runtime, error)
}

type dispatchKey struct{ from, root string }

func (b *dispatchBook) get(from, root string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sessions[dispatchKey{from, root}]
}

func (b *dispatchBook) targets(from string) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []string
	for _, m := range []map[dispatchKey]string{b.sessions, b.pending} {
		for k, id := range m {
			if k.from == from {
				out = append(out, id)
			}
		}
	}
	return out
}

func (b *dispatchBook) set(from, root, id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.sessions == nil {
		b.sessions = map[dispatchKey]string{}
	}
	k := dispatchKey{from, root}
	b.sessions[k] = id
	delete(b.pending, k)
}

func (b *dispatchBook) getPending(from, root string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.pending[dispatchKey{from, root}]
}

func (b *dispatchBook) setPending(from, root, id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pending == nil {
		b.pending = map[dispatchKey]string{}
	}
	b.pending[dispatchKey{from, root}] = id
}

// dispatcherFor is what a pane's controller calls to hand a task to another of
// the user's projects. The controller has already checked the user's switch.
func (h *Hub) dispatcherFor(from *Runtime) control.ProjectDispatcher {
	return func(ctx context.Context, project, text string) (control.DispatchResult, error) {
		return h.dispatchTo(ctx, from, project, text)
	}
}

func (h *Hub) dispatchTo(ctx context.Context, from *Runtime, project, text string) (control.DispatchResult, error) {
	root, err := resolveDispatchProject(project, Workspaces())
	if err != nil {
		return control.DispatchResult{}, err
	}
	sender, ok := from.Server.Controller().(*control.Controller)
	if !ok {
		return control.DispatchResult{}, control.ErrDispatchNotLocal
	}
	if sameFolder(root, sender.WorkspaceRoot()) {
		return control.DispatchResult{}, projectError(control.ErrDispatchSameProject, root)
	}
	target := h.runtimeByID(h.dispatch.get(from.ID, root))
	var secret []byte
	if target == nil {
		if sender.TrustOfWorkspace(root) == config.WorkspaceTrustDeclined {
			return control.DispatchResult{}, projectError(control.ErrDispatchUntrusted, root)
		}
		waiting := h.runtimeByID(h.dispatch.getPending(from.ID, root)) != nil
		if limit := config.LoadForEdit(config.UserConfigPath()).PeersMaxDispatchedSessions(); !waiting && limit > 0 && h.liveDispatched(from.ID) >= limit {
			return control.DispatchResult{}, control.ErrDispatchLimit
		}
		if secret, err = newPairSecret(); err != nil {
			return control.DispatchResult{}, fmt.Errorf("%w: %w", control.ErrDispatchFailed, err)
		}
		if target, err = h.openDispatchSession(ctx, from, sender, root, secret); err != nil {
			return control.DispatchResult{}, err
		}
	}
	targetCtrl, ok := target.Server.Controller().(*control.Controller)
	if !ok {
		return control.DispatchResult{}, control.ErrDispatchNotLocal
	}
	key := targetCtrl.PeerKey()
	if key == "" {
		return control.DispatchResult{}, fmt.Errorf("%w: the new session did not publish itself", control.ErrDispatchFailed)
	}
	sender.NoteDispatchedSession(key, secret)
	receipt, err := sender.SendPeerMessage(key, text)
	if err != nil {
		return control.DispatchResult{}, err
	}
	return control.DispatchResult{Project: root, Key: key, ID: receipt.ID}, nil
}

// newPairSecret makes the secret one dispatch pairing signs its messages with.
// It lives in the two controllers' memory and is never written to the spool.
func newPairSecret() ([]byte, error) {
	secret := make([]byte, 32)
	_, err := rand.Read(secret)
	return secret, err
}

// openDispatchSession opens a fresh pane in root, or takes back the one an
// earlier dispatch left waiting for the user's answer about the folder. A folder
// the user declined is not opened for someone else's task. A project in the
// user's own list that nobody has decided about is trusted by being named; one
// that cannot be trusted whole keeps its pane open on the trust card, task unsent.
func (h *Hub) openDispatchSession(ctx context.Context, from *Runtime, sender *control.Controller, root string, secret []byte) (*Runtime, error) {
	binding := sender.CompositionBinding()
	if binding != nil && binding.Workspace != "" && binding.Workspace != root {
		return nil, fmt.Errorf("%w: %w", control.ErrDispatchFailed, agentcomposition.Unavailable(binding.Version.Definition.ID, "A bound composition cannot dispatch into another workspace."))
	}
	open := h.dispatch.open
	if open == nil {
		open = h.openBound
	}
	grant := sender.DispatchGrant()
	target := h.runtimeByID(h.dispatch.getPending(from.ID, root))
	if target == nil {
		var err error
		if target, err = open(ctx, OpenRequest{Root: root, Model: grant.Model}, binding); err != nil {
			return nil, fmt.Errorf("%w: %w", control.ErrDispatchFailed, err)
		}
	}
	ctrl, ok := target.Server.Controller().(*control.Controller)
	if !ok {
		_ = h.Close(target.ID)
		return nil, control.ErrDispatchNotLocal
	}
	switch ctrl.WorkspaceTrust() {
	case config.WorkspaceTrusted:
	case config.WorkspaceTrustDeclined:
		_ = h.Close(target.ID)
		return nil, projectError(control.ErrDispatchUntrusted, root)
	default:
		if err := ctrl.DecideWorkspaceTrust(config.WorkspaceTrusted); err != nil {
			h.dispatch.setPending(from.ID, root, target.ID)
			return nil, projectError(control.ErrDispatchAwaitingTrust, root)
		}
	}
	ctrl.AdoptSessionGrant(grant)
	ctrl.MarkOpenedByDispatch(sender.PeerKey(), secret, sender.DispatchDepth()+1)
	ctrl.SetFreshSessionPath(sessionstore.NewSessionPath(SessionDirFor(root), "dispatch"))
	h.dispatch.set(from.ID, root, target.ID)
	return target, nil
}

// projectError names the project a refusal is about; errors.Is still finds the
// sentinel, which is the identity.
func projectError(sentinel error, root string) error {
	return fmt.Errorf("%w (project %q)", sentinel, filepath.Base(root))
}

// liveDispatched counts the panes from opened for its dispatches, waiting for a
// trust answer or not, that are still open; a pane the user closed no longer counts against the ceiling.
func (h *Hub) liveDispatched(from string) int {
	n := 0
	for _, id := range h.dispatch.targets(from) {
		if h.runtimeByID(id) != nil {
			n++
		}
	}
	return n
}

func (h *Hub) runtimeByID(id string) *Runtime {
	if id == "" {
		return nil
	}
	for _, rt := range h.localRuntimes() {
		if rt.ID == id {
			return rt
		}
	}
	return nil
}

// resolveDispatchProject maps what the model wrote onto one of the user's own
// projects: an exact path, else a folder name. Two projects with one name, or
// none, is refused; a folder outside the list never resolves.
func resolveDispatchProject(name string, projects []string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", control.ErrDispatchUnknownProject
	}
	if filepath.IsAbs(name) {
		want := filepath.Clean(name)
		for _, p := range projects {
			if filepath.Clean(p) == want && dirExists(p) {
				return p, nil
			}
		}
		return "", control.ErrDispatchUnknownProject
	}
	var found []string
	for _, p := range projects {
		if strings.EqualFold(filepath.Base(p), name) && dirExists(p) {
			found = append(found, p)
		}
	}
	switch len(found) {
	case 0:
		return "", control.ErrDispatchUnknownProject
	case 1:
		return found[0], nil
	}
	return "", control.ErrDispatchAmbiguous
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}
