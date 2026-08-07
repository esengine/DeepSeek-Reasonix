package flywheel

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ---------------------------------------------------------------------------
// Replay（事件溯源：精确重放）
//
// 从 flywheel 事件流（<state>/flywheel/events/YYYY-MM-DD.jsonl，genai.event.v1）
// 重建会话轨迹：工具调用序列 + 消息 + usage + 错误码。用于：
//   - 可审计：任何时间点"发生了什么"可精确重放；
//   - 崩溃恢复：重放事件流恢复会话上下文（配合 session 持久化与
//     DeliveryCheckpoint，见 docs/MODELLING_ENGINEERING.md §4）。
//
// 纯只读、无副作用；失败容错（坏行跳过并计数）。
// ---------------------------------------------------------------------------

// Event is one replayed flywheel event line (subset of the stored schema).
type Event struct {
	TS      string    `json:"ts"`
	Session string    `json:"session,omitempty"`
	Span    string    `json:"span"`
	Kind    string    `json:"kind"`
	Tool    *ToolLine `json:"gen_ai.tool,omitempty"`
	Model   *ModelUse `json:"gen_ai.model,omitempty"`
	Payload string    `json:"payload,omitempty"`
	Err     string    `json:"error_code,omitempty"`
}

// ReplayResult is the reconstructed view of one event stream.
type ReplayResult struct {
	Events     []Event `json:"events"`
	Skipped    int     `json:"skipped"` // malformed lines ignored
	ToolCalls  int     `json:"tool_calls"`
	ToolErrors int     `json:"tool_errors"`
	Messages   int     `json:"messages"`
	Usages     int     `json:"usages"`
	Compacts   int     `json:"compacts"`
}

// Replay reads every daily event file under dir (lexicographic = chronological)
// and reconstructs the event timeline, optionally filtered to one session.
// session "" = all sessions.
func Replay(dir, session string) (*ReplayResult, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return &ReplayResult{}, nil
		}
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		files = append(files, filepath.Join(dir, e.Name()))
	}
	sort.Strings(files) // daily files are name-sortable chronologically

	res := &ReplayResult{}
	for _, f := range files {
		if err := replayFile(f, session, res); err != nil {
			return nil, err
		}
	}
	return res, nil
}

func replayFile(path, session string, res *ReplayResult) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var ev Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			res.Skipped++
			continue
		}
		if session != "" && ev.Session != "" && ev.Session != session {
			continue
		}
		res.Events = append(res.Events, ev)
		switch ev.Kind {
		case "tool_use":
			res.ToolCalls++
			if ev.Err != "" {
				res.ToolErrors++
			}
		case "message":
			res.Messages++
		case "usage":
			res.Usages++
		case "compaction":
			res.Compacts++
		}
	}
	return sc.Err()
}

// ToolCalls returns the replayed tool-call sequence (in order) for audit view.
func (r *ReplayResult) ToolCallList() []ToolLine {
	if r == nil {
		return nil
	}
	var out []ToolLine
	for _, e := range r.Events {
		if e.Kind == "tool_use" && e.Tool != nil {
			out = append(out, *e.Tool)
		}
	}
	return out
}
