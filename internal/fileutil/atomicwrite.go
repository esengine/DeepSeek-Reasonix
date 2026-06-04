package fileutil

import (
	"errors"
	"fmt"
	"os"
)

// ReplaceFile renames tmp onto dest, falling back to a copy when the rename
// fails — Windows encryption-software filter drivers report a cross-device link
// (EXDEV) for a same-dir rename.
//
// The error returned preserves both the original rename failure (often an
// opaque EXDEV / "Access is denied" / share-violation surfaced mid-rename by
// an AV scanner) and the copy-onto failure that followed, joined with
// errors.Join so the operator sees the full failure shape. The rename
// error is the leading one because the copy was the recovery attempt; the
// copy error is appended so the user can see *why* the recovery also failed
// (e.g. tmp deleted mid-retry, dest is read-only, disk full).
//
// A nil rename error but failing copy is impossible by construction (the
// rename only fires inside `if err != nil`), so the joined pair is only
// returned when both halves failed.
func ReplaceFile(tmp, dest string) error {
	if err := os.Rename(tmp, dest); err != nil {
		if copyErr := copyOnto(tmp, dest); copyErr != nil {
			return errors.Join(
				fmt.Errorf("rename %s -> %s: %w", tmp, dest, err),
				fmt.Errorf("copy fallback %s -> %s: %w", tmp, dest, copyErr),
			)
		}
	}
	return nil
}

// copyOnto overwrites dest with the contents of tmp, preserving tmp's mode and
// removing tmp on success. Used as the cross-device fallback when os.Rename
// refuses a same-directory rename (Windows encryption filter drivers, some
// AV-on-write hooks, and network mounts that report EXDEV on what is really
// a single filesystem). The dest's existing mode is *not* honoured — a 0600
// tmp must not widen to 0644 because os.WriteFile keeps dest's pre-existing
// mode, so we explicitly re-apply tmp's mode after the overwrite.
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
	// Chmod is best-effort: on Windows, the file may be locked by an AV scanner
	// mid-rename; the in-memory mode is already correct (WriteFile used tmp's
	// perms), so a follow-up Chmod is just a belt-and-suspenders for the next
	// process that reads the file.
	if err := os.Chmod(dest, info.Mode().Perm()); err != nil && !isReadOnlyFS(err) {
		// A non-fatal Chmod failure on a read-only mount or a locked file is
		// fine — the WriteFile above already wrote the data with the right
		// perms on Unix (info.Mode().Perm() is the open-mode bit) and the
		// file is already usable. Surface other Chmod errors because they
		// hint at a real permission problem.
		return err
	}
	if err := os.Remove(tmp); err != nil && !os.IsNotExist(err) {
		// A leftover tmp on disk is harmless (next run will overwrite it), but
		// a missing tmp after WriteFile is the normal success path, not an
		// error. Anything else (cross-device, permission) is worth surfacing
		// so the caller can decide to retry the cleanup.
		return err
	}
	return nil
}

// isReadOnlyFS reports whether err looks like an attempt to mutate a
// read-only filesystem. Chmod/Rename against a read-only mount on Linux
// returns EROFS; on Windows, attempts against a locked file surface as
// ERROR_ACCESS_DENIED / ERROR_WRITE_PROTECT, but those are also returned for
// ordinary file locks, so this is intentionally a best-effort signal — the
// caller should treat a Chmod failure on a Chmod-with-best-effort path as
// non-fatal anyway.
func isReadOnlyFS(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return contains(msg, "read-only") || contains(msg, "EROFS") || contains(msg, "write protected")
}

// contains is a tiny case-sensitive substring check, kept local to avoid
// pulling strings.Contains into the dependency-free fileutil API surface.
func contains(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
