package agent

import (
	"os"
	"path/filepath"
	"strings"
)

func init() {
	registerMigratePass(MigratePass{
		Name: "events",
		Run:  runEventsPass,
	})
}

// runEventsPass imports event-log sessions (*.events.jsonl). When a same-named
// .jsonl exists in the source with a modification time >= the event log's, the
// .jsonl is preferred (it is already in the native message format).
func runEventsPass(ctx *MigrateContext) (int, bool, error) {
	imported := 0
	for _, e := range ctx.Entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".events.jsonl") {
			continue
		}
		base := strings.TrimSuffix(name, ".events.jsonl")
		meta := readLegacyMeta(ctx.SrcDir, base)
		destDir := ctx.GlobalDest
		if ctx.ProjectDir != nil && meta.Workspace != "" && dirExists(meta.Workspace) {
			if d := ctx.ProjectDir(meta.Workspace); d != "" {
				destDir = d
			}
		}
		dest := filepath.Join(destDir, base+".jsonl")
		if _, err := os.Stat(dest); err == nil {
			continue // already imported, or a v1+ session of the same name
		}
		eventsInfo, _ := e.Info()
		if destDir != ctx.GlobalDest && moveFlatImport(filepath.Join(ctx.GlobalDest, base+".jsonl"), dest, eventsInfo) {
			recordImportedTitle(destDir, base, meta.Summary)
			imported++
			continue
		}

		// If a .jsonl sidecar exists and is >= the event log's mtime, copy it
		// directly — the TS version wrote the native format alongside or after
		// the event log, so the .jsonl is the canonical record.
		jsonlPath := filepath.Join(ctx.SrcDir, base+".jsonl")
		if jsonlInfo, err := os.Stat(jsonlPath); err == nil && isMessageFormat(jsonlPath) {
			if eventsInfo == nil || !jsonlInfo.ModTime().Before(eventsInfo.ModTime()) {
				if err := transformAndCopyJsonl(jsonlPath, dest); err == nil {
					if eventsInfo != nil {
						_ = os.Chtimes(dest, eventsInfo.ModTime(), eventsInfo.ModTime())
					}
					recordImportedTitle(destDir, base, meta.Summary)
					imported++
					continue
				}
			}
		}

		msgs, err := reconstructSession(filepath.Join(ctx.SrcDir, name))
		if err != nil || len(msgs) == 0 {
			continue
		}
		s := &Session{Messages: msgs}
		if err := s.Save(dest); err != nil {
			return imported, false, err
		}
		if eventsInfo != nil {
			_ = os.Chtimes(dest, eventsInfo.ModTime(), eventsInfo.ModTime()) // preserve resume ordering
		}
		recordImportedTitle(destDir, base, meta.Summary)
		imported++
	}
	return imported, false, nil
}
