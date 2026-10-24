package gitcmd

import (
	"context"
	"errors"
	"fmt"
)

var ErrDiscoveryChanged = errors.New("gitcmd: repository discovery changed")

// VerifyDiscovery checks the identity an unpinned shell command would discover.
// Verify alone checks the pinned identity, not forward .git/commondir pointers.
func (r Repo) VerifyDiscovery(ctx context.Context) error {
	current, err := Open(ctx, r.Dir)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrDiscoveryChanged, err)
	}
	if !sameFile(r.GitDir, current.GitDir) || !sameFile(r.CommonDir, current.CommonDir) || !sameFile(r.WorkTree, current.WorkTree) {
		return fmt.Errorf("%w: %s", ErrDiscoveryChanged, r.Dir)
	}
	return nil
}
