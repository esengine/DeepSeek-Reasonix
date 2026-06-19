package agent

import (
	"os"
	"path/filepath"
	"strings"
)

func init() {
	registerMigratePass(MigratePass{
		Name: "subdir",
		Run:  runSubdirPass,
	})
}

// runSubdirPass recurses into subdirectories that look like project session
// dirs (e.g. Users_Yuki_git_polytone-audio-engine/ under ~/.reasonix/sessions/).
// The TS version nested project-scoped sessions under a workspace slug.
func runSubdirPass(ctx *MigrateContext) (int, bool, error) {
	imported := 0
	for _, e := range ctx.Entries {
		if !e.IsDir() {
			continue
		}
		if e.Name() == "subagents" {
			continue
		}
		subDir := filepath.Join(ctx.SrcDir, e.Name())
		subEntries, err := os.ReadDir(subDir)
		if err != nil {
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
		n, err := migrateSubDirectory(subDir, ctx.GlobalDest, ctx.ProjectDir)
		if err != nil {
			continue
		}
		imported += n
	}
	return imported, false, nil
}
