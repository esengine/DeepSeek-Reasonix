//go:build !windows

package cli

import "os"

func onCygwinPty(*os.File) bool { return false }
