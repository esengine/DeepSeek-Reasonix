// Package environment probes the local developer environment at startup and
// renders a small, stable model-facing summary.
package environment

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"reasonix/internal/proc"
	"reasonix/internal/secrets"
	"reasonix/internal/shellparse"
)

const ProbeTimeout = 2 * time.Second

const probeWaitDelay = time.Second

const probeCacheTTL = 5 * time.Minute

const maxRenderedTools = 24

var probeTimeout = ProbeTimeout

type probeCacheEntry struct {
	storedAt time.Time
	results  []ProbeResult
}

type probeInflight struct {
	done    chan struct{}
	results []ProbeResult
}

var (
	probeCacheMu       sync.Mutex
	probeCache         = map[string]probeCacheEntry{}
	probeInflightCalls = map[string]*probeInflight{}
	probeNow           = time.Now
)

type ProbeResult struct {
	Command string
	Binary  string
	Output  string
	Found   bool
	Error   string
}

type ProbeOptions struct {
	Overrides map[string]string
	DenyRoots []string
	// SnapshotDir, when set, persists probe results across process restarts
	// (one snapshot per fingerprint under SnapshotDir/environment). A snapshot
	// younger than probeSnapshotTTL is served without re-probing, and a
	// refresh merges transient failures against the previous snapshot — both
	// keep the rendered environment section byte-stable so the cached
	// system-prompt prefix survives rebuilds. Empty disables persistence.
	// The directory is host state, never model-visible, so it stays out of
	// the probe fingerprint.
	SnapshotDir string
}

func DefaultProbes() []string {
	return []string{
		"go version",
		"python3 --version",
		"python --version",
		"node --version",
		"npm --version",
		"rustc --version",
		"cargo --version",
		"git version",
		"make --version",
		"rg --version",
		"docker --version",
	}
}

func RunProbes(ctx context.Context, commands []string) []ProbeResult {
	return RunProbesWithOverrides(ctx, commands, nil)
}

func RunProbesWithOverrides(ctx context.Context, commands []string, overrides map[string]string) []ProbeResult {
	return RunProbesWithOptions(ctx, commands, ProbeOptions{Overrides: overrides})
}

func RunProbesWithOptions(ctx context.Context, commands []string, opts ProbeOptions) []ProbeResult {
	key := probeFingerprint(commands, opts)
	now := probeNow()
	if results, ok := cachedProbeResults(key, now); ok {
		return results
	}
	if call, ok := beginProbe(key); ok {
		<-call.done
		return cloneProbeResults(call.results)
	}
	// A fresh persisted snapshot substitutes for a live run entirely: rebuilds
	// and app relaunches within the TTL render the exact bytes the sessions on
	// this machine were recorded with, so the provider prefix cache survives.
	snapshot, hasSnapshot := loadProbeSnapshot(opts.SnapshotDir, key)
	if hasSnapshot && now.Sub(snapshot.StoredAt) < probeSnapshotTTL {
		finishProbe(key, snapshot.Results, now)
		return cloneProbeResults(snapshot.Results)
	}
	results := runProbesUncached(ctx, commands, opts)
	if hasSnapshot {
		// Even an expired snapshot anchors the flap merge: transient failures
		// (timeout, nonzero exit) keep the previous successful observation so
		// a slow tool cannot rewrite the prompt prefix.
		results = mergeProbeSnapshot(snapshot.Results, results)
	}
	saveProbeSnapshot(opts.SnapshotDir, key, results, now)
	finishProbe(key, results, probeNow())
	return cloneProbeResults(results)
}

func runProbesUncached(ctx context.Context, commands []string, opts ProbeOptions) []ProbeResult {
	results := make([]ProbeResult, len(commands))
	var wg sync.WaitGroup
	for i, command := range commands {
		wg.Add(1)
		go func(i int, command string) {
			defer wg.Done()
			results[i] = runOne(ctx, command, opts)
		}(i, command)
	}
	wg.Wait()
	sortResults(results)
	return results
}

func cachedProbeResults(key string, now time.Time) ([]ProbeResult, bool) {
	probeCacheMu.Lock()
	defer probeCacheMu.Unlock()
	entry, ok := probeCache[key]
	if !ok || now.Sub(entry.storedAt) >= probeCacheTTL {
		if ok {
			delete(probeCache, key)
		}
		return nil, false
	}
	return cloneProbeResults(entry.results), true
}

