package doctor

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/store"
)

const sessionBundleSchemaVersion = 1

// SessionBundleOptions controls a session-conflict diagnostic zip. SessionRef
// accepts either a branch id such as "20260706-...-recovery-abcd1234", a
// matching .jsonl basename, or an absolute/relative transcript path.
type SessionBundleOptions struct {
	Version    string
	SessionRef string
	OutputPath string
	Now        time.Time
}

type SessionBundleResult struct {
	Path        string
	SessionPath string
	Included    []SessionBundleFile
	Missing     []string
}

type SessionBundleManifest struct {
	SchemaVersion int                  `json:"schema_version"`
	GeneratedAt   time.Time            `json:"generated_at"`
	Version       string               `json:"version"`
	RequestedRef  string               `json:"requested_ref"`
	SessionPath   string               `json:"session_path"`
	Sessions      []SessionBundleEntry `json:"sessions"`
	Included      []SessionBundleFile  `json:"included"`
	Missing       []string             `json:"missing,omitempty"`
}

type SessionBundleEntry struct {
	BranchID       string `json:"branch_id"`
	Role           string `json:"role"`
	Path           string `json:"path"`
	ParentID       string `json:"parent_id,omitempty"`
	Recovered      bool   `json:"recovered,omitempty"`
	RecoveryReason string `json:"recovery_reason,omitempty"`
	RecoveryDepth  int    `json:"recovery_depth,omitempty"`
}

type SessionBundleFile struct {
	Role string `json:"role"`
	Name string `json:"name"`
	Path string `json:"path"`
	Size int64  `json:"size"`
}

type sessionArtifact struct {
	role string
	path string
}

// WriteSessionBundle writes a zip containing the selected session transcript,
// its persistence sidecars, a redacted doctor report, and the recovery parent
// chain discoverable through branch metadata.
func WriteSessionBundle(opts SessionBundleOptions) (SessionBundleResult, error) {
	ref := strings.TrimSpace(opts.SessionRef)
	if ref == "" {
		return SessionBundleResult{}, fmt.Errorf("session branch id or path is required")
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	sessionPath, err := resolveSessionBundlePath(ref)
	if err != nil {
		return SessionBundleResult{}, err
	}
	chain := sessionBundleChain(sessionPath)
	outPath := strings.TrimSpace(opts.OutputPath)
	if outPath == "" {
		outPath = defaultSessionBundlePath(agent.BranchID(sessionPath))
	}
	if abs, err := filepath.Abs(outPath); err == nil {
		outPath = abs
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return SessionBundleResult{}, err
	}

	f, err := os.Create(outPath)
	if err != nil {
		return SessionBundleResult{}, err
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	manifest := SessionBundleManifest{
		SchemaVersion: sessionBundleSchemaVersion,
		GeneratedAt:   now,
		Version:       opts.Version,
		RequestedRef:  redactSessionBundleRef(ref),
		SessionPath:   redactSessionBundlePath(sessionPath),
	}
	result := SessionBundleResult{Path: outPath, SessionPath: sessionPath}

	report := Collect(Options{Version: opts.Version})
	reportData, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		_ = zw.Close()
		return SessionBundleResult{}, err
	}
	if err := addBundleBytes(zw, "doctor.json", reportData, now); err != nil {
		_ = zw.Close()
		return SessionBundleResult{}, err
	}

	for i, path := range chain {
		role := "parent"
		if i == 0 {
			role = "requested"
		}
		meta, ok, metaErr := agent.LoadBranchMeta(path)
		if metaErr != nil {
			metaPath := store.SessionMeta(path)
			manifest.Missing = append(manifest.Missing, redactSessionBundlePath(metaPath)+": "+redactSessionBundleError(metaErr))
		}
		entry := SessionBundleEntry{
			BranchID: agent.BranchID(path),
			Role:     role,
			Path:     redactSessionBundlePath(path),
		}
		if ok {
			entry.ParentID = meta.ParentID
			entry.Recovered = meta.Recovered
			entry.RecoveryReason = meta.RecoveryReason
			entry.RecoveryDepth = meta.RecoveryDepth
		}
		manifest.Sessions = append(manifest.Sessions, entry)

		for _, artifact := range sessionBundleArtifacts(path) {
			info, statErr := os.Stat(artifact.path)
			if statErr != nil {
				if !os.IsNotExist(statErr) {
					manifest.Missing = append(manifest.Missing, redactSessionBundlePath(artifact.path)+": "+redactSessionBundleError(statErr))
				}
				continue
			}
			if info.IsDir() {
				continue
			}
			zipName := filepath.ToSlash(filepath.Join("sessions", safeBundleName(agent.BranchID(path)), filepath.Base(artifact.path)))
			if err := addBundleFile(zw, zipName, artifact.path, info); err != nil {
				_ = zw.Close()
				return SessionBundleResult{}, err
			}
			file := SessionBundleFile{
				Role: artifact.role,
				Name: zipName,
				Path: redactSessionBundlePath(artifact.path),
				Size: info.Size(),
			}
			manifest.Included = append(manifest.Included, file)
			result.Included = append(result.Included, file)
		}
	}
	sort.Strings(manifest.Missing)
	result.Missing = append(result.Missing, manifest.Missing...)
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		_ = zw.Close()
		return SessionBundleResult{}, err
	}
	if err := addBundleBytes(zw, "manifest.json", manifestData, now); err != nil {
		_ = zw.Close()
		return SessionBundleResult{}, err
	}
	if err := zw.Close(); err != nil {
		return SessionBundleResult{}, err
	}
	return result, nil
}

