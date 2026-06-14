package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"reasonix/internal/agent"
)

// FileWatchConfig configures file watching for a session.
type FileWatchConfig struct {
	// Paths to watch (relative to workspace root or absolute).
	Paths []string `json:"paths"`
	// IgnorePatterns are glob patterns to skip (e.g. "node_modules/**").
	IgnorePatterns []string `json:"ignore_patterns"`
	// Debounce is how long to wait after the last change before triggering.
	Debounce time.Duration `json:"debounce"`
	// Enabled controls whether this watcher is active.
	Enabled bool `json:"enabled"`
}

// FileWatcher monitors filesystem changes and queues wakeups for sessions
// that have file watch configurations. It prefers native fs notifications and
// keeps polling as a correctness fallback; per-session debounce controls wakeup
// batching for both paths.
type FileWatcher struct {
	daemon *Daemon
	logger *slog.Logger

	mu          sync.Mutex
	watches     map[string]*watchState // session ID -> state
	running     bool
	cancel      context.CancelFunc
	native      *fsnotify.Watcher
	nativeRoots map[string]struct{}
	stats       FileWatcherStats
}

type watchState struct {
	config    FileWatchConfig
	lastSeen  map[string]time.Time // path → last mod time
	lastFired time.Time
	pending   bool // debounce pending
	changes   map[string]struct{}
	timer     time.Time
}

// FileWatcherStats is exposed on /status so large-repo watcher health can be
// inspected without reading daemon logs.
type FileWatcherStats struct {
	Mode               string `json:"mode"`
	NativeAvailable    bool   `json:"native_available"`
	NativeWatchRoots   int    `json:"native_watch_roots"`
	NativeEvents       int64  `json:"native_events"`
	NativeErrors       int64  `json:"native_errors"`
	IgnoredChanges     int64  `json:"ignored_changes"`
	PollScans          int64  `json:"poll_scans"`
	PollInterval       string `json:"poll_interval"`
	LastPollDurationMS int64  `json:"last_poll_duration_ms"`
	LastPollFiles      int    `json:"last_poll_files"`
	LastPollDirs       int    `json:"last_poll_dirs"`
	LastNativeError    string `json:"last_native_error,omitempty"`
}

const (
	maxFileWatchSummaryFiles = 20
	pollingInterval          = 2 * time.Second
	hybridFallbackInterval   = 30 * time.Second
	debounceCheckInterval    = 250 * time.Millisecond
)

// DefaultIgnorePatterns are always excluded from file watching.
var DefaultIgnorePatterns = []string{
	"node_modules",
	".git",
	"__pycache__",
	".venv",
	"vendor",
	"target",
	"build",
	"dist",
	".env",
	"*.key",
	"*.pem",
	".DS_Store",
}

// NewFileWatcher creates a file watcher bound to the daemon.
func NewFileWatcher(d *Daemon, logger *slog.Logger) *FileWatcher {
	return &FileWatcher{
		daemon:  d,
		logger:  logger,
		watches: make(map[string]*watchState),
	}
}

// Register adds or updates a file watch for a session.
func (fw *FileWatcher) Register(sessionID string, cfg FileWatchConfig) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	fw.watches[sessionID] = &watchState{
		config:   cfg,
		lastSeen: make(map[string]time.Time),
		changes:  make(map[string]struct{}),
	}
	fw.refreshNativeWatchesLocked()
}

// Unregister removes a file watch for a session.
func (fw *FileWatcher) Unregister(sessionID string) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	delete(fw.watches, sessionID)
	fw.refreshNativeWatchesLocked()
}

