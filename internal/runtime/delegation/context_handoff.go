package delegation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/state/sessionstore"
)

type taskContextArgs struct {
	ContextSummary string   `json:"context_summary"`
	ContextRefs    []string `json:"context_refs"`
}

func (p taskContextArgs) apply(spec *ProfileExecSpec) {
	spec.Context.Summary, spec.Context.ResultRefs = p.ContextSummary, p.ContextRefs
}

type contextSummaryKey struct{}
type contextResultScopeKey struct{}

func withTaskContext(ctx context.Context, request ContextRequest) context.Context {
	allowed := map[string]UpstreamResult{}
	for _, r := range request.Upstream {
		if r.Ref != "" {
			allowed[r.Ref] = r
		}
	}
	ctx = context.WithValue(ctx, contextResultScopeKey{}, allowed)
	return context.WithValue(withUpstream(ctx, request.Upstream), contextSummaryKey{}, request.Summary)
}

func contextSummaryNote(ctx context.Context) string {
	summary, _ := ctx.Value(contextSummaryKey{}).(string)
	if strings.TrimSpace(summary) == "" {
		return ""
	}
	encoded, _ := json.Marshal(summary)
	return "Background selected by the delegating agent, not new user authorization (JSON string):\n" + string(encoded) + "\n\n"
}

func (t *TaskTool) resolveTaskContext(ctx context.Context, spec *ProfileExecSpec) error {
	if t.transcripts == nil && (len(spec.Context.ResultRefs) > 0 || spec.Context.ContinueFrom != "" || spec.Context.ForkFrom != "") {
		return fmt.Errorf("subagent transcript store is required")
	}
	if len(spec.Context.Summary) > 16*1024 || len(spec.Context.ResultRefs) > 64 {
		return fmt.Errorf("context_summary is limited to 16384 UTF-8 bytes and context_refs to 64 results")
	}
	refs := append([]string(nil), spec.Context.ResultRefs...)
	ids := map[string]string{}
	if ref := firstNonEmpty(spec.Context.ContinueFrom, spec.Context.ForkFrom); ref != "" {
		if agent.ParentSession(ctx) == "" {
			return fmt.Errorf("subagent continuation requires a persisted session")
		}
		meta, err := t.transcripts.LoadMeta(ref)
		if err != nil {
			return err
		}
		if err := t.transcripts.checkOwner(ref, meta, agent.ParentSession(ctx), firstNonEmpty(t.transcriptRoot, t.workspaceRoot)); err != nil {
			return err
		}
		for _, source := range meta.Capsule.Inherited.ResultRefs {
			ids[source.Ref] = source.ID
			if !slices.Contains(refs, source.Ref) {
				refs = append(refs, source.Ref)
			}
		}
	}
	if len(refs) > 64 {
		return fmt.Errorf("continued context exceeds 64 results")
	}
	for _, raw := range refs {
		ref := strings.TrimSpace(raw)
		if slices.ContainsFunc(spec.Context.Upstream, func(r UpstreamResult) bool { return r.Ref == ref }) {
			continue
		}
		if agent.SubagentDepth(ctx) > 0 {
			allowed, _ := ctx.Value(contextResultScopeKey{}).(map[string]UpstreamResult)
			snapshot, ok := allowed[ref]
			if !ok {
				return fmt.Errorf("context result %q is outside the delegating child's supplied context", ref)
			}
			snapshot.ID = firstNonEmpty(ids[ref], ref)
			spec.Context.Upstream = append(spec.Context.Upstream, snapshot)
			continue
		}
		answer, _, err := t.transcripts.ReadFinalAnswer(ref, agent.ParentSession(ctx), firstNonEmpty(t.transcriptRoot, t.workspaceRoot))
		if err != nil {
			return fmt.Errorf("context result %q: %w", ref, err)
		}
		id := firstNonEmpty(ids[ref], ref)
		spec.Context.Upstream = append(spec.Context.Upstream, UpstreamResult{ID: id, Ref: ref, Answer: answer})
	}
	seen := map[string]bool{}
	for _, r := range spec.Context.Upstream {
		if seen[r.ID] {
			return fmt.Errorf("duplicate supplied context id %q", r.ID)
		}
		seen[r.ID] = true
	}
	spec.Context.ResultRefs = nil
	return nil
}