func beginProbe(key string) (*probeInflight, bool) {
	probeCacheMu.Lock()
	defer probeCacheMu.Unlock()
	if call, ok := probeInflightCalls[key]; ok {
		return call, true
	}
	probeInflightCalls[key] = &probeInflight{done: make(chan struct{})}
	return nil, false
}

func finishProbe(key string, results []ProbeResult, now time.Time) {
	probeCacheMu.Lock()
	defer probeCacheMu.Unlock()
	cached := cloneProbeResults(results)
	probeCache[key] = probeCacheEntry{storedAt: now, results: cached}
	if call, ok := probeInflightCalls[key]; ok {
		call.results = cached
		delete(probeInflightCalls, key)
		close(call.done)
	}
}

func probeFingerprint(commands []string, opts ProbeOptions) string {
	var b strings.Builder
	b.WriteString("v1")
	for _, command := range commands {
		b.WriteByte('\x00')
		b.WriteString(strings.TrimSpace(command))
	}
	for _, name := range sortedMapKeys(opts.Overrides) {
		b.WriteByte('\x00')
		b.WriteString(name)
		b.WriteByte('=')
		b.WriteString(expandHome(opts.Overrides[name]))
	}
	for _, root := range normalizedDenyRoots(opts.DenyRoots) {
		b.WriteByte('\x00')
		b.WriteString("deny=")
		b.WriteString(root)
	}
	return b.String()
}

func cloneProbeResults(results []ProbeResult) []ProbeResult {
	if results == nil {
		return nil
	}
	return append([]ProbeResult(nil), results...)
}

func runOne(ctx context.Context, command string, opts ProbeOptions) ProbeResult {
	probe, err := shellparse.ParseStaticCommand(command, shellparse.StaticCommandPolicy{AllowEnvAssignments: true, AllowStderrToStdout: true})
	if err != nil {
		return ProbeResult{Command: command, Binary: command, Error: "invalid command: " + err.Error()}
	}
	parts := probe.Argv
	if len(parts) == 0 {
		return ProbeResult{Command: command, Binary: command, Error: "empty command"}
	}
	res := ProbeResult{Command: command, Binary: parts[0]}
	var exe string
	if override := strings.TrimSpace(opts.Overrides[parts[0]]); override != "" {
		exe = expandHome(override)
		if !filepath.IsAbs(exe) {
			res.Error = "not trusted"
			return res
		}
		if !fileExecutable(exe) {
			res.Error = "not found"
			return res
		}
	} else {
		found, err := exec.LookPath(parts[0])
		if err != nil {
			res.Error = "not found"
			return res
		}
		exe = found
	}
	if blockedExecutable(exe, opts.DenyRoots) {
		res.Error = "not trusted"
		return res
	}
	cmdCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, exe, parts[1:]...)
	// Always set the env explicitly: leaving cmd.Env nil would inherit the
	// full process environment and bypass [secrets] filter_subprocess_env for
	// probes that declare no extra variables of their own.
	cmd.Env = append(secrets.ProcessEnv(), probe.Env...)
	prepareProbeCommand(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	if probe.MergeStderr {
		cmd.Stderr = &stdout
	} else {
		cmd.Stderr = &stderr
	}
	err = cmd.Run()
	out := strings.TrimSpace(stdout.String())
	if out == "" {
		out = strings.TrimSpace(stderr.String())
	}
	if err != nil {
		if cmdCtx.Err() == context.DeadlineExceeded {
			res.Error = "timeout"
			return res
		}
		if out == "" {
			res.Error = "exit " + err.Error()
			return res
		}
		res.Error = firstLine(out)
		return res
	}
	res.Found = true
	res.Output = firstLine(out)
	return res
}

func prepareProbeCommand(cmd *exec.Cmd) {
	proc.HideWindow(cmd)
	proc.SetProcessGroupKill(cmd)
	cmd.Cancel = func() error {
		proc.KillTree(cmd)
		return nil
	}
	cmd.WaitDelay = probeWaitDelay
}