// Start begins the hybrid native/polling watch loop.
func (fw *FileWatcher) Start(ctx context.Context) {
	fw.mu.Lock()
	if fw.running {
		fw.mu.Unlock()
		return
	}
	ctx, fw.cancel = context.WithCancel(ctx)
	fw.running = true
	pollInterval := pollingInterval
	if native, err := fsnotify.NewWatcher(); err == nil {
		fw.native = native
		fw.nativeRoots = make(map[string]struct{})
		fw.stats.Mode = "hybrid"
		fw.stats.NativeAvailable = true
		pollInterval = hybridFallbackInterval
		fw.refreshNativeWatchesLocked()
	} else {
		fw.stats.Mode = "polling"
		fw.stats.NativeAvailable = false
		fw.stats.NativeErrors++
		fw.stats.LastNativeError = err.Error()
	}
	fw.stats.PollInterval = pollInterval.String()
	native := fw.native
	fw.mu.Unlock()

	pollTicker := time.NewTicker(pollInterval)
	defer pollTicker.Stop()
	debounceTicker := time.NewTicker(debounceCheckInterval)
	defer debounceTicker.Stop()
	var nativeEvents <-chan fsnotify.Event
	var nativeErrors <-chan error
	if native != nil {
		nativeEvents = native.Events
		nativeErrors = native.Errors
		defer native.Close()
	}

	for {
		select {
		case <-ctx.Done():
			fw.mu.Lock()
			fw.running = false
			fw.native = nil
			fw.nativeRoots = nil
			fw.mu.Unlock()
			return
		case event, ok := <-nativeEvents:
			if !ok {
				nativeEvents = nil
				continue
			}
			fw.handleNativeEvent(event, time.Now())
		case err, ok := <-nativeErrors:
			if !ok {
				nativeErrors = nil
				continue
			}
			fw.recordNativeError(err)
		case <-debounceTicker.C:
			fw.flushDue(time.Now())
		case <-pollTicker.C:
			fw.poll()
		}
	}
}

// Stop halts the file watcher.
func (fw *FileWatcher) Stop() {
	fw.mu.Lock()
	if fw.cancel != nil {
		fw.cancel()
	}
	fw.mu.Unlock()
}

func (fw *FileWatcher) Stats() FileWatcherStats {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	stats := fw.stats
	if stats.Mode == "" {
		stats.Mode = "polling"
	}
	if stats.PollInterval == "" {
		stats.PollInterval = pollingInterval.String()
	}
	return stats
}

func (fw *FileWatcher) poll() {
	start := time.Now()
	fw.mu.Lock()
	sessions := make([]string, 0, len(fw.watches))
	for id := range fw.watches {
		sessions = append(sessions, id)
	}
	fw.mu.Unlock()

	now := time.Now()
	totalFiles := 0
	totalDirs := 0
	for _, id := range sessions {
		fw.mu.Lock()
		state, ok := fw.watches[id]
		fw.mu.Unlock()
		if !ok {
			continue
		}
		if !state.config.Enabled {
			continue
		}

		changes, files, dirs := fw.detectChanges(state)
		totalFiles += files
		totalDirs += dirs
		if len(changes) == 0 {
			continue
		}

		fw.noteChanges(state, changes, now)
	}
	fw.mu.Lock()
	fw.stats.PollScans++
	fw.stats.LastPollDurationMS = time.Since(start).Milliseconds()
	fw.stats.LastPollFiles = totalFiles
	fw.stats.LastPollDirs = totalDirs
	fw.mu.Unlock()
	fw.flushDue(now)
}

func (fw *FileWatcher) detectChanges(state *watchState) ([]string, int, int) {
	var changed []string
	totalFiles := 0
	totalDirs := 0
	for _, path := range state.config.Paths {
		entries, dirs := fw.walkPath(path, state.config.IgnorePatterns)
		totalDirs += dirs
		totalFiles += len(entries)
		for _, entry := range entries {
			info, err := os.Stat(entry)
			if err != nil {
				continue
			}
			mod := info.ModTime()
			if prev, ok := state.lastSeen[entry]; ok {
				if mod.After(prev) {
					changed = append(changed, entry)
				}
			}
			state.lastSeen[entry] = mod
		}
	}
	return changed, totalFiles, totalDirs
}

func (fw *FileWatcher) walkPath(root string, ignorePatterns []string) ([]string, int) {
	var files []string
	dirs := 0
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			dirs++
			name := info.Name()
			if fw.shouldIgnore(name, ignorePatterns) {
				return filepath.SkipDir
			}
			return nil
		}
		if fw.shouldIgnore(info.Name(), ignorePatterns) {
			return nil
		}
		files = append(files, path)
		return nil
	})
	return files, dirs
}

func (fw *FileWatcher) shouldIgnore(name string, patterns []string) bool {
	allPatterns := append(DefaultIgnorePatterns, patterns...)
	for _, p := range allPatterns {
		if matched, _ := filepath.Match(p, name); matched {
			return true
		}
		// Also check if the directory name matches.
		if strings.EqualFold(name, p) {
			return true
		}
	}
	return false
}

