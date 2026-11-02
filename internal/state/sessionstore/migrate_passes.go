package sessionstore

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// legacySourceEntries lists the legacy source directory before any pass runs.
// An explicit import records a directory it could not list as a typed skip; an
// implicit import gets a LegacySourceError, so an unreadable source is not read
// as an empty one. Only an absent directory — the normal fresh install — exits
// silently, and neither exit path can stamp the one-time marker as imported.
func legacySourceEntries(srcDir string, rep *LegacyReport) (entries []os.DirEntry, done bool, err error) {
	entries, err = os.ReadDir(srcDir)
	if err == nil {
		return entries, false, nil
	}
	skip := readSkip(srcDir, err)
	if rep != nil {
		rep.Skipped = append(rep.Skipped, skip)
		return nil, true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return nil, true, nil
	}
	return nil, true, &LegacySourceError{SkippedSession: skip, Err: err}
}

// LegacyReport lists what an import found in the source but could not bring
// over, one entry per file, so a caller can count it instead of losing it.
type LegacyReport struct{ Skipped []SkippedSession }

// ImportLegacySessionsFromExplicitDir imports sessions from a user-selected
// legacy directory and reports what it skipped. The user asked for this folder, so earlier
// runs' markers do not stand in for what is there now: every pass runs, and a
// session already present is skipped by its destination.
func ImportLegacySessionsFromExplicitDir(srcDir, globalDest string, projectDir func(string) string) (int, *LegacyReport, error) {
	marker := explicitLegacyImportMarker(srcDir)
	rep := &LegacyReport{}
	n, err := migrateLegacySessionsWithMarkers(srcDir, globalDest, marker, marker+".jsonl", projectDir, rep)
	return n, rep, err
}

// legacyEventBases names the sessions that have a v0.x event log, so the
// .jsonl-only pass can skip the ones event reconstruction already handles.
func legacyEventBases(srcDir string, entries []os.DirEntry) map[string]bool {
	bases := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() && strings.HasSuffix(name, ".events.jsonl") && !isNativeSessionEventLog(filepath.Join(srcDir, name)) {
			bases[strings.TrimSuffix(name, ".events.jsonl")] = true
		}
	}
	return bases
}

// rehomeFlatImport re-homes one legacy event-log session's flat leftover into
// its routed project directory. count is 1 when the session was imported this
// way; deferred reports that a live runtime owns the leftover, so the base must
// neither be imported beside it nor let the one-time marker claim the source.
func rehomeFlatImport(oldPath, newPath, destDir, base, summary string, srcInfo os.FileInfo) (count int, deferred bool) {
	if sameDirPath(destDir, filepath.Dir(oldPath)) {
		return 0, false
	}
	switch moveFlatImport(oldPath, newPath, srcInfo) {
	case flatImportMoved:
		recordImportedTitle(destDir, base, summary)
		return 1, false
	case flatImportDeferred:
		return 0, true
	}
	return 0, false
}

// importBakSessions recovers sessions whose .jsonl was lost but a .jsonl.bak
// remains. It returns the count imported and whether a copy failed; a failed
// recovery copy has to reach the caller so the one-time marker is withheld.
func importBakSessions(entries []os.DirEntry, srcDir, globalDest string, hasEvents map[string]bool, projectDir func(string) string, rep *LegacyReport) (int, bool) {
	imported := 0
	hadCopyFailure := false
	// .jsonl.bak recovery: when the .jsonl was lost but a backup remains.
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".jsonl.bak") {
			continue
		}
		base := strings.TrimSuffix(name, ".jsonl.bak")
		if hasEvents[base] {
			continue
		}
		jsonlName := base + ".jsonl"
		if existsUnder(srcDir, jsonlName) {
			continue // .jsonl exists, prefer it
		}
		meta := readLegacyMeta(srcDir, base)
		destDir := globalDest
		if projectDir != nil && meta.Workspace != "" && dirExists(meta.Workspace) {
			if d := projectDir(meta.Workspace); d != "" {
				destDir = d
			}
		}
		dest := filepath.Join(destDir, base+".jsonl")
		if _, err := os.Stat(dest); err == nil {
			continue
		}
		bakPath := filepath.Join(srcDir, name)
		if !isMessageFormat(bakPath) {
			rep.skip(bakPath, SkipUnreadableFormat)
			continue
		}
		srcInfo, _ := e.Info()
		if err := transformAndCopyJsonl(bakPath, dest); err != nil {
			rep.skipWrite(bakPath, err)
			hadCopyFailure = true
			continue
		}
		if srcInfo != nil {
			_ = os.Chtimes(dest, srcInfo.ModTime(), srcInfo.ModTime())
		}
		recordImportedTitle(destDir, base, meta.Summary)
		imported++
	}

	return imported, hadCopyFailure
}

// migrateProjectSubdirs recurses into subdirectories that look like project
// session dirs: the TS version nested project-scoped sessions under a workspace
// slug. It returns the count imported and whether a copy failed, for the same
// marker contract as the other passes.
func migrateProjectSubdirs(entries []os.DirEntry, srcDir, globalDest string, projectDir func(string) string, rep *LegacyReport) (int, bool) {
	imported := 0
	hadCopyFailure := false
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if e.Name() == "subagents" {
			continue
		}
		subDir := filepath.Join(srcDir, e.Name())
		subEntries, err := readDirUnder(srcDir, e.Name())
		if err != nil {
			rep.skipRead(subDir, err)
			continue
		}
		hasSessions := false
		for _, se := range subEntries {
			sn := se.Name()
			if !se.IsDir() && (strings.HasSuffix(sn, ".jsonl") || strings.HasSuffix(sn, ".events.jsonl")) {
				hasSessions = true
				break
			}
		}
		if !hasSessions {
			continue
		}
		n, failed := migrateSubDirectory(subDir, globalDest, projectDir, rep)
		imported += n
		hadCopyFailure = hadCopyFailure || failed
	}

	return imported, hadCopyFailure
}

// existsUnder reports whether name exists directly under dir, resolved through
// an os.Root so a symlink in dir cannot answer for a file outside it.
func existsUnder(dir, name string) bool {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return false
	}
	defer root.Close()
	_, err = root.Stat(name)
	return err == nil
}

func readDirUnder(dir, name string) ([]os.DirEntry, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.ReadDir(-1)
}

// sameDirPath reports whether two directory paths resolve to the same location.
func sameDirPath(a, b string) bool {
	ca, cb := filepath.Clean(a), filepath.Clean(b)
	if ca == cb {
		return true
	}
	if aa, err := filepath.Abs(ca); err == nil {
		if bb, err := filepath.Abs(cb); err == nil {
			return aa == bb
		}
	}
	return false
}
