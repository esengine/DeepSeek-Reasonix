package doctor

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/fileutil"
	"reasonix/internal/provider"
	"reasonix/internal/secrets"
	"reasonix/internal/store"
)

// RedactSessionsOptions controls historical session-log redaction.
type RedactSessionsOptions struct {
	Dirs   []string
	DryRun bool
}

// RedactSessionsResult summarizes a historical session-log redaction run.
type RedactSessionsResult struct {
	Dirs           []string `json:"dirs"`
	FilesScanned   int64    `json:"files_scanned"`
	FilesChanged   int64    `json:"files_changed"`
	FilesSkipped   int64    `json:"files_skipped"`
	BytesRewritten int64    `json:"bytes_rewritten"`
	DryRun         bool     `json:"dry_run"`
	Errors         []string `json:"errors,omitempty"`
}

// RedactSessions masks credential-shaped values already persisted in Reasonix
// session transcripts, event logs, branch metadata, goal state, and
// background-job artifacts. It is intentionally scoped to known Reasonix
// session directories; it is not a general-purpose filesystem scrubber.
//
// Every JSON-bearing artifact is decoded before masking and re-encoded after:
// running Redact over raw encoded bytes would eat the backslash of a \" escape
// whenever a secret-shaped value abuts a quote, truncating the JSON string and
// leaving the transcript undecodable (and the secret unmasked). Only plain-text
// job logs are redacted as raw bytes.
func RedactSessions(opts RedactSessionsOptions) RedactSessionsResult {
	dirs := redactSessionDirs(opts.Dirs)
	res := RedactSessionsResult{Dirs: dirs, DryRun: opts.DryRun}
	for _, dir := range dirs {
		if err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", path, err))
				if d != nil && d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() || !redactSessionCandidate(path) {
				return nil
			}
			res.FilesScanned++
			if sessionPath := redactionSessionPath(path); sessionPath != "" && sessionRedactionLeaseHeld(sessionPath) {
				res.FilesSkipped++
				return nil
			}
			changed, rewritten, err := redactSessionArtifact(path, opts.DryRun)
			if err != nil {
				res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", path, err))
				return nil
			}
			res.FilesChanged += changed
			res.BytesRewritten += rewritten
			return nil
		}); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", dir, err))
		}
	}
	return res
}

func redactSessionDirs(in []string) []string {
	var candidates []string
	if len(in) > 0 {
		candidates = append(candidates, in...)
	} else {
		candidates = append(candidates, sessionBundleSearchDirs()...)
	}
	seen := map[string]bool{}
	var out []string
	for _, dir := range candidates {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		if abs, err := filepath.Abs(dir); err == nil {
			dir = abs
		}
		dir = filepath.Clean(dir)
		if seen[dir] {
			continue
		}
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			continue
		}
		seen[dir] = true
		out = append(out, dir)
	}
	sort.Strings(out)
	return out
}

func redactSessionCandidate(path string) bool {
	name := filepath.Base(path)
	switch {
	case store.IsSessionTranscriptName(name):
		return true
	case strings.HasSuffix(name, ".jsonl.meta"):
		return true
	case strings.HasSuffix(name, ".events.jsonl"):
		return true
	case strings.HasSuffix(name, ".guardian.jsonl"):
		return true
	case strings.HasSuffix(name, ".goal-state.json"):
		return true
	case filepath.Base(filepath.Dir(path)) != "" && strings.HasSuffix(filepath.Base(filepath.Dir(path)), ".jobs"):
		return strings.HasSuffix(name, ".log") || strings.HasSuffix(name, ".json")
	default:
		return false
	}
}

func redactionSessionPath(path string) string {
	name := filepath.Base(path)
	switch {
	case store.IsSessionTranscriptName(name), strings.HasSuffix(name, ".guardian.jsonl"):
		return path
	case strings.HasSuffix(path, ".jsonl.meta"):
		return strings.TrimSuffix(path, ".meta")
	case strings.HasSuffix(path, ".events.jsonl"):
		return strings.TrimSuffix(path, ".events.jsonl") + ".jsonl"
	case strings.HasSuffix(path, ".goal-state.json"):
		return strings.TrimSuffix(path, ".goal-state.json") + ".jsonl"
	case strings.HasSuffix(filepath.Base(filepath.Dir(path)), ".jobs"):
		return strings.TrimSuffix(filepath.Dir(path), ".jobs") + ".jsonl"
	default:
		return ""
	}
}

