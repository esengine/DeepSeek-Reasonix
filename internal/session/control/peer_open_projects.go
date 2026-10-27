package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/tool"
)

// OpenProjectsApprovalTool is the card for adding folders to the project list.
// It is the tool's own name, so the card finds the scope the tool declares.
const OpenProjectsApprovalTool = "open_projects"

// openProjectsMax bounds one card: the user reads every path before answering.
const openProjectsMax = 12

// Each is an identity the open_projects tool reports; none is matched by its words.
var (
	ErrOpenProjectsEmpty    = errors.New("open_projects needs at least one absolute folder path")
	ErrOpenProjectsTooMany  = errors.New("open_projects takes at most 12 folders at a time; ask again with the rest")
	ErrOpenProjectsRelative = errors.New("open_projects takes absolute paths only")
	ErrOpenProjectsDeclined = errors.New("the user declined to open those folders as projects; nothing was added")
	ErrOpenProjectsNoAnswer = errors.New("nobody answered the request to open those folders; nothing was added")
	ErrOpenProjectsPlan     = errors.New("projects are not opened while plan mode is on; ask again once the plan is approved")
)

// ProjectOpener adds folders to the user's project list as trusted. The host
// that owns the project list supplies it; the controller has already had the
// user's answer by the time it is called.
type ProjectOpener func(ctx context.Context, from *Controller, paths []string) ([]tool.OpenedProject, error)

// SetProjectOpener gives the open_projects tool somewhere to go; a controller
// without one (a terminal session, a test) answers ErrDispatchUnavailable.
func (c *Controller) SetProjectOpener(o ProjectOpener) {
	if o == nil {
		c.dispatch.opener.Store(nil)
		return
	}
	c.dispatch.opener.Store(&o)
}

// OpenProjects asks the user whether to add the folders to their project list,
// and on a yes has the host add and trust exactly the ones the card named. The
// card is fresh: no mode, grant or hook answers it for the user.
func (c *Controller) OpenProjects(ctx context.Context, paths []string) ([]tool.OpenedProject, error) {
	if !config.LoadForEdit(config.UserConfigPath()).PeersDispatch() {
		return nil, ErrDispatchDisabled
	}
	if c.PlanMode() {
		return nil, ErrOpenProjectsPlan
	}
	hook := c.dispatch.opener.Load()
	if hook == nil {
		return nil, ErrDispatchUnavailable
	}
	clean, err := cleanProjectPaths(paths)
	if err != nil {
		return nil, err
	}
	args, _ := json.Marshal(map[string]any{"paths": clean})
	subject := clean[0]
	if len(clean) > 1 {
		subject = fmt.Sprintf("%s (+%d)", clean[0], len(clean)-1)
	}
	reply, err := c.requestApprovalDecision(ctx, approvalRequest{tool: OpenProjectsApprovalTool, subject: subject, args: args, fresh: true})
	switch {
	case err != nil && ctx.Err() != nil:
		return nil, ctx.Err()
	case err != nil:
		return nil, ErrOpenProjectsNoAnswer
	case !reply.allow:
		return nil, ErrOpenProjectsDeclined
	}
	return (*hook)(ctx, c, clean)
}

// cleanProjectPaths is the list the card shows and the host receives: absolute,
// cleaned, each once. What the user read is exactly what is acted on.
func cleanProjectPaths(paths []string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !filepath.IsAbs(p) {
			return nil, ErrOpenProjectsRelative
		}
		p = filepath.Clean(p)
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	switch {
	case len(out) == 0:
		return nil, ErrOpenProjectsEmpty
	case len(out) > openProjectsMax:
		return nil, ErrOpenProjectsTooMany
	}
	return out, nil
}

// TrustFolder records the user's trust in root, having had their answer on a
// card that named it. A folder they declined earlier stays declined.
func (c *Controller) TrustFolder(root string) error {
	store := config.NewProjectGrantStore(c.posture.Home)
	if trust, _ := store.Trust(root); trust == config.WorkspaceTrustDeclined {
		return ErrDispatchUntrusted
	}
	return store.SetTrust(root, config.WorkspaceTrusted)
}

func (d peerDirectory) OpenProjects(ctx context.Context, paths []string) ([]tool.OpenedProject, error) {
	got, err := d.c.OpenProjects(ctx, paths)
	if err != nil {
		return nil, fmtPeerError(err)
	}
	return got, nil
}
