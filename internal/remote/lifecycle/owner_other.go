//go:build !linux

package lifecycle

import "os"

func currentUID() int { return -1 }

func ownerUID(os.FileInfo) (int, bool) { return 0, false }
