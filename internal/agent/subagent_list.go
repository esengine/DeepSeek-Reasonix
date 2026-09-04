package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	fileencoding "reasonix/internal/fileutil/encoding"
)

// maxListedSubagents bounds one listing. A conversation that spawned hundreds
// of children still gets a usable answer, and the newest are the ones a
// recovery is reaching for.
const maxListedSubagents = 50

// SubagentListTool enumerates the persisted children this conversation owns. A
// child's answer is addressed only by its reference, and a task or fleet that a
// restart interrupted never delivered the aggregate carrying those references —
// so its completed children sit on disk with nothing pointing at them. Re-running
// them pays their cost twice; this is how they are found instead.
type SubagentListTool struct {
	store         *SubagentStore
	workspaceRoot string
}

func NewSubagentListTool(task *TaskTool) *SubagentListTool {
	if task == nil {
		return &SubagentListTool{}
	}
	return &SubagentListTool{store: task.transcripts, workspaceRoot: task.workspaceRoot}
}

func (*SubagentListTool) Name() string { return "list_subagents" }

func (*SubagentListTool) Description() string {
	return "List the sub-agents this conversation owns, newest first, with the Subagent reference that read_subagent_result needs to page each full answer. Use it to recover work whose aggregate never arrived: a background task or fleet interrupted by a restart leaves its completed children persisted, and re-running them spends again what they already cost. Each row reports the child's state, its worker name, and the tool call that started it, so a fleet's items stay identifiable by position."
}

func (*SubagentListTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"status":{"type":"string","enum":["completed","failed","cancelled","interrupted","running"],"description":"Only list children in this state. Omit to list every state; completed is the only state read_subagent_result can read."}}}`)
}

func (*SubagentListTool) ReadOnly() bool { return true }

func (*SubagentListTool) PlanModeSafe() bool { return true }

func (t *SubagentListTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Status string `json:"status"`
	}
	dec := json.NewDecoder(bytes.NewReader(args))
	dec.DisallowUnknownFields()
	if len(bytes.TrimSpace(args)) > 0 {
		if err := dec.Decode(&p); err != nil {
			return "", fmt.Errorf("invalid args: %w", err)
		}
	}
	want := SubagentStatus(strings.TrimSpace(p.Status))
	switch want {
	case "", SubagentCompleted, SubagentFailed, SubagentCancelled, SubagentInterrupted, SubagentRunning:
	default:
		return "", fmt.Errorf("unknown status %q; want completed, failed, cancelled, interrupted, or running", p.Status)
	}
	if t == nil || t.store == nil {
		return "", fmt.Errorf("subagent transcript storage is not available in this session")
	}
	parentSession := ParentSession(ctx)
	if parentSession == "" {
		return "", fmt.Errorf("subagent listing requires a persisted parent session")
	}

	artifacts, err := t.store.ListForParent(parentSession, t.workspaceRoot)
	if err != nil {
		return "", err
	}
	return formatSubagentListing(artifacts, want), nil
}

func formatSubagentListing(artifacts []SubagentArtifact, want SubagentStatus) string {
	kept := make([]SubagentArtifact, 0, len(artifacts))
	for _, a := range artifacts {
		if want == "" || a.Meta.Status == want {
			kept = append(kept, a)
		}
	}
	if len(kept) == 0 {
		if want == "" {
			return "This conversation owns no persisted sub-agents."
		}
		return fmt.Sprintf("This conversation owns no persisted sub-agents in state %q.", want)
	}
	truncated := 0
	if len(kept) > maxListedSubagents {
		truncated = len(kept) - maxListedSubagents
		kept = kept[:maxListedSubagents]
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Sub-agents owned by this conversation (%d, newest first):\n", len(kept))
	for _, a := range kept {
		fmt.Fprintf(&b, "- %s  status=%s  name=%s", a.Ref, a.Meta.Status, boundedInline(a.Meta.Name, 60))
		if id := strings.TrimSpace(a.Meta.ParentToolCallID); id != "" {
			fmt.Fprintf(&b, "  from=%s", boundedInline(id, 80))
		}
		if a.Meta.Capsule.Inherited.HasUpstream() {
			b.WriteString("  started-from=upstream")
		}
		fmt.Fprintf(&b, "  updated=%s\n", a.Meta.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"))
	}
	if truncated > 0 {
		fmt.Fprintf(&b, "%d older sub-agent(s) not listed.\n", truncated)
	}
	b.WriteString("Read a completed child's full answer with read_subagent_result(ref=...).")
	return b.String()
}

// ListForParent returns the persisted children this conversation may read,
// newest first. Ownership follows ReadFinalAnswer's rule, so a recovery fork
// inherits its ancestor's children instead of losing sight of them. A record
// whose lineage cannot be proven is skipped rather than failing the listing:
// one odd sidecar must not hide every other child.
func (s *SubagentStore) ListForParent(parentSession, workspaceRoot string) ([]SubagentArtifact, error) {
	if s == nil {
		return nil, fmt.Errorf("subagent transcript storage is not available")
	}
	parentSession = strings.TrimSpace(parentSession)
	if parentSession == "" {
		return nil, fmt.Errorf("subagent listing requires a persisted parent session")
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	wantWorkspace := strings.TrimSpace(workspaceRoot)
	owned := map[string]bool{parentSession: true}
	var out []SubagentArtifact
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".meta.json") {
			continue
		}
		ref := strings.TrimSuffix(entry.Name(), ".meta.json")
		if !validSubagentRef(ref) {
			continue
		}
		meta, err := s.readListedMeta(ref, entry.Name())
		if err != nil {
			return nil, err
		}
		if meta == nil {
			continue
		}
		if wantWorkspace != "" && strings.TrimSpace(meta.WorkspaceRoot) != wantWorkspace {
			continue
		}
		if !s.ownedForListing(owned, strings.TrimSpace(meta.ParentSession), parentSession) {
			continue
		}
		out = append(out, SubagentArtifact{
			Ref:         ref,
			SessionPath: s.sessionPath(ref),
			MetaPath:    s.metaPath(ref),
			Meta:        *meta,
		})
	}
	slices.SortFunc(out, func(a, b SubagentArtifact) int {
		if !a.Meta.UpdatedAt.Equal(b.Meta.UpdatedAt) {
			return b.Meta.UpdatedAt.Compare(a.Meta.UpdatedAt)
		}
		return strings.Compare(b.Ref, a.Ref)
	})
	return out, nil
}

// readListedMeta returns nil for a sidecar this listing should pass over: an
// undecodable record is one child's problem, not the listing's.
func (s *SubagentStore) readListedMeta(ref, name string) (*SubagentMeta, error) {
	data, err := fileencoding.ReadFileUTF8(filepath.Join(s.dir, name))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("load subagent metadata %q: %w", ref, err)
	}
	var meta SubagentMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, nil
	}
	return &meta, nil
}

// ownedForListing memoizes the lineage answer per owner so a session with many
// children walks its ancestry once rather than once per record.
func (s *SubagentStore) ownedForListing(owned map[string]bool, owner, parentSession string) bool {
	if owner == "" {
		return false
	}
	if answer, known := owned[owner]; known {
		return answer
	}
	ok, err := s.isAncestorSession(owner, parentSession)
	answer := err == nil && ok
	owned[owner] = answer
	return answer
}
