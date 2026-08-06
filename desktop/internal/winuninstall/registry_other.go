//go:build !windows

package winuninstall

func Reconcile(string, string) (bool, error) { return false, nil }
