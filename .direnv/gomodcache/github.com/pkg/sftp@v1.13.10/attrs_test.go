package sftp

import (
	"os"
)

// ensure that attrs implemenst os.FileInfo
var _ os.FileInfo = new(fileInfo)
