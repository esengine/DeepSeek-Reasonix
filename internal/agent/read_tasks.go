package agent

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"sync"

	"reasonix/internal/tool"
)

// readState groups the run-scoped read registry with its generation.
type readState struct {
	tasks  *readTasks
	runGen uint64
	// gates enables the per-operation evidence check for this run.
	gates bool
	// deliveries retains metadata only; visible is rebuilt from each frozen request.
	deliveries map[string]readDelivery
	visible    map[string]readDelivery
}

// readTasks keeps the logical identity of in-flight read tasks so a
// continuation page joins the read it continues. A forged, expired,
// cross-session, cross-file, or out-of-position cursor is rejected at the
// execution entry; decoding a token is never the same as accepting it.
type readTasks struct {
	mu         sync.Mutex
	sessionID  string
	generation uint64
	binding    string
	byID       map[string]readTask
}

type readTask struct {
	path         string
	argumentPath string
	snapshot     string
	requestEnd   int
	cursor       tool.ReadCursor
	issued       bool
}

func newReadTasks(sessionID string, generation uint64) *readTasks {
	return &readTasks{sessionID: sessionID, generation: generation, binding: rand.Text(), byID: map[string]readTask{}}
}

// accept reports whether the cursor may continue a live read task.
func (r *readTasks) accept(cursor tool.ReadCursor, path string) bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	task, known := r.byID[cursor.ReadID]
	switch {
	case !known:
		return false
	case cursor.Binding != r.binding:
		return false
	case cursor.SessionID != r.sessionID:
		return false
	case cursor.RunGen != r.generation:
		return false
	case cursor.Path != path || cursor.Path != task.path:
		return false
	case cursor.Snapshot == "" || cursor.Snapshot != task.snapshot:
		return false
	case !task.issued || cursor != task.cursor:
		return false
	}
	return true
}

// remember records the task's latest snapshot and requested window.
func (r *readTasks) remember(readID string, env tool.ReadResultEnvelope, paths ...string) {
	if r == nil || readID == "" {
		return
	}
	requestEnd := 0
	if env.RequestedRange != nil {
		requestEnd = env.RequestedRange.End
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	cursor, issued := tool.DecodeReadCursor(env.NextCursor)
	argumentPath := r.byID[readID].argumentPath
	if len(paths) > 0 && paths[0] != "" {
		argumentPath = paths[0]
	}
	r.byID[readID] = readTask{path: env.Source.CanonicalPath, argumentPath: argumentPath, snapshot: env.Source.Snapshot, requestEnd: requestEnd, cursor: cursor, issued: issued}
}

// resolveReadCursor rewrites a continuation call into the explicit window its
// cursor names and marks the plan with the logical read it continues. A cursor
// the host cannot vouch for is an error, never a silent new read.
func (a *Agent) resolveReadCursor(plan *toolCallPlan) (toolOutcome, bool) {
	token := readCursorArg(plan.execArgs)
	if token == "" {
		return toolOutcome{}, false
	}
	cursor, ok := tool.DecodeReadCursor(token)
	if !ok {
		return readCursorRejected("the read continuation cursor is malformed; re-read the file with read_file", plan)
	}
	path := readPathArg(plan.execArgs)
	if resolver, ok := plan.execTool.(tool.ReadPathResolver); ok {
		resolved, err := resolver.ResolveReadPath(plan.execArgs)
		if err != nil {
			return readCursorRejected(err.Error(), plan)
		}
		path = resolved
	}
	if !filepath.IsAbs(path) && a.writeWorkspaceRoot != "" {
		path = filepath.Join(a.writeWorkspaceRoot, path)
	}
	path = filepath.Clean(path)
	if !a.reads.tasks.accept(cursor, path) {
		return readCursorRejected("the read continuation cursor is not valid for this file or session; re-read the file with read_file", plan)
	}
	rewritten, err := withResolvedReadWindow(plan.execArgs, cursor)
	if err != nil {
		return readCursorRejected(err.Error(), plan)
	}
	plan.execArgs = rewritten
	plan.permArgs = rewritten
	plan.evidenceArgs = rewritten
	plan.readTaskID = cursor.ReadID
	plan.readSnapshot = cursor.Snapshot
	return toolOutcome{}, false
}

func readCursorRejected(msg string, plan *toolCallPlan) (toolOutcome, bool) {
	d := &tool.OperationDiagnostic{Code: tool.ReadCursorInvalid, OperationID: plan.call.ID, Path: readPathArg(plan.execArgs), Recovery: "inspect a fresh explicit range; do not reuse the rejected cursor"}
	return toolOutcome{output: "error: " + msg, errMsg: msg, blocked: true, diagnostic: d}, true
}

func readCursorArg(args json.RawMessage) string {
	var fields struct {
		Cursor string `json:"cursor"`
	}
	if err := json.Unmarshal(args, &fields); err != nil {
		return ""
	}
	return fields.Cursor
}

func readPathArg(args json.RawMessage) string {
	var fields struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &fields); err != nil {
		return ""
	}
	return fields.Path
}

// withResolvedReadWindow replaces the cursor with the explicit offset/limit it
// names, so the reader never needs to understand the token.
func withResolvedReadWindow(args json.RawMessage, cursor tool.ReadCursor) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(args, &fields); err != nil {
		return nil, fmt.Errorf("invalid args: %w", err)
	}
	if _, present := fields["offset"]; present {
		return nil, fmt.Errorf("cursor cannot be combined with offset; pass the issued cursor unchanged")
	}
	if _, present := fields["limit"]; present {
		return nil, fmt.Errorf("cursor cannot be combined with limit; pass the issued cursor unchanged")
	}
	delete(fields, "cursor")
	delete(fields, "intent")
	delete(fields, "offset")
	delete(fields, "limit")
	fields["offset"] = json.RawMessage(strconv.Itoa(cursor.NextStart))
	if cursor.RequestEnd > cursor.NextStart {
		fields["limit"] = json.RawMessage(strconv.Itoa(cursor.RequestEnd - cursor.NextStart))
	}
	out, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("invalid args: %w", err)
	}
	return out, nil
}
