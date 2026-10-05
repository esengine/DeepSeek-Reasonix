//go:build !darwin

package repair

func platformRepairPathUnicodeNormalized(path string) string {
	return path
}
