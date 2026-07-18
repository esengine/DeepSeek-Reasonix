//go:build linux

package cli

import (
	"errors"
	"os"
)

var remoteServeEffectiveUID = os.Geteuid

func productionRemoteServePrivilegeGuard() error {
	if remoteServeEffectiveUID() == 0 {
		return errors.New("Reasonix Remote daemon refuses to run as root")
	}
	return nil
}