func (fw *FileWatcher) handleNativeEvent(event fsnotify.Event, now time.Time) {
	if event.Name == "" || event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) == 0 {
		return
	}
	path := filepath.Clean(event.Name)
	var matched bool
	var ignored bool

	fw.mu.Lock()
	if event.Op&fsnotify.Create != 0 {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			fw.refreshNativeWatchesLocked()
		}
	}
	for _, state := range fw.watches {
		if state == nil || !state.config.Enabled {
			continue
		}
		if fw.pathIgnored(path, state.config.IgnorePatterns) {
			ignored = true
			continue
		}
		if !fw.eventMatchesConfig(state.config, path) {
			continue
		}
		matched = true
		if info, err := os.Stat(path); err == nil {
			state.lastSeen[path] = info.ModTime()
		}
		fw.noteChangesLocked(state, []string{path}, now)
	}
	fw.stats.NativeEvents++
	if ignored || !matched {
		fw.stats.IgnoredChanges++
	}
	fw.mu.Unlock()
}

func (fw *FileWatcher) noteChanges(state *watchState, changes []string, now time.Time) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	fw.noteChangesLocked(state, changes, now)
}

func (fw *FileWatcher) noteChangesLocked(state *watchState, changes []string, now time.Time) {
	for _, change := range changes {
		state.changes[change] = struct{}{}
	}
	debounce := state.config.Debounce
	if debounce == 0 {
		debounce = 3 * time.Second
	}
	state.pending = true
	state.timer = now.Add(debounce)
}

func (fw *FileWatcher) flushDue(now time.Time) {
	fw.mu.Lock()
	sessions := make([]string, 0, len(fw.watches))
	for id := range fw.watches {
		sessions = append(sessions, id)
	}
	fw.mu.Unlock()
	for _, id := range sessions {
		fw.mu.Lock()
		state, ok := fw.watches[id]
		due := ok && state.pending && now.After(state.timer)
		fw.mu.Unlock()
		if due {
			fw.fireWakeup(id, state, now)
		}
	}
}

func (fw *FileWatcher) eventMatchesConfig(cfg FileWatchConfig, path string) bool {
	path = filepath.Clean(path)
	for _, root := range cfg.Paths {
		root = filepath.Clean(strings.TrimSpace(root))
		if root == "" {
			continue
		}
		if info, err := os.Stat(root); err == nil && info.IsDir() {
			if path == root || strings.HasPrefix(path, root+string(filepath.Separator)) {
				return true
			}
			continue
		}
		if path == root {
			return true
		}
	}
	return false
}

func (fw *FileWatcher) pathIgnored(path string, patterns []string) bool {
	return fw.shouldIgnore(filepath.Base(path), patterns)
}

func (fw *FileWatcher) refreshNativeWatchesLocked() {
	if fw.native == nil {
		return
	}
	want := make(map[string]struct{})
	for _, state := range fw.watches {
		if state == nil || !state.config.Enabled {
			continue
		}
		for _, dir := range fw.nativeWatchDirs(state.config) {
			want[dir] = struct{}{}
		}
	}
	for root := range fw.nativeRoots {
		if _, ok := want[root]; ok {
			continue
		}
		_ = fw.native.Remove(root)
		delete(fw.nativeRoots, root)
	}
	for root := range want {
		if _, ok := fw.nativeRoots[root]; ok {
			continue
		}
		if err := fw.native.Add(root); err != nil {
			fw.stats.NativeErrors++
			fw.stats.LastNativeError = err.Error()
			continue
		}
		fw.nativeRoots[root] = struct{}{}
	}
	fw.stats.NativeWatchRoots = len(fw.nativeRoots)
}

func (fw *FileWatcher) nativeWatchDirs(cfg FileWatchConfig) []string {
	seen := make(map[string]struct{})
	var dirs []string
	add := func(dir string) {
		dir = filepath.Clean(strings.TrimSpace(dir))
		if dir == "" || fw.pathIgnored(dir, cfg.IgnorePatterns) {
			return
		}
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			return
		}
		if _, ok := seen[dir]; ok {
			return
		}
		seen[dir] = struct{}{}
		dirs = append(dirs, dir)
	}
	for _, root := range cfg.Paths {
		root = filepath.Clean(strings.TrimSpace(root))
		if root == "" {
			continue
		}
		info, err := os.Stat(root)
		if err != nil {
			add(filepath.Dir(root))
			continue
		}
		if !info.IsDir() {
			add(filepath.Dir(root))
			continue
		}
		filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil || !entry.IsDir() {
				return nil
			}
			if path != root && fw.shouldIgnore(entry.Name(), cfg.IgnorePatterns) {
				return filepath.SkipDir
			}
			add(path)
			return nil
		})
	}
	sort.Strings(dirs)
	return dirs
}

