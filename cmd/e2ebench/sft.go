// sft.go — turning recorded runs into supervised-fine-tuning samples: the
// request-side prefix a run header proves, plus the conversation it produced.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// sftSample is one training example: the prefix a run sampled against and the
// conversation that prefix produced.
type sftSample struct {
	TaskID   string          `json:"task_id"`
	ModelRef string          `json:"model_ref,omitempty"`
	Tools    json.RawMessage `json:"tools,omitempty"`
	Messages []sftMessage    `json:"messages"`
}

type sftMessage struct {
	Role       string        `json:"role"`
	Content    string        `json:"content"`
	Reasoning  string        `json:"reasoning_content,omitempty"`
	ToolCalls  []sftToolCall `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
}

type sftToolCall struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Function sftToolFunction `json:"function"`
}

type sftToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// sftRecord is the trajectory projection this export needs. It is separate
// from trajectoryRecord because that one deliberately omits tool output: the
// analysis passes would otherwise hold every result of every run in memory.
type sftRecord struct {
	RunHeader *sftRunHeader `json:"run_header"`
	Event     *struct {
		Kind      string `json:"kind"`
		Text      string `json:"text"`
		Reasoning string `json:"reasoning"`
		Usage     *struct {
			CacheDiagnostics *struct {
				PrefixHash string `json:"prefixHash"`
			} `json:"cacheDiagnostics"`
		} `json:"usage"`
		Tool *struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Args     string `json:"args"`
			Output   string `json:"output"`
			Err      string `json:"err"`
			Partial  bool   `json:"partial"`
			ParentID string `json:"parentId"`
		} `json:"tool"`
	} `json:"event"`
}

type sftRunHeader struct {
	ModelRef      string          `json:"model_ref"`
	WorkspaceRoot string          `json:"workspace_root"`
	System        string          `json:"system"`
	PrefixHash    string          `json:"prefix_hash"`
	Tools         json.RawMessage `json:"tools"`
}

// workspacePlaceholder replaces the run's workspace path in exported text. The
// real path is a per-run temporary directory; training on it would teach a
// location that never exists again.
const workspacePlaceholder = "/workspace"

// sftBuilder accumulates one run's messages. Tool calls are held until their
// results arrive so an assistant turn and its results stay adjacent.
type sftBuilder struct {
	msgs      []sftMessage
	order     []string
	calls     map[string]sftToolFunction
	results   map[string]string
	text      string
	reasoning string
}

func newSFTBuilder() *sftBuilder {
	return &sftBuilder{calls: map[string]sftToolFunction{}, results: map[string]string{}}
}

func (b *sftBuilder) flush() {
	if b.text == "" && b.reasoning == "" && len(b.order) == 0 {
		return
	}
	msg := sftMessage{Role: "assistant", Content: b.text, Reasoning: b.reasoning}
	for _, id := range b.order {
		msg.ToolCalls = append(msg.ToolCalls, sftToolCall{ID: id, Type: "function", Function: b.calls[id]})
	}
	b.msgs = append(b.msgs, msg)
	for _, id := range b.order {
		b.msgs = append(b.msgs, sftMessage{Role: "tool", ToolCallID: id, Content: b.results[id]})
	}
	b.order = nil
	clear(b.calls)
	clear(b.results)
	b.text, b.reasoning = "", ""
}

// convertSFT walks one trajectory into an assistant/tool message sequence.
// Sub-agent calls are skipped: they were sampled against their own prefix, so
// they do not belong to the header this sample carries.
func convertSFT(records []sftRecord) []sftMessage {
	b := newSFTBuilder()
	for _, rec := range records {
		e := rec.Event
		if e == nil {
			continue
		}
		if e.Tool != nil && e.Tool.ParentID != "" {
			continue
		}
		switch e.Kind {
		case "message":
			if len(b.order) > 0 {
				b.flush()
			}
			b.text, b.reasoning = e.Text, e.Reasoning
		case "tool_dispatch":
			if e.Tool == nil || e.Tool.Partial || e.Tool.ID == "" {
				continue
			}
			args := e.Tool.Args
			if args == "" {
				args = "{}"
			}
			if _, seen := b.calls[e.Tool.ID]; !seen {
				b.order = append(b.order, e.Tool.ID)
			}
			b.calls[e.Tool.ID] = sftToolFunction{Name: e.Tool.Name, Arguments: args}
		case "tool_result":
			if e.Tool == nil || e.Tool.ID == "" {
				continue
			}
			out := e.Tool.Output
			if out == "" {
				out = e.Tool.Err
			}
			b.results[e.Tool.ID] = out
			if len(b.results) == len(b.order) && len(b.order) > 0 {
				b.flush()
			}
		}
	}
	b.flush()
	return b.msgs
}

func readSFTRecords(path string) ([]sftRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []sftRecord
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for sc.Scan() {
		var rec sftRecord
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			continue
		}
		out = append(out, rec)
	}
	return out, sc.Err()
}

// verifyPrefix reports the header and how many of the run's rounds it provably
// covers. A header that does not cover every round describes a different
// request than the one that produced these messages.
func verifyPrefix(records []sftRecord) (*sftRunHeader, int, int) {
	var hdr *sftRunHeader
	covered, total := 0, 0
	for _, rec := range records {
		if rec.RunHeader != nil && hdr == nil {
			hdr = rec.RunHeader
			continue
		}
		e := rec.Event
		if e == nil || e.Kind != "usage" || e.Usage == nil || e.Usage.CacheDiagnostics == nil {
			continue
		}
		if e.Usage.CacheDiagnostics.PrefixHash == "" {
			continue
		}
		total++
		if hdr != nil && e.Usage.CacheDiagnostics.PrefixHash == hdr.PrefixHash {
			covered++
		}
	}
	return hdr, covered, total
}

// buildSFTSample turns one recorded run into a training sample, or explains in
// one phrase why it cannot be one.
func buildSFTSample(id, prompt, path string) (*sftSample, string) {
	records, err := readSFTRecords(path)
	if err != nil {
		return nil, "unreadable: " + err.Error()
	}
	hdr, covered, total := verifyPrefix(records)
	if hdr == nil {
		return nil, "no run header"
	}
	if total == 0 {
		return nil, "no rounds"
	}
	if covered != total {
		return nil, fmt.Sprintf("prefix covers %d/%d rounds", covered, total)
	}
	msgs := convertSFT(records)
	if len(msgs) == 0 {
		return nil, "no messages"
	}
	sample := &sftSample{
		TaskID: id, ModelRef: hdr.ModelRef, Tools: hdr.Tools,
		Messages: append([]sftMessage{
			{Role: "system", Content: hdr.System},
			{Role: "user", Content: prompt},
		}, msgs...),
	}
	if root := strings.TrimSpace(hdr.WorkspaceRoot); root != "" {
		portableWorkspace(sample, root)
	}
	return sample, ""
}

func portableWorkspace(s *sftSample, root string) {
	for i := range s.Messages {
		m := &s.Messages[i]
		m.Content = strings.ReplaceAll(m.Content, root, workspacePlaceholder)
		m.Reasoning = strings.ReplaceAll(m.Reasoning, root, workspacePlaceholder)
		for j := range m.ToolCalls {
			fn := &m.ToolCalls[j].Function
			fn.Arguments = strings.ReplaceAll(fn.Arguments, root, workspacePlaceholder)
		}
	}
}

// runSFTMode exports the graded, header-verified runs under dir as JSONL. The
// grader's verdict decides what is kept — a run's own completion claim is the
// thing a trained model would learn to imitate, not evidence it succeeded.
func runSFTMode(dir, suite, reportPath, out string) error {
	if out == "" {
		return fmt.Errorf("-out is required: it names the JSONL file to write")
	}
	paths, err := filepath.Glob(filepath.Join(dir, "*.trajectory.jsonl"))
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return fmt.Errorf("no *.trajectory.jsonl files under %s", dir)
	}
	slices.Sort(paths)
	prompts, err := sftPrompts(suite)
	if err != nil {
		return err
	}
	passed, err := sftVerdicts(reportPath)
	if err != nil {
		return err
	}

	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	buf := bufio.NewWriter(f)
	enc := json.NewEncoder(buf)
	kept := 0
	for _, p := range paths {
		id := strings.TrimSuffix(filepath.Base(p), ".trajectory.jsonl")
		switch {
		case !passed[id]:
			fmt.Fprintf(os.Stderr, "skip %s: the grader did not pass it\n", id)
			continue
		case prompts[id] == "":
			fmt.Fprintf(os.Stderr, "skip %s: no task prompt under %s\n", id, suite)
			continue
		}
		sample, why := buildSFTSample(id, prompts[id], p)
		if sample == nil {
			fmt.Fprintf(os.Stderr, "skip %s: %s\n", id, why)
			continue
		}
		if err := enc.Encode(sample); err != nil {
			return err
		}
		kept++
	}
	if err := buf.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "wrote %d of %d runs to %s\n", kept, len(paths), out)
	return nil
}

func sftPrompts(suite string) (map[string]string, error) {
	tasks, err := loadTasks(suite)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(tasks))
	for _, t := range tasks {
		out[t.ID] = t.Prompt
	}
	return out, nil
}

func sftVerdicts(path string) (map[string]bool, error) {
	if path == "" {
		return nil, fmt.Errorf("-report is required: it names the run report whose grader verdicts decide what is kept")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var results []result
	if err := json.Unmarshal(raw, &results); err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(results))
	for _, r := range results {
		out[r.ID] = r.Passed
	}
	return out, nil
}
