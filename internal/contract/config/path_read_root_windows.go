package config

import (
	"errors"

	"golang.org/x/sys/windows"
)

func configReadRootPlatformUnavailable(err error) bool {
	return errors.Is(err, windows.ERROR_INVALID_FUNCTION)
}
