package migration

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/event"
)

// SessionImport records one legacy session source that contributed sessions.
type SessionImport struct {
	Source      string
	Destination string
	Count       int
}

// MemoryImport records one legacy memory source that contributed files.
type MemoryImport struct {
	Source      string
	Destination string
	Count       int
}

// Result summarizes an explicit migration rescue run.
type Result struct {
	Config         *config.MigrationResult
	ConfigErr      error
	MemoryImports  []MemoryImport
	MemoryErrs     []error
	SessionImports []SessionImport
	SessionErrs    []error
}

// Summary returns the final user-visible status for a migration rescue run.
func (r Result) Summary() string {
	importedSessions := 0
	for _, imp := range r.SessionImports {
		importedSessions += imp.Count
	}
	importedMemory := 0
	for _, imp := range r.MemoryImports {
		importedMemory += imp.Count
	}
	warnings := 0
	if r.ConfigErr != nil {
		warnings++
	}
	warnings += len(r.MemoryErrs)
	warnings += len(r.SessionErrs)
	switch {
	case warnings > 0:
		return fmt.Sprintf("migration rescue completed with %d warning(s): imported %d memory file(s) and %d past session(s)", warnings, importedMemory, importedSessions)
	case r.Config != nil || importedMemory > 0 || importedSessions > 0:
		parts := []string{}
		if r.Config != nil {
			parts = append(parts, "config/credentials")
		}
		if importedMemory > 0 {
			parts = append(parts, fmt.Sprintf("%d memory file(s)", importedMemory))
		}
		if importedSessions > 0 {
			parts = append(parts, fmt.Sprintf("%d past session(s)", importedSessions))
		}
		return "migration rescue complete: imported " + strings.Join(parts, " and ")
	default:
		return "migration rescue complete: no legacy data needed migration"
	}
}

// RunLegacyRescue retries the non-destructive legacy migration path and emits
// progress notices suitable for both the CLI TUI and desktop frontend.
func RunLegacyRescue(sink event.Sink) Result {
	sink = event.Sync(sink)
	emit := func(level event.Level, text string) {
		sink.Emit(event.Event{Kind: event.Notice, Level: level, Text: text})
	}
	result := Result{}
	if config.IsolatedHomeDir() != "" {
		emit(event.LevelInfo, "migration rescue: REASONIX_HOME is set; implicit legacy migration is skipped")
		emit(event.LevelInfo, result.Summary())
		return result
	}
	emit(event.LevelInfo, "migration rescue: checking legacy config and credentials")
	migrated, err := config.MigrateLegacyIfNeeded()
	result.Config = migrated
	result.ConfigErr = err
	if err != nil {
		emit(event.LevelWarn, "migration rescue: config migration warning: "+err.Error())
	} else if migrated != nil {
		emit(event.LevelInfo, migrated.Notice())
	} else {
		emit(event.LevelInfo, "migration rescue: current config is already present or no legacy config was found")
	}
	emit(event.LevelInfo, "migration rescue: scanning legacy memory")
	memoryResult := migrateLegacyMemorySources(sink, true)
	result.MemoryImports = memoryResult.imports
	result.MemoryErrs = memoryResult.errs
	emit(event.LevelInfo, "migration rescue: scanning legacy sessions")
	sessionResult := migrateLegacySessionSources(sink, true)
	result.SessionImports = sessionResult.imports
	result.SessionErrs = sessionResult.errs
	emit(event.LevelInfo, result.Summary())
	return result
}

// RunLegacyRescueCommand handles the /migrate argument form shared by the CLI
// TUI and desktop submit path. With no arguments it runs the default rescue;
// with --from it imports sessions from a user-selected legacy directory.
func RunLegacyRescueCommand(args string, sink event.Sink) Result {
	source, explicit, err := parseLegacyRescueArgs(args)
	if err != nil {
		sink = event.Sync(sink)
		sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: "migration rescue: " + err.Error()})
		return Result{SessionErrs: []error{err}}
	}
	if explicit {
		return RunLegacySessionImportFrom(source, sink)
	}
	return RunLegacyRescue(sink)
}

