package agent

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

func init() {
	registerMigratePass(MigratePass{
		Name:      "rehome",
		Marker:    "",    // marker is taken from the pipeline's ctx.Marker at runtime
		AlwaysRun: true,  // runs even when markers exist (downgrade recovery)
		Run:       runRehomePass,
	})
}

// runRehomePass copies project-scoped sessions that were written into the flat
// global dir AFTER the one-time routing pass already ran — the signature of a
// user who downgraded to a pre-routing build (which writes every session to
// the flat dir regardless of workspace) and then upgraded again (#4666).
//
// It is deliberately conservative:
//   - Only sessions whose mtime is newer than the marker (the last migration
//     watermark) are considered, so a session the user imported and then
//     deleted is never resurrected.
//   - Only sessions that explicitly name a still-existing workspace — via a v1+
//     branch-meta sidecar with scope=project, or a v0.x .meta.json — are moved.
//   - It never modifies the source files.
//
// The marker mtime is advanced to now after a successful scan so the next boot
// does not re-walk the same files.
func runRehomePass(ctx *MigrateContext) (int, bool, error) {
	if !ctx.AllPassesDone || ctx.ProjectDir == nil {
		return 0, false, nil
	}
	markerPath := filepath.Join(ctx.GlobalDest, ctx.Marker)
	markerInfo, err := os.Stat(markerPath)
	if err != nil {
		return 0, false, nil // no watermark to compare against — full passes own this dir
	}
	watermark := markerInfo.ModTime()

	imported := 0
	hadCopyFailure := false
	for _, e := range ctx.Entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".jsonl") ||
			strings.HasSuffix(name, ".events.jsonl") || strings.HasSuffix(name, ".jsonl.bak") {
			continue
		}
		base := strings.TrimSuffix(name, ".jsonl")
		if strings.HasPrefix(base, "subagent-") {
			continue // surfaced only through their parent session
		}
		info, ierr := e.Info()
		if ierr != nil || !info.ModTime().After(watermark) {
			continue // written before the last migration — not a downgrade straggler
		}
		srcPath := filepath.Join(ctx.SrcDir, name)
		if !isMessageFormat(srcPath) {
			continue
		}
		destDir, summary := strandedSessionDestDir(ctx.SrcDir, srcPath, base, ctx.ProjectDir)
		if destDir == "" || sameDirPath(destDir, ctx.GlobalDest) {
			continue // global session, or no live workspace — leave it in the flat dir
		}
		dest := filepath.Join(destDir, name)
		if _, err := os.Stat(dest); err == nil {
			if err := copySubagentArtifacts(ctx.SrcDir, destDir, base); err != nil {
				hadCopyFailure = true
			}
			continue // already routed on a previous boot
		}
		if err := transformAndCopyJsonl(srcPath, dest); err != nil {
			hadCopyFailure = true
			continue
		}
		_ = os.Chtimes(dest, info.ModTime(), info.ModTime()) // preserve resume ordering
		copyBranchMetaSidecar(srcPath, dest)
		if err := copySubagentArtifacts(ctx.SrcDir, destDir, base); err != nil {
			hadCopyFailure = true
		}
		recordImportedTitle(destDir, base, summary)
		imported++
	}
	// Advance the watermark so the next boot starts from here, unless a matched
	// project session failed to copy and still needs a retry.
	if !hadCopyFailure {
		now := time.Now()
		_ = os.Chtimes(markerPath, now, now)
	}
	return imported, hadCopyFailure, nil
}

// strandedSessionDestDir resolves the per-project session dir a flat-dir session
// belongs to, preferring the v1+ branch-meta sidecar and falling back to the
// v0.x .meta.json. It returns "" when the session is global or names a workspace
// that no longer exists on disk. The second return is the display summary, if any.
func strandedSessionDestDir(srcDir, srcPath, base string, projectDir func(string) string) (string, string) {
	if meta, ok, err := LoadBranchMeta(srcPath); err == nil && ok {
		if meta.DefaultScope() == "project" && meta.WorkspaceRoot != "" && dirExists(meta.WorkspaceRoot) {
			if d := projectDir(meta.WorkspaceRoot); d != "" {
				return d, strings.TrimSpace(meta.TopicTitle)
			}
		}
		// A branch sidecar that explicitly marks the session global wins over a
		// stale v0.x sidecar of the same name.
		if meta.Scope != "" {
			return "", ""
		}
	}
	legacy := readLegacyMeta(srcDir, base)
	if legacy.Workspace != "" && dirExists(legacy.Workspace) {
		if d := projectDir(legacy.Workspace); d != "" {
			return d, legacy.Summary
		}
	}
	return "", ""
}
