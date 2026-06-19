package agent

import "os"

// MigratePass defines one migration step in the session import pipeline.
// Each pass is registered via init() and runs in registration order.
type MigratePass struct {
	// Name is a human-readable label for logging.
	Name string
	// Marker, when non-empty, gates this pass: if the marker file exists in
	// the destination directory, the pass is skipped (idempotent guard).
	Marker string
	// AlwaysRun bypasses the marker check when true. Used by the rehome pass
	// which must run on every boot to handle downgrade-stranded sessions.
	AlwaysRun bool
	// Run executes the pass. It returns the count of sessions imported and
	// whether any non-fatal artifact failures occurred (which prevent marker
	// stamping so the pass retries on the next boot).
	Run func(ctx *MigrateContext) (imported int, artifactFailure bool, err error)
}

// MigrateContext carries the shared state across all migration passes.
// It is constructed once by the pipeline and mutated by passes as they run.
type MigrateContext struct {
	SrcDir     string
	GlobalDest string
	ProjectDir func(string) string

	// Entries is the pre-read directory listing of SrcDir, shared so each
	// pass doesn't re-scan the filesystem.
	Entries []os.DirEntry

	// HasEvents maps base names that have a .events.jsonl file, built once
	// by the pipeline and consumed by the jsonl pass to skip duplicates.
	HasEvents map[string]bool

	// Marker is the primary import marker for this migration invocation
	// (e.g. legacyRoutedHomeImportMarker or legacyRoutedConfigImportMarker).
	Marker string

	// AllPassesDone is set by the pipeline after all main passes (events,
	// jsonl, subdir) complete successfully. The rehome pass uses this to
	// decide whether it should run.
	AllPassesDone bool
}

// registry holds all registered migration passes in order.
var migrateRegistry []MigratePass

// registerMigratePass appends a pass to the migration pipeline.
// Called from init() functions in migrate_pass_*.go files.
func registerMigratePass(p MigratePass) {
	migrateRegistry = append(migrateRegistry, p)
}
