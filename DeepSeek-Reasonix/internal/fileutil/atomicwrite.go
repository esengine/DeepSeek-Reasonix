package fileutil

import "os"

// ReplaceFile renames tmp onto dest, falling back to a copy when the rename
// fails — Windows encryption-software filter drivers report a cross-device link
// (EXDEV) for a same-dir rename. The rename error surfaces only if the copy also fails.
func ReplaceFile(tmp, dest string) error {
	if err := os.Rename(tmp, dest); err != nil {
		if copyErr := copyOnto(tmp, dest); copyErr != nil {
			return err
		}
	}
	return nil
}

func copyOnto(tmp, dest string) error {
	info, err := os.Stat(tmp)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(tmp)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dest, data, info.Mode().Perm()); err != nil {
		return err
	}
	// WriteFile keeps an existing dest's mode, so re-apply tmp's mode to match
	// what the rename would have done (a 0600 config tmp must not widen to 0644).
	_ = os.Chmod(dest, info.Mode().Perm())
	_ = os.Remove(tmp)
	return nil
}
