//go:build plan9

package repair

import "os"

func openRepairRegularRead(path string) (*os.File, error) {
	return os.Open(path)
}
