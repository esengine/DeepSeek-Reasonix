//go:build windows

package builtin

import "reasonix/internal/base/fileutil"

const pathSeparators = `/\`

func resolveExisting(p string) (string, error) { return fileutil.ResolveExistingPath(p) }