func (fw *FileWatcher) recordNativeError(err error) {
	if err == nil {
		return
	}
	fw.mu.Lock()
	fw.stats.NativeErrors++
	fw.stats.LastNativeError = err.Error()
	fw.mu.Unlock()
	if fw.logger != nil {
		fw.logger.Warn("file watcher native event error", "err", err)
	}
}

func (fw *FileWatcher) fireWakeup(sessionID string, state *watchState, now time.Time) {
	state.pending = false
	state.lastFired = now
	changes := sortedFileWatchChanges(state.changes)
	state.changes = make(map[string]struct{})
	summary := fileWatchWakeupContext(state.config, changes)

	fw.daemon.mu.Lock()
	entry, ok := fw.daemon.registry[sessionID]
	if !ok {
		fw.daemon.mu.Unlock()
		return
	}
	if _, running := fw.daemon.activeRuns[sessionID]; running {
		fw.daemon.mu.Unlock()
		return
	}

	// Guards: goal must be active, run must not be in-flight.
	if entry.Runtime.Goal.Status != "running" && entry.Runtime.Goal.Status != "blocked" {
		fw.daemon.mu.Unlock()
		return
	}
	if agent.IsRunInFlight(entry.Runtime.Run.Status) {
		fw.daemon.mu.Unlock()
		return
	}
	wait := entry.Runtime.Wait
	if wait.Kind != "" && wait.Kind != "file" {
		runtime := entry.Runtime
		path := entry.Path
		fw.daemon.mu.Unlock()
		fw.daemon.appendTimeline(path, agent.RuntimeTimelineEvent{
			Type:       "file_change_ignored",
			Source:     "file_watch",
			Reason:     "waiting_for_" + wait.Kind,
			Step:       "deterministic",
			RunStatus:  runtime.Run.Status,
			GoalStatus: runtime.Goal.Status,
			WaitKind:   wait.Kind,
			Subject:    wait.Subject,
			Message:    summary,
		})
		return
	}
	if wait.Kind == "file" && !fileWaitMatches(wait, state.config, changes) {
		runtime := entry.Runtime
		path := entry.Path
		fw.daemon.mu.Unlock()
		fw.daemon.appendTimeline(path, agent.RuntimeTimelineEvent{
			Type:       "wait_file_ignored",
			Source:     "file_watch",
			Reason:     "changed files did not match wait condition",
			Step:       "deterministic",
			RunStatus:  runtime.Run.Status,
			GoalStatus: runtime.Goal.Status,
			WaitKind:   wait.Kind,
			Subject:    wait.Subject,
			Message:    summary,
		})
		return
	}
	if ok, reason := reserveAutoWakeupBudget(&entry.Runtime, "file_watch", now); !ok {
		entry.Runtime.Scheduler.LastWakeupAt = now
		entry.Runtime.Scheduler.LastWakeupReason = "budget_blocked:file_watch"
		runtime := entry.Runtime
		path := entry.Path
		fw.daemon.mu.Unlock()
		if err := agent.SaveRuntimeMeta(path, runtime); err != nil {
			fw.logger.Warn("file watcher: save runtime after budget block", "err", err, "session", sessionID)
		}
		fw.daemon.appendTimeline(path, agent.RuntimeTimelineEvent{
			Type:       "wakeup_budget_blocked",
			Source:     "file_watch",
			Reason:     reason,
			Step:       "deterministic",
			RunStatus:  runtime.Run.Status,
			GoalStatus: runtime.Goal.Status,
			WaitKind:   runtime.Wait.Kind,
			Subject:    runtime.Wait.Subject,
			Message:    reason,
		})
		return
	}

	clearFileWait := wait.Kind == "file"
	if clearFileWait {
		entry.Runtime.Wait = agent.RuntimeWaitMeta{}
		entry.Runtime.FileWatch = agent.RuntimeWatchMeta{}
	}
	entry.Runtime.Run.Status = agent.RunStatusQueued
	entry.Runtime.Run.LastWakeupReason = "file_change"
	entry.Runtime.Run.ResumeCount++
	entry.Runtime.Scheduler.LastWakeupAt = now
	entry.Runtime.Scheduler.LastWakeupReason = "file_change"
	runtime := entry.Runtime
	path := entry.Path
	fw.daemon.mu.Unlock()

	if err := agent.SaveRuntimeMeta(path, runtime); err != nil {
		fw.logger.Warn("file watcher: save runtime", "err", err, "session", sessionID)
	} else {
		fw.logger.Info("file watcher triggered wakeup", "session", sessionID)
	}
	if clearFileWait {
		fw.Unregister(sessionID)
	}
	fw.daemon.appendTimeline(path, agent.RuntimeTimelineEvent{
		Type:       "file_change_detected",
		Source:     "file_watch",
		Reason:     "file_change",
		Step:       "deterministic",
		RunStatus:  runtime.Run.Status,
		GoalStatus: runtime.Goal.Status,
		WaitKind:   wait.Kind,
		Subject:    wait.Subject,
		Message:    summary,
	})
	fw.daemon.enqueueIntent(RunIntent{
		SessionID:   sessionID,
		SessionPath: path,
		Source:      "file_watch",
		Reason:      "file_change",
		Context:     summary,
	})
}

