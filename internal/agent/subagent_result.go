package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"reasonix/internal/provider"
)

const (
	// Leave room for the generic tool-result guard to add metadata without
	// clipping a task manifest. The aggregate itself owns fair per-task preview
	// allocation so every completed child keeps its status and retrieval ref.
	subagentAggregateBudgetBytes = maxToolOutputBytes - 512
	subagentResultDefaultBytes   = 12 * 1024
	subagentResultMaxBytes       = 24 * 1024
)

// SubagentResultTool pages through the final answer of a completed persisted
// sub-agent. Parallel/fleet aggregates use this stable reader instead of
// forcing every child answer through one fixed-size tool result.
type SubagentResultTool struct {
	store         *SubagentStore
	workspaceRoot string
}

func NewSubagentResultTool(task *TaskTool) *SubagentResultTool {
	if task == nil {
		return &SubagentResultTool{}
	}
	return &SubagentResultTool{store: task.transcripts, workspaceRoot: task.workspaceRoot}
}

func (*SubagentResultTool) Name() string { return "read_subagent_result" }

func (*SubagentResultTool) Description() string {
	return "Read a completed sub-agent's full final answer by the Subagent reference returned from task, parallel_tasks, or fleet. Results are scoped to the current conversation lineage and paged by UTF-8 byte offset so large answers remain lossless without overflowing one tool result."
}