// RunLegacySessionImportFrom imports sessions from a user-selected legacy root.
// The root may be the old install directory, a data directory, or the sessions
// directory itself. Only sessions are imported; config and credentials stay on
// the default non-destructive migration path.
func RunLegacySessionImportFrom(sourceRoot string, sink event.Sink) Result {
	sink = event.Sync(sink)
	emit := func(level event.Level, text string) {
		sink.Emit(event.Event{Kind: event.Notice, Level: level, Text: text})
	}
	result := Result{}
	sourceRoot = strings.TrimSpace(sourceRoot)
	emit(event.LevelInfo, "migration rescue: scanning explicit legacy sessions from "+sourceRoot)
	sources, err := explicitLegacySessionSources(sourceRoot)
	if err != nil {
		result.SessionErrs = append(result.SessionErrs, err)
		emit(event.LevelWarn, "migration rescue: "+err.Error())
		emit(event.LevelInfo, result.Summary())
		return result
	}
	if len(sources) == 0 {
		emit(event.LevelInfo, "migration rescue: no legacy session directories found under "+sourceRoot)
		emit(event.LevelInfo, result.Summary())
		return result
	}
	for _, src := range sources {
		n, err := agent.MigrateLegacySessionsFromExplicitDir(src.dir, config.SessionDir(), config.ProjectSessionDir)
		if err != nil {
			result.SessionErrs = append(result.SessionErrs, fmt.Errorf("%s: %w", src.label, err))
			emit(event.LevelWarn, "migration rescue: skipped "+src.label+": "+err.Error())
			continue
		}
		if n > 0 {
			result.SessionImports = append(result.SessionImports, SessionImport{Source: src.label, Destination: config.SessionDir(), Count: n})
			emit(event.LevelInfo, fmt.Sprintf("imported %d past session(s) from %s — resume them with --resume or the history panel", n, src.label))
		}
	}
	if len(result.SessionImports) == 0 && len(result.SessionErrs) == 0 {
		emit(event.LevelInfo, "migration rescue: no legacy sessions needed migration from "+sourceRoot)
	}
	emit(event.LevelInfo, result.Summary())
	return result
}

// MigrateLegacyMemorySources imports older memory stores during normal boot.
// It stays quiet unless files were actually copied.
func MigrateLegacyMemorySources(sink event.Sink) []MemoryImport {
	if config.IsolatedHomeDir() != "" {
		return nil
	}
	sink = event.Sync(sink)
	return migrateLegacyMemorySources(sink, false).imports
}

// MigrateLegacySessionSources imports older session stores during normal boot.
// It preserves the historical boot-time behavior: notify only when something was
// imported, and otherwise stay quiet.
func MigrateLegacySessionSources(sink event.Sink) []SessionImport {
	if config.IsolatedHomeDir() != "" {
		return nil
	}
	sink = event.Sync(sink)
	return migrateLegacySessionSources(sink, false).imports
}

type sessionMigrationResult struct {
	imports []SessionImport
	errs    []error
}

type memoryMigrationResult struct {
	imports []MemoryImport
	errs    []error
}

func migrateLegacyMemorySources(sink event.Sink, verbose bool) memoryMigrationResult {
	dest := config.MemoryUserDir()
	if strings.TrimSpace(dest) == "" {
		return memoryMigrationResult{}
	}
	type legacyMemorySource struct {
		root  string
		label string
	}
	var sources []legacyMemorySource
	addRoot := func(root, label string) {
		root = strings.TrimSpace(root)
		if root == "" || samePath(root, dest) {
			return
		}
		sources = append(sources, legacyMemorySource{root: root, label: label})
	}
	if home, herr := os.UserHomeDir(); herr == nil {
		addRoot(filepath.Join(home, ".reasonix"), "~/.reasonix")
	}
	for _, legacyConfig := range config.LegacyUserConfigPaths() {
		addRoot(filepath.Dir(legacyConfig), filepath.Dir(legacyConfig))
	}

	seen := map[string]bool{}
	result := memoryMigrationResult{}
	for _, src := range sources {
		key := cleanAbs(src.root)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		n, err := copyLegacyMemoryRoot(src.root, dest)
		if err != nil {
			result.errs = append(result.errs, fmt.Errorf("%s: %w", src.label, err))
			if verbose {
				sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: "migration rescue: skipped memory from " + src.label + ": " + err.Error()})
			}
			continue
		}
		if n > 0 {
			result.imports = append(result.imports, MemoryImport{Source: src.label, Destination: dest, Count: n})
			sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: fmt.Sprintf("imported %d memory file(s) from %s", n, src.label)})
		}
	}
	if verbose && len(result.imports) == 0 && len(result.errs) == 0 {
		sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: "migration rescue: no legacy memory needed migration"})
	}
	return result
}