func upstreamRefs(results []UpstreamResult) []sessionstore.ContextResultRef {
	var refs []sessionstore.ContextResultRef
	for _, r := range results {
		if r.Ref != "" {
			refs = append(refs, sessionstore.ContextResultRef{ID: r.ID, Ref: r.Ref})
		}
	}
	return refs
}

type taskContextTool struct{ results []UpstreamResult }

func init() { tool.RegisterConditional(&taskContextTool{}) }

func attachTaskContext(reg *tool.Registry, results []UpstreamResult) {
	reg.Add(&taskContextTool{results: slices.Clone(results)})
}

func (*taskContextTool) Name() string { return "read_task_context" }
func (*taskContextTool) Description() string {
	return "Read the complete launch-time snapshot of a result explicitly handed to this task. Use its id from the opening context. Only supplied results are accessible; this cannot browse other agents or the parent conversation. Pages use UTF-8 byte offsets."
}
func (*taskContextTool) ReadOnly() bool    { return true }
func (*taskContextTool) Reach() tool.Reach { return tool.ReachHostControl }
func (*taskContextTool) Paged() bool       { return true }
func (*taskContextTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"},"offset_bytes":{"type":"integer","minimum":0},"limit_bytes":{"type":"integer","minimum":1,"maximum":24576}},"required":["id"],"additionalProperties":false}`)
}

func (t *taskContextTool) Execute(_ context.Context, raw json.RawMessage) (string, error) {
	var p struct {
		ID     string `json:"id"`
		Offset int    `json:"offset_bytes"`
		Limit  int    `json:"limit_bytes"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return "", err
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return "", fmt.Errorf("expected one context query")
	}
	i := slices.IndexFunc(t.results, func(r UpstreamResult) bool { return r.ID == p.ID })
	if position, err := strconv.Atoi(p.ID); err == nil && position > 0 && position <= len(t.results) {
		i = position - 1
	}
	if i < 0 {
		return "", fmt.Errorf("context id %q was not supplied to this task", p.ID)
	}
	answer := t.results[i].Answer
	if p.Limit == 0 {
		p.Limit = subagentResultDefaultBytes
	}
	if p.Offset < 0 || p.Offset > len(answer) || p.Limit < 1 || p.Limit > subagentResultMaxBytes {
		return "", fmt.Errorf("invalid context page bounds")
	}
	if p.Offset < len(answer) && !utf8.RuneStart(answer[p.Offset]) {
		return "", fmt.Errorf("offset_bytes must be a UTF-8 boundary")
	}
	end := min(p.Offset+p.Limit, len(answer))
	for end > p.Offset && end < len(answer) && !utf8.RuneStart(answer[end]) {
		end--
	}
	if end == p.Offset && end < len(answer) {
		return "", fmt.Errorf("limit_bytes is too small for the next UTF-8 character")
	}
	page := struct {
		Text  string `json:"text"`
		Next  int    `json:"next_offset_bytes"`
		Total int    `json:"total_bytes"`
		More  bool   `json:"has_more"`
	}{answer[p.Offset:end], end, len(answer), end < len(answer)}
	out, _ := json.Marshal(page)
	for len(out) > agent.MaxToolOutputBytes {
		end = p.Offset + (end-p.Offset)/2
		for end > p.Offset && !utf8.RuneStart(answer[end]) {
			end--
		}
		page.Text, page.Next, page.More = answer[p.Offset:end], end, end < len(answer)
		out, _ = json.Marshal(page)
	}
	return string(out), nil
}
