//go:build !windows && !linux

package serve

import "errors"

func platformReasonixSessionHolderProcess(int) bool { return false }

func platformTerminateReasonixSessionHolder(int) error {
	return errors.New("force reclaim is unsupported on this platform")
}
