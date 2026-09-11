package cli

import (
	"fmt"
	"os"

	"reasonix/internal/control"
	"reasonix/internal/i18n"
)

// cliFailure reports err on stderr and yields the process exit code, so a
// command can end a failed step in one statement.
func cliFailure(err error) int {
	fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
	return 1
}

// cliTakeoverFailure also returns a claimed session lease to its previous
// owner, which every failure after a takeover binding must do.
func cliTakeoverFailure(binding *cliTakeoverBinding, leases *control.SessionLeaseKeeper, manager *cliTakeoverManager, err error) int {
	_ = cliReturnFailedTakeover(binding, leases, manager)
	return cliFailure(err)
}