func resolveSessionBundlePath(ref string) (string, error) {
	if path, ok := existingSessionPath(ref); ok {
		return path, nil
	}
	branch := sessionBundleBranchID(ref)
	if branch == "" {
		return "", fmt.Errorf("invalid session reference %q", ref)
	}
	name := branch + ".jsonl"
	var matches []string
	for _, dir := range sessionBundleSearchDirs() {
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			matches = append(matches, path)
		}
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("session %q not found under Reasonix session directories", branch)
	}
	sort.Strings(matches)
	if len(matches) > 1 {
		redacted := make([]string, len(matches))
		for i, match := range matches {
			redacted[i] = redactSessionBundlePath(match)
		}
		return "", fmt.Errorf("session %q matched multiple files; pass an absolute path: %s", branch, strings.Join(redacted, ", "))
	}
	return matches[0], nil
}

func redactSessionBundleRef(ref string) string {
	if strings.ContainsAny(ref, `/\`) {
		return redactSessionBundlePath(ref)
	}
	return ref
}

func redactSessionBundlePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return path
	}
	root := strings.TrimSpace(config.MemoryUserDir())
	if root != "" {
		absRoot, rootErr := filepath.Abs(root)
		absPath, pathErr := filepath.Abs(path)
		if rootErr == nil && pathErr == nil {
			if rel, err := filepath.Rel(absRoot, absPath); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
				if rel == "." {
					return "<REASONIX_HOME>"
				}
				return filepath.ToSlash(filepath.Join("<REASONIX_HOME>", rel))
			}
		}
	}
	return redactHome(path)
}

func redactSessionBundleError(err error) string {
	if err == nil {
		return ""
	}
	return redactSessionBundleText(err.Error())
}

func redactSessionBundleText(text string) string {
	if text == "" {
		return text
	}
	if root := strings.TrimSpace(config.MemoryUserDir()); root != "" {
		text = replacePathPrefix(text, root, "<REASONIX_HOME>")
		if absRoot, err := filepath.Abs(root); err == nil {
			text = replacePathPrefix(text, absRoot, "<REASONIX_HOME>")
		}
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		text = replacePathPrefix(text, home, "~")
	}
	return text
}

func replacePathPrefix(text, old, new string) string {
	old = strings.TrimRight(strings.TrimSpace(old), string(os.PathSeparator))
	if old == "" {
		return text
	}
	text = strings.ReplaceAll(text, old+string(os.PathSeparator), new+string(os.PathSeparator))
	return strings.ReplaceAll(text, old, new)
}

func existingSessionPath(ref string) (string, bool) {
	path := strings.Trim(strings.TrimSpace(ref), `"'`)
	if path == "" {
		return "", false
	}
	if !strings.HasSuffix(filepath.Base(path), ".jsonl") {
		if candidate := path + ".jsonl"; strings.ContainsAny(path, `/\`) {
			path = candidate
		}
	}
	if !strings.HasSuffix(filepath.Base(path), ".jsonl") {
		return "", false
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return "", false
	}
	return path, true
}

func sessionBundleBranchID(ref string) string {
	ref = strings.Trim(strings.TrimSpace(ref), `"'`)
	base := filepath.Base(ref)
	for _, suffix := range []string{".jsonl.meta", ".jsonl", ".meta", ".json"} {
		base = strings.TrimSuffix(base, suffix)
	}
	return strings.TrimSpace(base)
}

func sessionBundleSearchDirs() []string {
	seen := map[string]bool{}
	var out []string
	add := func(dir string) {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			return
		}
		if abs, err := filepath.Abs(dir); err == nil {
			dir = abs
		}
		if seen[dir] {
			return
		}
		seen[dir] = true
		out = append(out, dir)
	}
	add(config.SessionDir())
	if cwd, err := os.Getwd(); err == nil {
		add(config.ProjectSessionDir(cwd))
	}
	if root := config.MemoryUserDir(); root != "" {
		projects := filepath.Join(root, "projects")
		_ = filepath.WalkDir(projects, func(path string, d os.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return nil
			}
			if d.Name() == "sessions" {
				add(path)
				return filepath.SkipDir
			}
			return nil
		})
	}
	sort.Strings(out)
	return out
}

func sessionBundleChain(path string) []string {
	var out []string
	seen := map[string]bool{}
	for len(out) < 16 {
		if path == "" {
			break
		}
		if abs, err := filepath.Abs(path); err == nil {
			path = abs
		}
		if seen[path] {
			break
		}
		seen[path] = true
		out = append(out, path)
		meta, ok, err := agent.LoadBranchMeta(path)
		if err != nil || !ok || strings.TrimSpace(meta.ParentID) == "" {
			break
		}
		parent := filepath.Join(filepath.Dir(path), meta.ParentID+".jsonl")
		if info, err := os.Stat(parent); err != nil || info.IsDir() {
			break
		}
		path = parent
	}
	return out
}

func sessionBundleArtifacts(sessionPath string) []sessionArtifact {
	return []sessionArtifact{
		{role: "transcript", path: sessionPath},
		{role: "meta", path: store.SessionMeta(sessionPath)},
		{role: "goal_state", path: store.SessionGoalState(sessionPath)},
		{role: "events", path: store.SessionEventLog(sessionPath)},
		{role: "event_index", path: store.SessionEventIndex(sessionPath)},
		{role: "conflicts", path: store.SessionConflictLog(sessionPath)},
		{role: "lease", path: store.SessionLeaseInfo(sessionPath)},
	}
}

func addBundleFile(zw *zip.Writer, name, path string, info os.FileInfo) error {
	h, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	h.Name = name
	h.Method = zip.Deflate
	w, err := zw.CreateHeader(h)
	if err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(w, f)
	return err
}

func addBundleBytes(zw *zip.Writer, name string, data []byte, modTime time.Time) error {
	h := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: modTime}
	h.SetMode(0o644)
	w, err := zw.CreateHeader(h)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

func defaultSessionBundlePath(branch string) string {
	branch = safeBundleName(branch)
	if branch == "" {
		branch = "session"
	}
	return filepath.Join(os.TempDir(), "reasonix-session-"+branch+"-diag.zip")
}

func safeBundleName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
		if b.Len() >= 120 {
			break
		}
	}
	return strings.Trim(b.String(), "._-")
}
