package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/config"
	"reasonix/internal/i18n"
	"reasonix/internal/session"
	"reasonix/internal/store"
)

// migrateLegacySessionsOnStartup destructively converts every pre-v4 session on
// this machine into the canonical sessions-v4 store, then deletes the old
// sources. Migration copies the source byte-for-byte into the v4 target's
// legacy/ evidence directory before the source is removed, so nothing is lost.
//
// This is a deliberately self-contained, temporary bridge: the v4 store is now
// the only session format the CLI reads or writes, and this file can be deleted
// once every installation has upgraded. Keep all of it in one place so removal
// is a single-file change.
//
// It is safe to run on every startup: a session whose v4 target already exists
// is skipped (MigrateLegacy/ImportStoredPreview reuse the published target),
// and a source still held by another process is left untouched.
func migrateLegacySessionsOnStartup(report func(imported int)) {
	ctx := context.Background()
	imported := 0
	dirs := legacySessionDirs()
	for _, legacyDir := range dirs {
		target := legacyStoreV4Root(legacyDir)
		if target == "" {
			continue
		}
		switch filepath.Base(legacyDir) {
		case "sessions":
			imported += migrateLegacyTranscripts(ctx, legacyDir, target)
		case "sessions-v3":
			imported += migrateLegacyStores(ctx, legacyDir, target)
		}
	}
	// Sessions imported before the source times were recorded sort by their
	// import time. Repair them once from the preserved legacy/ evidence so
	// resume keeps the real chronology.
	seen := map[string]bool{}
	for _, legacyDir := range dirs {
		target := legacyStoreV4Root(legacyDir)
		if target == "" || seen[target] {
			continue
		}
		seen[target] = true
		_, _ = session.RepairMigratedActivity(ctx, target)
	}
	if report != nil && imported > 0 {
		report(imported)
	}
}

// reportLegacyMigration surfaces the one-time startup migration on stderr.
func reportLegacyMigration(imported int) {
	fmt.Fprintln(os.Stderr, fmt.Sprintf(i18n.M.MigratedLegacySessionsFmt, imported))
}

// legacySessionDirs lists every legacy JSONL and v3 store directory on this
// machine: the current workspace, the global store, and every other known
// project. Missing directories are included and skipped by the callers.
func legacySessionDirs() []string {
	var dirs []string
	add := func(dir string) {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			return
		}
		dirs = append(dirs, filepath.Clean(dir))
	}
	add(config.SessionDir())
	if home := config.ReasonixHomeDir(); home != "" {
		add(filepath.Join(home, "sessions-v3"))
	}
	projects := filepath.Join(config.MemoryUserDir(), "projects")
	if entries, err := os.ReadDir(projects); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			base := filepath.Join(projects, entry.Name())
			add(filepath.Join(base, "sessions"))
			add(filepath.Join(base, "sessions-v3"))
		}
	}
	// De-duplicate while preserving order.
	seen := map[string]bool{}
	out := dirs[:0]
	for _, dir := range dirs {
		if seen[dir] {
			continue
		}
		seen[dir] = true
		out = append(out, dir)
	}
	return out
}

// legacyStoreV4Root maps a legacy JSONL or v3 store directory to its sibling
// sessions-v4 root. Only the two known legacy store names are recognized.
func legacyStoreV4Root(legacyDir string) string {
	base := filepath.Base(legacyDir)
	if base != "sessions" && base != "sessions-v3" {
		return ""
	}
	return filepath.Join(filepath.Dir(legacyDir), "sessions-v4")
}

// migrateLegacyTranscripts imports every JSONL transcript in dir and deletes
// its files and sidecars once the v4 target is verified.
func migrateLegacyTranscripts(ctx context.Context, dir, target string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	imported := 0
	for _, entry := range entries {
		if entry.IsDir() || !store.IsSessionTranscriptName(entry.Name()) {
			continue
		}
		source := filepath.Join(dir, entry.Name())
		if !migrateLegacyTranscript(ctx, source, target) {
			continue
		}
		if removeLegacyTranscriptFiles(dir, entry.Name()) {
			imported++
		}
	}
	return imported
}

// migrateLegacyTranscript imports every live head of one transcript. A schema-2
// DAG has independent heads, so a single default-view import would lose the
// others when the source is deleted; each live head is published separately and
// the caller deletes the source only when all of them succeeded.
func migrateLegacyTranscript(ctx context.Context, source, target string) bool {
	heads, err := session.LegacyMigrationHeads(ctx, source)
	if err != nil {
		return false
	}
	if len(heads) == 0 {
		_, err := session.MigrateLegacy(ctx, source, target)
		return err == nil
	}
	migrated := false
	for _, head := range heads {
		if head.Retired || head.Covered {
			continue
		}
		if _, err := session.MigrateLegacyHead(ctx, source, target, head.ID); err != nil {
			return false
		}
		migrated = true
	}
	return migrated
}

// migrateLegacyStores imports every v3 session directory and deletes it once
// the v4 target is verified.
func migrateLegacyStores(ctx context.Context, dir, target string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	imported := 0
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		source := filepath.Join(dir, entry.Name())
		if _, err := session.ImportStoredPreview(ctx, source, target); err != nil {
			continue
		}
		if os.RemoveAll(source) == nil {
			imported++
		}
	}
	return imported
}

// removeLegacyTranscriptFiles deletes one transcript and every sidecar that
// belongs to it. Session artifacts are named "<stem>.…" or "<stem>.jsonl.…",
// so a dot-terminated stem prefix cannot capture a neighbouring session.
func removeLegacyTranscriptFiles(dir, transcriptName string) bool {
	stem := strings.TrimSuffix(transcriptName, ".jsonl")
	if stem == "" {
		return false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	removed := true
	for _, entry := range entries {
		name := entry.Name()
		if name != transcriptName && !strings.HasPrefix(name, stem+".") {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dir, name)); err != nil {
			removed = false
		}
	}
	return removed
}
