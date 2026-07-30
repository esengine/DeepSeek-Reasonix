//go:build !darwin && !windows

package repair

func platformRepairPathCaseInsensitive(string) bool {
	return false
}