func copyLegacyMemoryRoot(srcRoot, destRoot string) (int, error) {
	if samePath(srcRoot, destRoot) {
		return 0, nil
	}
	total := 0
	for _, name := range []string{"REASONIX.md", "AGENTS.md", "CLAUDE.md"} {
		n, err := copyFileIfMissing(filepath.Join(srcRoot, name), filepath.Join(destRoot, name))
		if err != nil {
			return total, err
		}
		total += n
	}
	if n, err := copyMissingTree(filepath.Join(srcRoot, "memory"), filepath.Join(destRoot, "memory")); err != nil {
		return total, err
	} else {
		total += n
	}
	projectsDir := filepath.Join(srcRoot, "projects")
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return total, nil
		}
		return total, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		slug := entry.Name()
		n, err := copyMissingTree(filepath.Join(projectsDir, slug, "memory"), filepath.Join(destRoot, "projects", slug, "memory"))
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func copyMissingTree(src, dst string) (int, error) {
	info, err := os.Stat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	if !info.IsDir() {
		return copyFileIfMissing(src, dst)
	}
	count := 0
	err = filepath.WalkDir(src, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil || rel == "." {
			return err
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		n, err := copyFileIfMissing(path, target)
		count += n
		return err
	})
	return count, err
}

func copyFileIfMissing(src, dst string) (int, error) {
	in, err := os.Open(src)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return 0, err
	}
	if !info.Mode().IsRegular() {
		return 0, nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return 0, err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		if os.IsExist(err) {
			return 0, nil
		}
		return 0, err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		_ = os.Remove(dst)
		return 0, err
	}
	return 1, nil
}

func migrateLegacySessionSources(sink event.Sink, verbose bool) sessionMigrationResult {
	dest := config.SessionDir()
	if strings.TrimSpace(dest) == "" {
		return sessionMigrationResult{}
	}
	type legacySource struct {
		dir     string
		dest    string
		label   string
		migrate func(srcDir, globalDest string, projectDir func(string) string) (int, error)
	}
	var sources []legacySource
	addFlatSource := func(dir, label string, migrate func(string, string, func(string) string) (int, error)) {
		sources = append(sources, legacySource{
			dir:     dir,
			dest:    dest,
			label:   label,
			migrate: migrate,
		})
	}
	addProjectSources := func(root string) {
		root = strings.TrimSpace(root)
		if root == "" || config.MemoryUserDir() == "" {
			return
		}
		if samePath(root, config.MemoryUserDir()) {
			return
		}
		projectsDir := filepath.Join(root, "projects")
		entries, err := os.ReadDir(projectsDir)
		if err != nil {
			return
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			slug := entry.Name()
			srcDir := filepath.Join(projectsDir, slug, "sessions")
			dstDir := filepath.Join(config.MemoryUserDir(), "projects", slug, "sessions")
			sources = append(sources, legacySource{
				dir:     srcDir,
				dest:    dstDir,
				label:   srcDir,
				migrate: agent.MigrateLegacySessionsFromConfigDir,
			})
		}
	}
	if home, herr := os.UserHomeDir(); herr == nil {
		reasonixHome := filepath.Join(home, ".reasonix")
		addFlatSource(filepath.Join(reasonixHome, "sessions"), "~/.reasonix/sessions", agent.MigrateLegacySessions)
		addProjectSources(reasonixHome)
	}
	for _, legacyConfig := range config.LegacyUserConfigPaths() {
		legacyDir := filepath.Join(filepath.Dir(legacyConfig), "sessions")
		addFlatSource(legacyDir, legacyDir, agent.MigrateLegacySessionsFromConfigDir)
		addProjectSources(filepath.Dir(legacyConfig))
	}
	// Back-fill v0.x sessions from the current user config session directory as
	// well. This covers users whose platform config root was redirected before the
	// Go rewrite; their event logs can already live where v2 stores sessions.
	addFlatSource(dest, dest, agent.MigrateLegacySessionsFromConfigDir)

	seen := map[string]bool{}
	result := sessionMigrationResult{}
	for _, src := range sources {
		if strings.TrimSpace(src.dir) == "" {
			continue
		}
		sourceDest := strings.TrimSpace(src.dest)
		if sourceDest == "" {
			sourceDest = dest
		}
		key := filepath.Clean(src.dir) + "=>" + filepath.Clean(sourceDest)
		if seen[key] {
			continue
		}
		seen[key] = true
		n, err := src.migrate(src.dir, sourceDest, config.ProjectSessionDir)
		if err != nil {
			result.errs = append(result.errs, fmt.Errorf("%s: %w", src.label, err))
			if verbose {
				sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: "migration rescue: skipped " + src.label + ": " + err.Error()})
			}
			continue
		}
		if n > 0 {
			result.imports = append(result.imports, SessionImport{Source: src.label, Destination: sourceDest, Count: n})
			sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: fmt.Sprintf("imported %d past session(s) from %s — resume them with --resume or the history panel", n, src.label)})
		}
	}
	if verbose && len(result.imports) == 0 && len(result.errs) == 0 {
		sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: "migration rescue: no legacy sessions needed migration"})
	}
	return result
}

