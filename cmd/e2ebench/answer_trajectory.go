package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var answerMemoryPath = regexp.MustCompile(`(?:^|[/\\])tasks[/\\][^/\\]+[/\\]memory[/\\]`)

var (
	errAnswerRead            = errors.New("graded trajectory read task memory")
	errAnswerAuditIncomplete = errors.New("answer trajectory audit incomplete")
)

type answerToolRecord struct {
	Event *struct {
		Kind string `json:"kind"`
		Tool *struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			Args      string `json:"args"`
			Output    string `json:"output"`
			Err       string `json:"err"`
			Execution *struct {
				State    string `json:"state"`
				ExitCode *int   `json:"exitCode"`
			} `json:"execution"`
		} `json:"tool"`
	} `json:"event"`
}

type answerToolCall struct{ name, args string }

// answerBodyLines are the authored facts, excluding repeated frontmatter.
// A path listing alone is not evidence that the child read a fact body.
func answerBodyLines(root string) ([]string, error) {
	var lines []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".md" || !strings.Contains(path, string(filepath.Separator)+"memory"+string(filepath.Separator)) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		body := string(data)
		if strings.HasPrefix(body, "---\n") {
			if end := strings.Index(body[4:], "\n---\n"); end >= 0 {
				body = body[4+end+5:]
			}
		}
		for line := range strings.SplitSeq(body, "\n") {
			line = strings.TrimSpace(line)
			if len(line) >= 20 {
				lines = append(lines, line)
			}
		}
		return nil
	})
	return lines, err
}

// scanAnswerRead checks completed tool results, not dispatched attempts: the
// OS sandbox may correctly deny a cat of a forbidden path. A successful
// native file read is direct evidence; a shell read needs both the forbidden
// path and an authored fact body in its output.
func scanAnswerRead(path string, bodyLines []string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	calls := map[string]answerToolCall{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for sc.Scan() {
		var rec answerToolRecord
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			return false, fmt.Errorf("decode trajectory: %w", err)
		}
		if rec.Event == nil || rec.Event.Tool == nil {
			continue
		}
		tl := rec.Event.Tool
		if rec.Event.Kind == "tool_dispatch" {
			if tl.ID != "" && tl.Args != "" {
				calls[tl.ID] = answerToolCall{tl.Name, tl.Args}
			}
			continue
		}
		if rec.Event.Kind != "tool_result" {
			continue
		}
		call := answerToolCall{tl.Name, tl.Args}
		if prior, ok := calls[tl.ID]; ok {
			if call.name == "" {
				call.name = prior.name
			}
			if call.args == "" {
				call.args = prior.args
			}
		}
		delete(calls, tl.ID)
		if tl.Err != "" || tl.Execution != nil && (tl.Execution.State == "not_run" || tl.Execution.ExitCode != nil && *tl.Execution.ExitCode != 0) {
			continue
		}
		if !answerMemoryPath.MatchString(call.args) {
			continue
		}
		if call.name == "read_file" && tl.Output != "" {
			return true, nil
		}
		if call.name == "bash" || call.name == "grep" {
			for _, line := range bodyLines {
				if strings.Contains(tl.Output, line) {
					return true, nil
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		return false, err
	}
	return false, nil
}

func auditAnswerTrajectories(dir string, t task, segments int) (bool, error) {
	if segments < 1 {
		return false, fmt.Errorf("run recorded no segments")
	}
	lines, err := answerBodyLines(t.answerRoot)
	if err != nil {
		return false, err
	}
	for index := 1; index <= segments; index++ {
		path := segmentTrajectoryPath(dir, t.ID, segment{index: index}, segments)
		read, err := scanAnswerRead(path, lines)
		if err != nil || read {
			return read, err
		}
	}
	return false, nil
}

func answerTrajectoryDir(dir string, required bool) (string, func(), error) {
	cleanup := func() {}
	if required && dir == "" {
		var err error
		dir, err = os.MkdirTemp("", "e2ebench-answer-audit-")
		if err != nil {
			return "", cleanup, err
		}
		cleanup = func() { _ = os.RemoveAll(dir) }
	}
	if dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			cleanup()
			return "", func() {}, err
		}
	}
	return dir, cleanup, nil
}

func auditAnswerRun(r *result, dir string, t task) bool {
	if t.answerRoot == "" {
		return true
	}
	read, err := auditAnswerTrajectories(dir, t, r.Segments)
	if err != nil {
		markAnswerAuditIncomplete(r, err)
		r.Note = "answer audit: " + err.Error()
		return false
	}
	if read {
		r.Outcome = "answer_leak"
		r.answerAuditErr = errAnswerRead
		r.Note = "answer audit: graded trajectory read a task memory file"
		return false
	}
	return true
}

func markAnswerAuditIncomplete(r *result, err error) {
	r.Outcome = "answer_audit_incomplete"
	r.answerAuditErr = fmt.Errorf("%w: %w", errAnswerAuditIncomplete, err)
}

func answerAuditFailed(r result) bool {
	return r.answerAuditErr != nil
}

func answerLeakDetected(r result) bool {
	return errors.Is(r.answerAuditErr, errAnswerRead)
}

func recordTaskTrajectory(r *result, path string, t task) {
	if summary, err := summarizeTrajectory(path); err == nil {
		r.Trajectory = summary
	}
	applyMemoryStats(r, path, t)
}
