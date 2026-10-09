//go:build !windows

package serve

import "context"

func pickLocalFolder(context.Context, string) (string, error) {
	return "", errFolderPickerUnsupported
}

func pickLocalFolderAvailable() bool { return false }
