package agent

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"reasonix/internal/store"
)

const (
	sessionDAGRotationWait        = 5 * time.Second
	sessionDAGRotationMarkerStale = 10 * time.Second
	sessionDAGRotationPoll        = 20 * time.Millisecond
)

// sessionDAGRotateBeforeReplace runs after a rotation raised its marker and
// before it reads late appends. Tests use it to land an unlocked append in
// that window; production leaves it nil.
var sessionDAGRotateBeforeReplace func(sessionPath string)

// appendSessionDAGEntriesUnlocked appends one batch without the session file
// lock. It terminates a torn tail first so the batch starts on its own line,
// and re-appends when the log was rotated underneath the write, since a
// rotation only carries bytes it can still see in the old file.
func appendSessionDAGEntriesUnlocked(sessionPath string, entries []sessionDAGEntry) (int64, error) {
	path := store.SessionEventLog(sessionPath)
	if path == "" || len(entries) == 0 {
		return 0, fmt.Errorf("nothing to append to session event log %q", path)
	}
	data, err := encodeSessionDAGEntries(entries, time.Now().UTC())
	if err != nil {
		return 0, err
	}
	for range 3 {
		size, moved, err := appendUnlockedOnce(sessionPath, path, data)
		if err != nil || !moved {
			return size, err
		}
	}
	return 0, fmt.Errorf("session log %s kept rotating during an unlocked append", path)
}

func appendUnlockedOnce(sessionPath, path string, data []byte) (size int64, moved bool, err error) {
	terminated, err := logEndsWithNewline(path)
	if err != nil {
		return 0, false, err
	}
	if !terminated {
		data = append([]byte{'\n'}, data...)
	}
	written, err := writeUnlockedBatch(path, data)
	if err != nil {
		return 0, false, err
	}
	// The handle is closed before waiting: a rotation publishing under us
	// renames over the log, which Windows refuses while it is open here.
	waitForSessionLogRotation(sessionPath)
	current, err := os.Stat(path)
	if err != nil {
		return 0, false, err
	}
	if !os.SameFile(written, current) {
		return 0, true, nil
	}
	return written.Size(), false, nil
}

func writeUnlockedBatch(path string, data []byte) (os.FileInfo, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open session event log: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("append session entries: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return nil, err
	}
	written, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return written, f.Close()
}

func logEndsWithNewline(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return false, err
	}
	if info.Size() == 0 {
		return true, nil
	}
	var last [1]byte
	if _, err := f.ReadAt(last[:], info.Size()-1); err != nil {
		return false, err
	}
	return last[0] == '\n', nil
}

// appendLateLinesToStaged copies the complete lines appended to the live log
// after the bytes a rotation consumed onto the staged replacement, so an
// unlocked append that landed before the rotation marker is not dropped by
// the atomic replace. A partial last line belongs to a writer that will
// re-append once the marker clears.
func appendLateLinesToStaged(path string, consumed int64, staged string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Seek(consumed, io.SeekStart); err != nil {
		return err
	}
	late, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	if cut := bytes.LastIndexByte(late, '\n'); cut < 0 {
		return nil
	} else {
		late = late[:cut+1]
	}
	out, err := os.OpenFile(staged, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("carry late appends across rotation: %w", err)
	}
	defer out.Close()
	if _, err := out.Write(late); err != nil {
		return fmt.Errorf("carry late appends across rotation: %w", err)
	}
	if err := out.Sync(); err != nil {
		return err
	}
	slog.Info("session: carried late appends across log rotation", "path", path, "bytes", len(late))
	return nil
}

// waitForSessionLogRotation blocks while a rotation of the log is between its
// marker and its publish, so the SameFile check that follows an unlocked
// append sees the outcome of that rotation. A marker left by a crashed
// rotation is ignored once it is old enough.
func waitForSessionLogRotation(sessionPath string) {
	marker := store.SessionEventLogRotating(sessionPath)
	deadline := time.Now().Add(sessionDAGRotationWait)
	for time.Now().Before(deadline) {
		info, err := os.Stat(marker)
		if err != nil || time.Since(info.ModTime()) > sessionDAGRotationMarkerStale {
			return
		}
		time.Sleep(sessionDAGRotationPoll)
	}
	slog.Warn("session: rotation marker did not clear; trusting the current log", "path", sessionPath)
}