func (*SubagentResultTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"ref":{"type":"string","description":"The sa_... value from a Subagent reference line."},"offset_bytes":{"type":"integer","description":"UTF-8 byte offset to start reading from. Omit for the beginning; use next_offset_bytes from the previous page.","minimum":0},"limit_bytes":{"type":"integer","description":"Maximum UTF-8 bytes to return. Defaults to 12288 and is capped at 24576.","minimum":1,"maximum":24576}},"required":["ref"]}`)
}

func (*SubagentResultTool) ReadOnly() bool { return true }

func (*SubagentResultTool) PlanModeSafe() bool { return true }

func (t *SubagentResultTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Ref         string `json:"ref"`
		OffsetBytes int    `json:"offset_bytes"`
		LimitBytes  int    `json:"limit_bytes"`
	}
	dec := json.NewDecoder(bytes.NewReader(args))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	p.Ref = strings.TrimSpace(p.Ref)
	if p.Ref == "" {
		return "", fmt.Errorf("ref is required")
	}
	if p.OffsetBytes < 0 {
		return "", fmt.Errorf("offset_bytes must be non-negative")
	}
	if p.LimitBytes == 0 {
		p.LimitBytes = subagentResultDefaultBytes
	}
	if p.LimitBytes < 1 || p.LimitBytes > subagentResultMaxBytes {
		return "", fmt.Errorf("limit_bytes must be between 1 and %d", subagentResultMaxBytes)
	}
	if t == nil || t.store == nil {
		return "", fmt.Errorf("subagent result storage is not available in this session")
	}
	parentSession := ParentSession(ctx)
	if parentSession == "" {
		return "", fmt.Errorf("subagent result retrieval requires a persisted parent session")
	}

	answer, status, err := t.store.ReadFinalAnswer(p.Ref, parentSession, t.workspaceRoot)
	if err != nil {
		return "", err
	}
	if p.OffsetBytes > len(answer) {
		return "", fmt.Errorf("offset_bytes %d exceeds result size %d", p.OffsetBytes, len(answer))
	}
	if p.OffsetBytes < len(answer) && !utf8.RuneStart(answer[p.OffsetBytes]) {
		return "", fmt.Errorf("offset_bytes %d is not at a UTF-8 character boundary; use next_offset_bytes from the previous page", p.OffsetBytes)
	}
	end := min(p.OffsetBytes+p.LimitBytes, len(answer))
	for end > p.OffsetBytes && end < len(answer) && !utf8.RuneStart(answer[end]) {
		end--
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Subagent result %s (status=%s, bytes %d-%d of %d):\n", p.Ref, status, p.OffsetBytes, end, len(answer))
	b.WriteString(answer[p.OffsetBytes:end])
	if end < len(answer) {
		fmt.Fprintf(&b, "\n\nMore remains. Call read_subagent_result with ref=%q and offset_bytes=%d.", p.Ref, end)
	} else {
		b.WriteString("\n\nEnd of subagent result.")
	}
	return b.String(), nil
}

// ReadFinalAnswer returns a completed child answer only when the caller owns
// the parent conversation (or a verified descendant) and the workspace still
// matches. The per-ref lock prevents a read racing the terminal transcript save.
func (s *SubagentStore) ReadFinalAnswer(ref, parentSession, workspaceRoot string) (string, SubagentStatus, error) {
	if s == nil {
		return "", "", fmt.Errorf("subagent result storage is not available")
	}
	ref = strings.TrimSpace(ref)
	parentSession = strings.TrimSpace(parentSession)
	if parentSession == "" {
		return "", "", fmt.Errorf("subagent result parent session is required")
	}
	release, err := s.lock(ref)
	if err != nil {
		return "", "", err
	}
	defer release()

	meta, err := s.LoadMeta(ref)
	if err != nil {
		return "", "", err
	}
	owner := strings.TrimSpace(meta.ParentSession)
	if owner != parentSession {
		ok, lineageErr := s.isAncestorSession(owner, parentSession)
		if lineageErr != nil {
			return "", meta.Status, fmt.Errorf("subagent reference %q ownership could not be verified: %w", ref, lineageErr)
		}
		if !ok {
			return "", meta.Status, fmt.Errorf("subagent reference %q does not belong to the current conversation lineage", ref)
		}
	}
	if want := strings.TrimSpace(workspaceRoot); want != "" && strings.TrimSpace(meta.WorkspaceRoot) != want {
		return "", meta.Status, fmt.Errorf("subagent reference %q belongs to a different workspace", ref)
	}
	if meta.Status != SubagentCompleted {
		return "", meta.Status, fmt.Errorf("subagent reference %q is %s; only completed results can be read", ref, meta.Status)
	}

	sess, err := LoadSession(s.sessionPath(ref))
	if err != nil {
		return "", meta.Status, fmt.Errorf("load subagent transcript %q: %w", ref, err)
	}
	msgs := sess.Snapshot()
	for _, v := range slices.Backward(msgs) {
		if v.Role == provider.RoleAssistant && strings.TrimSpace(v.Content) != "" {
			return v.Content, meta.Status, nil
		}
	}
	return "", meta.Status, fmt.Errorf("subagent reference %q has no final assistant answer", ref)
}

type subagentAggregateItem struct {
	header string
	status string
	answer string
	ref    string
	detail string
}

func formatBoundedSubagentAggregate(prefix string, items []subagentAggregateItem) string {
	// Attestations are reserved before prose gets any budget: a long child
	// answer must never truncate away what the host saw it change. They
	// degrade to header plus violations only if they would starve previews.
	prose := make([]string, len(items))
	receipts := make([]string, len(items))
	receiptBytes := 0
	for i, item := range items {
		prose[i], receipts[i] = splitHostReceipts(item.answer)
		receiptBytes += len(receipts[i]) + 1
	}
	if reserve := subagentAggregateBudgetBytes / 2; receiptBytes > reserve && len(items) > 0 {
		receiptBytes = 0
		for i := range receipts {
			receipts[i] = boundedHostReceipts(receipts[i], reserve/len(items))
			receiptBytes += len(receipts[i]) + 1
		}
	}

	baseBytes := len(prefix) + receiptBytes
	completed := 0
	for i, item := range items {
		baseBytes += len(item.header) + len(item.status) + len(item.detail)
		if item.ref != "" {
			baseBytes += len("Subagent reference: \n") + len(item.ref)
		}
		if prose[i] != "" {
			baseBytes += len("Final answer preview:\n\n")
			completed++
		}
	}
	available := max(subagentAggregateBudgetBytes-baseBytes, 0)
	perAnswer := 0
	if completed > 0 {
		perAnswer = available / completed
	}

	var b strings.Builder
	b.Grow(minInt(subagentAggregateBudgetBytes, baseBytes+available))
	b.WriteString(prefix)
	for i, item := range items {
		b.WriteString(item.header)
		b.WriteString(item.status)
		if item.ref != "" {
			fmt.Fprintf(&b, "Subagent reference: %s\n", item.ref)
		}
		if item.detail != "" {
			b.WriteString(item.detail)
		}
		if prose[i] != "" {
			b.WriteString("Final answer preview:\n")
			b.WriteString(subagentAnswerPreview(prose[i], item.ref, perAnswer))
			b.WriteByte('\n')
		}
		if receipts[i] != "" {
			b.WriteString(receipts[i])
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func subagentAnswerPreview(answer, ref string, limit int) string {
	answer = strings.TrimSpace(answer)
	if len(answer) <= limit {
		return answer
	}
	if limit <= 0 {
		return ""
	}
	marker := "\n…[preview truncated; full result unavailable in this ephemeral run]…\n"
	if ref != "" {
		marker = fmt.Sprintf("\n…[preview truncated; read the full result with read_subagent_result(ref=%q)]…\n", ref)
	}
	if len(marker) >= limit {
		return utf8Prefix(answer, limit)
	}
	keep := limit - len(marker)
	headBytes := keep / 2
	tailBytes := keep - headBytes
	head := utf8Prefix(answer, headBytes)
	tail := utf8Suffix(answer, tailBytes)
	return head + marker + tail
}

func utf8Prefix(s string, limit int) string {
	if limit >= len(s) {
		return s
	}
	if limit <= 0 {
		return ""
	}
	for limit > 0 && !utf8.RuneStart(s[limit]) {
		limit--
	}
	return s[:limit]
}

func utf8Suffix(s string, limit int) string {
	if limit >= len(s) {
		return s
	}
	if limit <= 0 {
		return ""
	}
	start := len(s) - limit
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return s[start:]
}

func boundedInline(s string, limit int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= limit {
		return s
	}
	if limit <= len("…") {
		return utf8Prefix(s, limit)
	}
	return utf8Prefix(s, limit-len("…")) + "…"
}

func splitSubagentRunResult(output string) (answer, ref string) {
	ref = extractSubagentRef(output)
	if ref == "" {
		return strings.TrimSpace(output), ""
	}
	const marker = "\n\nFinal answer:\n"
	if _, after, ok := strings.Cut(output, marker); ok {
		return strings.TrimSpace(after), ref
	}
	return strings.TrimSpace(output), ref
}

func extractSubagentRef(output string) string {
	const prefix = "Subagent reference: "
	if !strings.HasPrefix(output, prefix) {
		return ""
	}
	line := output
	if end := strings.IndexByte(line, '\n'); end >= 0 {
		line = line[:end]
	}
	ref := strings.TrimSpace(strings.TrimPrefix(line, prefix))
	if validSubagentRef(ref) {
		return ref
	}
	return ""
}
