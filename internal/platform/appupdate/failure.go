package appupdate

import (
	"errors"

	"reasonix/internal/platform/update"
)

// Why a move did not happen, as the dotted code a panel explains. The step that
// failed attaches it, because only that step knows: each code is a different
// thing for the user to do next — wait and retry, report, free disk space, or
// install once by hand.
const (
	FailCatalog    = "update.catalog_unreachable"
	FailNoPackage  = "update.no_package"
	FailDownload   = "update.download_failed"
	FailVerify     = "update.signature_invalid"
	FailDisk       = "update.disk"
	FailInstaller  = "update.installer_failed"
	FailNotApplied = "update.not_applied"
	FailUnknown    = "update.failed"
)

// Why an offered chunked update was abandoned for the full package. None is a
// failure of the move; each names what would let the next one take the delta.
const (
	DeltaNotSwappable = "update.delta.not_swappable"
	DeltaFetchFailed  = "update.delta.fetch_failed"
	DeltaTimedOut     = "update.delta.timed_out"
	DeltaMismatch     = "update.delta.mismatch"
	DeltaDisk         = "update.delta.disk"
	DeltaFailed       = "update.delta.failed"
)

type stepError struct {
	code string
	err  error
}

func (e *stepError) Error() string { return e.err.Error() }
func (e *stepError) Unwrap() error { return e.err }

func failAs(code string, err error) error {
	if err == nil {
		return nil
	}
	return &stepError{code: code, err: err}
}

// failureCode projects the class a step attached. A download reports its own
// through update's sentinels, which is where fetch, signature and disk part.
func failureCode(err error) string {
	var step *stepError
	switch {
	case errors.As(err, &step):
		return step.code
	case errors.Is(err, update.ErrVerify):
		return FailVerify
	case errors.Is(err, update.ErrStore):
		return FailDisk
	case errors.Is(err, update.ErrFetch):
		return FailDownload
	}
	return FailUnknown
}