func sessionRedactionLeaseHeld(sessionPath string) bool {
	if agent.SessionLeaseHeld(sessionPath) {
		return true
	}
	if strings.HasSuffix(sessionPath, ".guardian.jsonl") {
		parent := strings.TrimSuffix(sessionPath, ".guardian.jsonl") + ".jsonl"
		return agent.SessionLeaseHeld(parent)
	}
	return false
}

// redactSessionArtifact dispatches one candidate file to a format-aware
// redactor and reports how many files it changed.
func redactSessionArtifact(path string, dryRun bool) (changed int64, bytesRewritten int64, err error) {
	name := filepath.Base(path)
	switch {
	case store.IsSessionTranscriptName(name), strings.HasSuffix(name, ".guardian.jsonl"):
		return redactSessionTranscript(path, dryRun)
	case strings.HasSuffix(name, ".events.jsonl"):
		anchor := strings.TrimSuffix(path, ".events.jsonl") + ".jsonl"
		if _, statErr := os.Stat(anchor); statErr == nil {
			// The anchor's own walk entry rewrites the event log with it.
			return 0, 0, nil
		}
		return redactSessionTranscript(anchor, dryRun)
	case strings.HasSuffix(name, ".jsonl.meta"):
		return redactBranchMeta(strings.TrimSuffix(path, ".meta"), dryRun)
	case strings.HasSuffix(name, ".goal-state.json"):
		return redactJSONFile(path, dryRun)
	case strings.HasSuffix(name, ".json"):
		return redactJSONFile(path, dryRun)
	default:
		// Background-job .log files are plain text: raw-byte redaction is
		// correct there and only there.
		return redactPlainTextFile(path, dryRun)
	}
}

// redactSessionTranscript rewrites one session (anchor .jsonl plus its event
// log) through the agent's own save machinery. Session.Save re-runs
// RedactMessages on the snapshot, folds the event log into a single redacted
// replace event, refreshes the anchor and event index, and records the
// revision under the same cross-process file locks live sessions use — so
// digests, the CAS ledger, and event-log replay stay coherent, and a rerun is
// a no-op because Redact is idempotent on decoded content.
func redactSessionTranscript(path string, dryRun bool) (int64, int64, error) {
	s, err := agent.LoadSession(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, nil
		}
		return 0, 0, err
	}
	files := int64(1)
	eventLog := store.SessionEventLog(path)
	eventLogExists := false
	if _, err := os.Stat(eventLog); err == nil {
		files++
		eventLogExists = true
	}
	// The replayed view alone is not enough: a replace event supersedes
	// earlier records without erasing them, so a raw secret can survive in a
	// stale event while the current messages are already clean. Scan every
	// record; Session.Save compacts the whole log into a single clean replace
	// event, which erases the stale bytes.
	if !messagesNeedRedaction(s.Messages) && !(eventLogExists && eventLogNeedsRedaction(eventLog)) {
		return 0, 0, nil
	}
	if dryRun {
		return files, redactedEncodedSize(s.Messages), nil
	}
	if err := s.Save(path); err != nil {
		return 0, 0, err
	}
	var rewritten int64
	if info, err := os.Stat(path); err == nil {
		rewritten += info.Size()
	}
	if info, err := os.Stat(eventLog); err == nil {
		rewritten += info.Size()
	}
	return files, rewritten, nil
}

// eventLogNeedsRedaction reports whether any event record — including ones a
// later replace event superseded — still carries redactable message content.
// Undecodable trailing bytes also count: a torn tail can hold raw secret text,
// and the compaction that a rewrite performs erases it either way.
func eventLogNeedsRedaction(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	for {
		var rec struct {
			Messages []provider.Message `json:"messages"`
		}
		if err := dec.Decode(&rec); err != nil {
			// EOF is a clean end; anything else is an undecodable tail whose
			// torn bytes may hold raw secret text — compact it away.
			return !errors.Is(err, io.EOF)
		}
		if messagesNeedRedaction(rec.Messages) {
			return true
		}
	}
}

