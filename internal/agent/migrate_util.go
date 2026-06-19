package agent

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/provider"
)

// Shared utility functions used by all migration passes and the pipeline.

// legacyAssistantMsg is the minimal JSON shape needed to detect and transform
// the legacy nested-function tool-call format into the flat format the Go
// version expects.
type legacyAssistantMsg struct {
	Role      string          `json:"role"`
	ToolCalls json.RawMessage `json:"tool_calls"`
}

// legacyToolCallObj matches the OpenAI-style tool call where name and
// arguments live under a "function" key.
type legacyToolCallObj struct {
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

func readLegacyMeta(srcDir, base string) legacyMeta {
	var m legacyMeta
	b, err := os.ReadFile(filepath.Join(srcDir, base+".meta.json"))
	if err != nil {
		return m
	}
	_ = json.Unmarshal(b, &m)
	m.Workspace = strings.TrimSpace(m.Workspace)
	m.Summary = strings.TrimSpace(m.Summary)
	return m
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// isMessageFormat returns true when path's first non-whitespace bytes look like
// a JSON object with a "role" key (the v1+ message format) or a "schema_version"
// key (a v1+ header-prefixed session file), as opposed to the legacy event-log
// format whose first key is "id".
func isMessageFormat(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var buf [64]byte
	n, _ := f.Read(buf[:])
	s := strings.TrimLeft(string(buf[:n]), " \t\r\n")
	return strings.HasPrefix(s, `{"role":`) || strings.HasPrefix(s, `{"schema_version":`)
}

// recordImportedTitle stores the legacy summary as the session's display title
// in the dir's .titles.json — the same map the desktop sidebar reads
// (desktop/sessions.go). Existing titles are never overwritten.
func recordImportedTitle(destDir, base, summary string) {
	if summary == "" {
		return
	}
	path := filepath.Join(destDir, ".titles.json")
	titles := map[string]string{}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &titles)
	}
	key := base + ".jsonl"
	if titles[key] != "" {
		return
	}
	titles[key] = summary
	b, err := json.MarshalIndent(titles, "", "  ")
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

func importMarkerExists(destDir, marker string) bool {
	if strings.TrimSpace(destDir) == "" || strings.TrimSpace(marker) == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(destDir, marker))
	return err == nil
}

func writeImportMarkers(destDir string, markers ...string) {
	if strings.TrimSpace(destDir) == "" {
		return
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return
	}
	seen := map[string]bool{}
	for _, marker := range markers {
		marker = strings.TrimSpace(marker)
		if marker == "" || seen[marker] {
			continue
		}
		seen[marker] = true
		_ = os.WriteFile(filepath.Join(destDir, marker), nil, 0o644)
	}
}

// moveFlatImport re-homes a session the flat import left in the global dir.
// The legacy event log's mtime was stamped onto the imported file, so a match
// identifies it; a same-named native v1+ session never matches and stays put.
func moveFlatImport(oldPath, newPath string, srcInfo os.FileInfo) bool {
	if srcInfo == nil {
		return false
	}
	info, err := os.Stat(oldPath)
	if err != nil {
		return false
	}
	d := info.ModTime().Sub(srcInfo.ModTime())
	if d < -2*time.Second || d > 2*time.Second {
		return false
	}
	if err := os.MkdirAll(filepath.Dir(newPath), 0o755); err != nil {
		return false
	}
	return os.Rename(oldPath, newPath) == nil
}

func copyBranchMetaSidecar(srcPath, dstPath string) {
	b, err := os.ReadFile(BranchMetaPath(srcPath))
	if err != nil {
		return
	}
	dstMeta := BranchMetaPath(dstPath)
	if err := os.MkdirAll(filepath.Dir(dstMeta), 0o755); err != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(dstMeta), ".branch.*.tmp")
	if err != nil {
		return
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return
	}
	if err := os.Rename(tmpPath, dstMeta); err != nil {
		os.Remove(tmpPath)
	}
}

func copySubagentArtifacts(srcSessionDir, dstSessionDir, parentSession string) error {
	if sameDirPath(srcSessionDir, dstSessionDir) {
		return nil
	}
	artifacts, err := ListSubagentsByParent(srcSessionDir, parentSession)
	if err != nil {
		return err
	}
	var errs []error
	dstSubagentDir := filepath.Join(dstSessionDir, "subagents")
	for _, artifact := range artifacts {
		for _, src := range []string{artifact.SessionPath, artifact.MetaPath} {
			if err := copyFileIfExists(src, filepath.Join(dstSubagentDir, filepath.Base(src))); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func copyFileIfExists(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.IsDir() {
		return nil
	}
	if _, err := os.Stat(dst); err == nil {
		return nil
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".subagent.*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		os.Remove(tmpPath)
		return err
	}
	_ = os.Chtimes(dst, info.ModTime(), info.ModTime())
	return nil
}

// sameDirPath reports whether two directory paths resolve to the same location.
func sameDirPath(a, b string) bool {
	ca, cb := filepath.Clean(a), filepath.Clean(b)
	if ca == cb {
		return true
	}
	if aa, err := filepath.Abs(ca); err == nil {
		if bb, err := filepath.Abs(cb); err == nil {
			return aa == bb
		}
	}
	return false
}

// reconstructSession folds the chronological event stream into the provider
// message sequence. Tool results inherit their tool name from the assistant turn
// that issued the call (the v0.x result event carries only the call id).
func reconstructSession(path string) ([]provider.Message, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var msgs []provider.Message
	toolName := map[string]string{}
	dec := json.NewDecoder(f)
	for {
		var e legacyEvent
		if err := dec.Decode(&e); err != nil {
			if !errors.Is(err, io.EOF) {
				return msgs, nil // malformed tail — keep what parsed cleanly
			}
			break
		}
		switch e.Type {
		case "user.message":
			if e.Text != "" {
				msgs = append(msgs, provider.Message{Role: provider.RoleUser, Content: e.Text})
			}
		case "model.final":
			m := provider.Message{Role: provider.RoleAssistant, Content: e.Content, ReasoningContent: e.ReasoningContent}
			for _, tc := range e.ToolCalls {
				m.ToolCalls = append(m.ToolCalls, provider.ToolCall{ID: tc.ID, Name: tc.Function.Name, Arguments: tc.Function.Arguments})
				toolName[tc.ID] = tc.Function.Name
			}
			msgs = append(msgs, m)
		case "tool.result":
			msgs = append(msgs, provider.Message{Role: provider.RoleTool, ToolCallID: e.CallID, Name: toolName[e.CallID], Content: e.Output})
		}
	}
	return msgs, nil
}
