package serve

import (
	"errors"
	"os"
	"path/filepath"

	"reasonix/internal/contract/config"
	"reasonix/internal/session/team"
	"reasonix/internal/state/teamroster"
)

var errTeamFolderUntrusted = errors.New("open this folder as an ordinary workspace and approve its trust before assigning it to an agent")

func (t *hubTeam) prepareTeamFolder(r teamroster.Roster, seat team.SeatRef, root string) (bool, error) {
	trust, err := config.NewProjectGrantStore(config.RootsForHome("").Home()).Trust(root)
	if err != nil {
		return false, err
	}
	if trust == config.WorkspaceTrusted {
		return false, os.MkdirAll(root, 0o700)
	}
	a, ok := r.Agent(seat.Agent)
	if !ok {
		return false, teamroster.ErrUnknownAgent
	}
	private := a.Folder == ""
	if g, ok := r.Group(seat.Group); ok && g.Folder != "" {
		private = false
	}
	if !private || trust == config.WorkspaceTrustDeclined || filepath.Clean(root) != filepath.Join(t.opts.Workspace, a.ID) {
		return false, errTeamFolderUntrusted
	}
	if err := os.MkdirAll(t.opts.Workspace, 0o700); err != nil {
		return false, err
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		if errors.Is(err, os.ErrExist) {
			return false, errTeamFolderUntrusted
		}
		return false, err
	}
	return true, nil
}