// messagesNeedRedaction reports whether RedactMessages would alter the
// storage encoding of msgs. Comparing encoded forms (not struct equality)
// matches exactly what a rewrite would put on disk.
func messagesNeedRedaction(msgs []provider.Message) bool {
	redacted := secrets.RedactMessages(msgs)
	for i := range msgs {
		before, errB := json.Marshal(msgs[i])
		after, errA := json.Marshal(redacted[i])
		if errB != nil || errA != nil || !bytes.Equal(before, after) {
			return true
		}
	}
	return false
}

func redactedEncodedSize(msgs []provider.Message) int64 {
	var n int64
	for _, m := range secrets.RedactMessages(msgs) {
		if b, err := json.Marshal(m); err == nil {
			n += int64(len(b)) + 1
		}
	}
	return n
}

// redactBranchMeta masks the free-text fields of the branch-metadata sidecar
// (preview, titles, goal, recovery reason) through the typed load/save pair so
// revisions, digests, and timestamps survive untouched.
func redactBranchMeta(sessionPath string, dryRun bool) (int64, int64, error) {
	unlock := agent.LockSessionMetaPath(sessionPath)
	defer unlock()
	meta, ok, err := agent.LoadBranchMeta(sessionPath)
	if err != nil || !ok {
		return 0, 0, err
	}
	changed := false
	for _, field := range []*string{&meta.Name, &meta.TopicTitle, &meta.CustomTitle, &meta.Goal, &meta.Preview, &meta.RecoveryReason} {
		if masked := secrets.Redact(*field); masked != *field {
			*field = masked
			changed = true
		}
	}
	if !changed {
		return 0, 0, nil
	}
	if dryRun {
		return 1, 0, nil
	}
	if err := agent.SaveBranchMetaPreserveUpdated(sessionPath, meta); err != nil {
		return 0, 0, err
	}
	var rewritten int64
	if info, err := os.Stat(agent.BranchMetaPath(sessionPath)); err == nil {
		rewritten = info.Size()
	}
	return 1, rewritten, nil
}

// redactJSONFile decodes a single-document JSON sidecar, masks every string
// value in the tree, and re-encodes. UseNumber keeps numeric literals (large
// IDs, timestamps) byte-faithful through the round trip. A file that does not
// parse is reported and left untouched rather than risked with a raw rewrite.
func redactJSONFile(path string, dryRun bool) (int64, int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, 0, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return 0, 0, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return 0, 0, fmt.Errorf("not valid JSON, left untouched: %w", err)
	}
	doc, changed := redactJSONValue(doc)
	if !changed {
		return 0, 0, nil
	}
	next, err := json.Marshal(doc)
	if err != nil {
		return 0, 0, err
	}
	next = append(next, '\n')
	if dryRun {
		return 1, int64(len(next)), nil
	}
	perm := info.Mode().Perm()
	if perm == 0 {
		perm = 0o600
	}
	if err := fileutil.AtomicWriteFile(path, next, perm); err != nil {
		return 0, 0, err
	}
	return 1, int64(len(next)), nil
}

func redactJSONValue(v any) (any, bool) {
	switch t := v.(type) {
	case string:
		masked := secrets.Redact(t)
		return masked, masked != t
	case map[string]any:
		changed := false
		for key, val := range t {
			next, ch := redactJSONValue(val)
			if ch {
				t[key] = next
				changed = true
			}
		}
		return t, changed
	case []any:
		changed := false
		for i, val := range t {
			next, ch := redactJSONValue(val)
			if ch {
				t[i] = next
				changed = true
			}
		}
		return t, changed
	default:
		return v, false
	}
}

// redactPlainTextFile masks raw bytes — safe only for non-JSON artifacts
// (background-job .log output).
func redactPlainTextFile(path string, dryRun bool) (int64, int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, 0, err
	}
	if info.IsDir() {
		return 0, 0, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, err
	}
	next := []byte(secrets.Redact(string(raw)))
	if bytes.Equal(raw, next) {
		return 0, 0, nil
	}
	if dryRun {
		return 1, int64(len(next)), nil
	}
	perm := info.Mode().Perm()
	if perm == 0 {
		perm = 0o600
	}
	if err := fileutil.AtomicWriteFile(path, next, perm); err != nil {
		return 0, 0, err
	}
	return 1, int64(len(next)), nil
}