type explicitSessionSource struct {
	dir   string
	label string
}

func parseLegacyRescueArgs(args string) (source string, explicit bool, err error) {
	args = strings.TrimSpace(args)
	if args == "" {
		return "", false, nil
	}
	const flag = "--from"
	switch {
	case args == flag:
		return "", false, fmt.Errorf("--from requires a legacy directory path")
	case strings.HasPrefix(args, flag+"="):
		source = strings.TrimSpace(strings.TrimPrefix(args, flag+"="))
	case len(args) > len(flag) && strings.HasPrefix(args, flag) && (args[len(flag)] == ' ' || args[len(flag)] == '\t'):
		source = strings.TrimSpace(args[len(flag):])
	default:
		first := args
		if i := strings.IndexAny(first, " \t"); i >= 0 {
			first = first[:i]
		}
		return "", false, fmt.Errorf("unknown /migrate option %q; use /migrate --from <legacy-dir>", first)
	}
	source = trimMatchingQuotes(source)
	if source == "" {
		return "", false, fmt.Errorf("--from requires a legacy directory path")
	}
	return source, true, nil
}

func trimMatchingQuotes(s string) string {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return s
	}
	if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
		return s[1 : len(s)-1]
	}
	return s
}

func explicitLegacySessionSources(root string) ([]explicitSessionSource, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("--from requires a legacy directory path")
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("legacy directory %s is not readable: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("legacy path %s is not a directory", root)
	}
	candidates := []string{
		filepath.Join(root, "sessions"),
		filepath.Join(root, ".reasonix", "sessions"),
		filepath.Join(root, "reasonix", "sessions"),
	}
	var out []explicitSessionSource
	seen := map[string]bool{}
	for _, dir := range candidates {
		key := cleanAbs(dir)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		if dirLooksLikeLegacySessionDir(dir) {
			out = append(out, explicitSessionSource{dir: dir, label: dir})
		}
	}
	if len(out) == 0 && dirLooksLikeLegacySessionDir(root) {
		out = append(out, explicitSessionSource{dir: root, label: root})
	}
	return out, nil
}

func dirLooksLikeLegacySessionDir(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() && legacySessionArtifactName(entry.Name()) {
			return true
		}
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "subagents" {
			continue
		}
		subEntries, err := os.ReadDir(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		for _, sub := range subEntries {
			if !sub.IsDir() && legacySessionArtifactName(sub.Name()) {
				return true
			}
		}
	}
	return false
}

func legacySessionArtifactName(name string) bool {
	return strings.HasSuffix(name, ".events.jsonl") ||
		strings.HasSuffix(name, ".jsonl") ||
		strings.HasSuffix(name, ".jsonl.bak")
}

func samePath(a, b string) bool {
	aa := cleanAbs(a)
	bb := cleanAbs(b)
	return aa != "" && bb != "" && aa == bb
}

func cleanAbs(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return filepath.Clean(path)
}