func sortResults(results []ProbeResult) {
	sort.Slice(results, func(i, j int) bool {
		if results[i].Found != results[j].Found {
			return results[i].Found
		}
		return results[i].Binary < results[j].Binary
	})
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimRight(s[:i], "\r")
	}
	return s
}

func FormatSection(results []ProbeResult, osName, shellPath string, overrides map[string]string) string {
	if len(results) == 0 && len(overrides) == 0 && osName == "" && shellPath == "" {
		return ""
	}
	results = append([]ProbeResult(nil), results...)
	sortResults(results)
	var b strings.Builder
	b.WriteString("## Environment\n\n")
	if osName == "" {
		osName = runtime.GOOS + "/" + runtime.GOARCH
	}
	b.WriteString("- OS: " + osName + "\n")
	if shellPath != "" {
		b.WriteString("- Shell: " + redactHome(shellPath) + "\n")
	}
	if len(overrides) > 0 {
		b.WriteString("\nConfigured tools:\n")
		names := sortedMapKeys(overrides)
		for _, name := range limitStrings(names, maxRenderedTools) {
			fmt.Fprintf(&b, "- %s: %s\n", name, redactHome(overrides[name]))
		}
		if omitted := len(names) - maxRenderedTools; omitted > 0 {
			fmt.Fprintf(&b, "- ... %d more configured tools omitted\n", omitted)
		}
	}
	if len(results) > 0 {
		b.WriteString("\nDetected tools:\n")
		foundShown := 0
		foundTotal := 0
		for _, r := range results {
			if r.Found {
				foundTotal++
				if foundShown >= maxRenderedTools {
					continue
				}
				out := r.Output
				if out == "" {
					out = "available"
				}
				fmt.Fprintf(&b, "- %s: %s\n", r.Binary, out)
				foundShown++
			}
		}
		if omitted := foundTotal - foundShown; omitted > 0 {
			fmt.Fprintf(&b, "- ... %d more detected tools omitted\n", omitted)
		}
		b.WriteString("\nNot found or unavailable:\n")
		missingShown := 0
		missingTotal := 0
		for _, r := range results {
			if !r.Found {
				missingTotal++
				if missingShown >= maxRenderedTools {
					continue
				}
				reason := r.Error
				if reason == "" {
					reason = "not found"
				}
				fmt.Fprintf(&b, "- %s: %s\n", r.Binary, reason)
				missingShown++
			}
		}
		if omitted := missingTotal - missingShown; omitted > 0 {
			fmt.Fprintf(&b, "- ... %d more unavailable tools omitted\n", omitted)
		}
	}
	b.WriteString("\nUse detected tools when appropriate. Do not try unavailable tools unless the user installs or configures them.\n")
	return strings.TrimRight(b.String(), "\n")
}

func sortedMapKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		if strings.TrimSpace(k) != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

func limitStrings(in []string, limit int) []string {
	if len(in) <= limit {
		return in
	}
	return in[:limit]
}

func redactHome(path string) string {
	path = expandHome(path)
	if path == "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.ToSlash(filepath.Clean(path))
	}
	clean := filepath.Clean(path)
	home = filepath.Clean(home)
	if clean == home {
		return "~"
	}
	if strings.HasPrefix(clean, home+string(filepath.Separator)) {
		return filepath.ToSlash("~" + strings.TrimPrefix(clean, home))
	}
	return filepath.ToSlash(clean)
}

func expandHome(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || path == "~" || !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~/"))
}

func fileExecutable(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}

func blockedExecutable(path string, denyRoots []string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = filepath.Clean(path)
	}
	for _, root := range normalizedDenyRoots(denyRoots) {
		if pathWithin(abs, root) {
			return true
		}
	}
	return false
}

func normalizedDenyRoots(roots []string) []string {
	out := make([]string, 0, len(roots))
	seen := map[string]bool{}
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		root = expandHome(root)
		abs, err := filepath.Abs(root)
		if err != nil {
			abs = filepath.Clean(root)
		}
		abs = filepath.Clean(abs)
		if !seen[abs] {
			seen[abs] = true
			out = append(out, abs)
		}
	}
	sort.Strings(out)
	return out
}

func pathWithin(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	if path == root {
		return true
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != "." && rel != "" && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".."
}
