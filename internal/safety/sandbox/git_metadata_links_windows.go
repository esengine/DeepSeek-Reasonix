package sandbox

import "os"

// linkCount is unused on Windows, where no sandbox confines writes.
func linkCount(os.FileInfo) uint64 { return 1 }
