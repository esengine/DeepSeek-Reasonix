//go:build !darwin && !windows

package sessioncatalog

func platformCatalogPathIdentity(path string) string {
	return path
}
