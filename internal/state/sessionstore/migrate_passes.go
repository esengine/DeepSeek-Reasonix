package sessionstore

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LegacyReport lists what an import found in the source but could not bring
// over, one entry per file, so a caller can count it instead of losing it.
type LegacyReport struct{ Skipped []error }

func (r *LegacyReport) skip(name, why string) {
	if r != nil {
		r.Skipped = append(r.Skipped, fmt.Errorf("%s: %s", name, why))
	}
}

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

// importBakSessions recovers sessions whose .jsonl was lost but a .jsonl.bak
// remains.
func importBakSessions(entries []os.DirEntry, srcDir, globalDest string, hasEvents map[string]bool, projectDir func(string) string, rep *LegacyReport) int {
	imported := 0
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
			rep.skip(name, "not a session format this version reads")
			continue
		}
		srcInfo, _ := e.Info()
		if err := transformAndCopyJsonl(bakPath, dest); err != nil {
			rep.skip(name, err.Error())
			continue
		}
		if srcInfo != nil {
			_ = os.Chtimes(dest, srcInfo.ModTime(), srcInfo.ModTime())
		}
		recordImportedTitle(destDir, base, meta.Summary)
		imported++
	}

	return imported
}

// migrateProjectSubdirs recurses into subdirectories that look like project
// session dirs: the TS version nested project-scoped sessions under a workspace
// slug.
func migrateProjectSubdirs(entries []os.DirEntry, srcDir, globalDest string, projectDir func(string) string, rep *LegacyReport) int {
	imported := 0
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
			rep.skip(e.Name(), err.Error())
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
		n, err := migrateSubDirectory(subDir, globalDest, projectDir, rep)
		if err != nil {
			rep.skip(e.Name(), err.Error())
			continue
		}
		imported += n
	}

	return imported
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
