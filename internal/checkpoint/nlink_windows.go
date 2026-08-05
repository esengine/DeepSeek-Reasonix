//go:build windows

package checkpoint

import "os"

// The portable os.FileInfo contract does not expose Windows link counts.
// Returning one avoids rejecting ordinary files while the path/symlink guards
// still protect restore targets on Windows.
func fileNlink(_ os.FileInfo) uint64 { return 1 }
