package agent

import (
	"os"
	"path/filepath"
	"strings"
)

func init() {
	registerMigratePass(MigratePass{
		Name:   "jsonl",
		Marker: legacyJsonlPassMarker,
		Run:    runJsonlPass,
	})
}

// runJsonlPass imports message-format .jsonl files without a .events.jsonl
// counterpart. These are sessions the TS version wrote directly in the v1+
// format (ACP, desktop, subagent, and later-version chat sessions). It also
// recovers from .jsonl.bak files when the .jsonl was lost.
func runJsonlPass(ctx *MigrateContext) (int, bool, error) {
	imported, failed := importJsonlSessions(ctx.Entries, ctx.SrcDir, ctx.GlobalDest, ctx.HasEvents, ctx.ProjectDir)

	// .jsonl.bak recovery: when the .jsonl was lost but a backup remains.
	for _, e := range ctx.Entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".jsonl.bak") {
			continue
		}
		base := strings.TrimSuffix(name, ".jsonl.bak")
		if ctx.HasEvents[base] {
			continue
		}
		jsonlName := base + ".jsonl"
		if _, err := os.Stat(filepath.Join(ctx.SrcDir, jsonlName)); err == nil {
			continue // .jsonl exists, prefer it
		}
		meta := readLegacyMeta(ctx.SrcDir, base)
		destDir := ctx.GlobalDest
		if ctx.ProjectDir != nil && meta.Workspace != "" && dirExists(meta.Workspace) {
			if d := ctx.ProjectDir(meta.Workspace); d != "" {
				destDir = d
			}
		}
		dest := filepath.Join(destDir, base+".jsonl")
		if _, err := os.Stat(dest); err == nil {
			continue
		}
		bakPath := filepath.Join(ctx.SrcDir, name)
		if !isMessageFormat(bakPath) {
			continue
		}
		srcInfo, _ := e.Info()
		if err := transformAndCopyJsonl(bakPath, dest); err != nil {
			continue
		}
		if srcInfo != nil {
			_ = os.Chtimes(dest, srcInfo.ModTime(), srcInfo.ModTime())
		}
		recordImportedTitle(destDir, base, meta.Summary)
		imported++
	}

	return imported, failed, nil
}
