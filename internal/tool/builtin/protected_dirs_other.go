//go:build !darwin

package builtin

func isProtectedDir(string) bool { return false }
