//go:build !windows

package sandbox

func singleProtectedStateRoot(protected []string) string {
	if len(protected) != 1 {
		return ""
	}
	return protected[0]
}