func fileWaitMatches(wait agent.RuntimeWaitMeta, cfg FileWatchConfig, changes []string) bool {
	if len(wait.FilePaths) == 0 {
		return true
	}
	for _, changed := range changes {
		for _, want := range wait.FilePaths {
			if filePathMatchesWait(want, changed, cfg.Paths) {
				return true
			}
		}
	}
	return false
}

func filePathMatchesWait(want, changed string, roots []string) bool {
	want = filepath.Clean(strings.TrimSpace(want))
	if want == "." || want == "" {
		return false
	}
	changed = filepath.Clean(changed)
	display := filepath.Clean(displayFileWatchPath(roots, changed))
	base := filepath.Base(changed)
	candidates := []string{changed, display, base}
	matches := func(pattern, candidate string) bool {
		candidate = filepath.Clean(candidate)
		if candidate == pattern || strings.HasSuffix(candidate, string(filepath.Separator)+pattern) {
			return true
		}
		if strings.HasPrefix(candidate, pattern+string(filepath.Separator)) {
			return true
		}
		if matched, _ := filepath.Match(pattern, candidate); matched {
			return true
		}
		if matched, _ := filepath.Match(pattern, filepath.Base(candidate)); matched {
			return true
		}
		return false
	}
	if !filepath.IsAbs(want) {
		for _, root := range roots {
			root = filepath.Clean(strings.TrimSpace(root))
			if root == "" {
				continue
			}
			if matches(filepath.Join(root, want), changed) {
				return true
			}
			if rel, err := filepath.Rel(filepath.Dir(root), changed); err == nil {
				candidates = append(candidates, filepath.Clean(rel))
			}
		}
	}
	for _, candidate := range candidates {
		if matches(want, candidate) {
			return true
		}
	}
	return false
}

func sortedFileWatchChanges(changes map[string]struct{}) []string {
	if len(changes) == 0 {
		return nil
	}
	out := make([]string, 0, len(changes))
	for path := range changes {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

func fileWatchWakeupContext(cfg FileWatchConfig, changes []string) string {
	if len(changes) == 0 {
		return "File watch detected changes, but no changed file paths were captured."
	}
	limit := len(changes)
	if limit > maxFileWatchSummaryFiles {
		limit = maxFileWatchSummaryFiles
	}
	var b strings.Builder
	fmt.Fprintf(&b, "File watch detected %d changed file(s).\nChanged files:", len(changes))
	for _, path := range changes[:limit] {
		fmt.Fprintf(&b, "\n- %s", displayFileWatchPath(cfg.Paths, path))
	}
	if omitted := len(changes) - limit; omitted > 0 {
		fmt.Fprintf(&b, "\n... %d more file(s) omitted", omitted)
	}
	return b.String()
}

func displayFileWatchPath(roots []string, path string) string {
	path = filepath.Clean(path)
	best := path
	bestLen := -1
	for _, root := range roots {
		root = filepath.Clean(strings.TrimSpace(root))
		if root == "" {
			continue
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
			continue
		}
		if rel == "." {
			rel = filepath.Base(path)
		}
		if len(root) > bestLen {
			best = rel
			bestLen = len(root)
		}
	}
	return best
}
