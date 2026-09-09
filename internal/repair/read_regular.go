package repair

import (
	"fmt"
	"io"
	"path/filepath"
)

// Read through a regular-file handle that permits atomic replacement on
// Windows. Pending-update readers run before acquiring the mutation lock;
// their handles must not prevent another owner from archiving the marker.
func readRepairRegularFile(path string) ([]byte, error) {
	file, err := openRepairRegularRead(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", filepath.Base(path))
	}
	return io.ReadAll(file)
}
