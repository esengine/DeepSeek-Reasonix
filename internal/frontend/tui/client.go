package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

// APIError is a refusal the kernel named. Code is the stable identity a
// caller branches on; Message is the kernel's English, for display only.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("%s (%d %s)", e.Message, e.Status, e.Code)
	}
	return fmt.Sprintf("%s (%d)", e.Message, e.Status)
}

// Code reports the kernel's refusal code carried by err, or "".
func Code(err error) string {
	if api, ok := errors.AsType[*APIError](err); ok {
		return api.Code
	}
	return ""
}

// Refusal codes the TUI acts on rather than only shows.
const (
	CodeSessionBusy = "busy.session_running"
	CodePlanStale   = "plan.decision_stale"
	CodeKeyMissing  = "provider.key_missing"
)

// Client drives one runtime of a serve hub. Base is its route prefix, e.g.
// http://reasonix.local/rt/r1; every write sends JSON, which serve requires of
// state-changing requests.
type Client struct {
	HTTP *http.Client
	Base string
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(raw)
	} else if method != http.MethodGet {
		rd = bytes.NewReader([]byte("{}"))
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+path, rd)
	if err != nil {
		return err
	}
	if rd != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return decodeRefusal(resp)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func decodeRefusal(resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var r struct {
		Code    string `json:"code"`
		Message string `json:"error"`
	}
	if json.Unmarshal(raw, &r) != nil || r.Message == "" {
		r.Message = string(bytes.TrimSpace(raw))
	}
	if r.Message == "" {
		r.Message = http.StatusText(resp.StatusCode)
	}
	return &APIError{Status: resp.StatusCode, Code: r.Code, Message: r.Message}
}

// Submit starts a turn, or runs a `!` shell command where the transport allows
// it. A running turn refuses it with CodeSessionBusy; Queue is the way in then.
// A slash command nothing answers is sent to the model as a regular message
// with a visible notice, as in 1.x (#5756).
func (c *Client) Submit(ctx context.Context, input string) error {
	return c.do(ctx, http.MethodPost, "/submit", map[string]string{"input": input}, nil)
}

// RunShell runs a `!` command on this machine. It stays local, as in 1.x: the
// output is shown and the model is not asked to answer it.
func (c *Client) RunShell(ctx context.Context, command string) error {
	return c.do(ctx, http.MethodPost, "/submit", map[string]any{"input": "!" + command, "localShell": true}, nil)
}

// Queue hands input to the running turn: steer lands at its next tool
// boundary, a follow-up starts once it finishes.
func (c *Client) Queue(ctx context.Context, input string, steer bool) (string, error) {
	intent := "followup"
	if steer {
		intent = "steer"
	}
	var receipt struct {
		ItemID string `json:"itemId"`
	}
	err := c.do(ctx, http.MethodPost, "/inbox/items", map[string]string{"input": input, "intent": intent}, &receipt)
	return receipt.ItemID, err
}

func (c *Client) Cancel(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/cancel", nil, nil)
}

// Approve answers an approval request: once, for the session, or persisted.
func (c *Client) Approve(ctx context.Context, id string, allow, session, persist bool) error {
	return c.do(ctx, http.MethodPost, "/approve", map[string]any{"id": id, "allow": allow, "session": session, "persist": persist}, nil)
}

func (c *Client) PlanDecision(ctx context.Context, id, action string) error {
	return c.do(ctx, http.MethodPost, "/plan-decision", map[string]string{"id": id, "action": action}, nil)
}

// AskAnswer is one question's selection. The kernel's type has no json tags,
// so its keys are the Go field names.
type AskAnswer struct {
	QuestionID string
	Selected   []string
}

func (c *Client) Answer(ctx context.Context, id string, answers []AskAnswer) error {
	return c.do(ctx, http.MethodPost, "/answer", map[string]any{"id": id, "answers": answers}, nil)
}

func (c *Client) SetApprovalMode(ctx context.Context, mode string) error {
	return c.do(ctx, http.MethodPost, "/tool-approval-mode", map[string]string{"mode": mode}, nil)
}

// HistoryMessage is one record of the session as /history returns it.
type HistoryMessage struct {
	Role         string            `json:"role"`
	Content      string            `json:"content"`
	Reasoning    string            `json:"reasoning,omitempty"`
	MsgIndex     int               `json:"msgIndex"`
	HostAuthored bool              `json:"hostAuthored,omitempty"`
	Steer        bool              `json:"steer,omitempty"`
	ThoughtMs    int64             `json:"thoughtMs,omitempty"`
	ToolCalls    []HistoryToolCall `json:"toolCalls,omitempty"`
	ToolCallID   string            `json:"toolCallId,omitempty"`
	ToolFailed   bool              `json:"toolFailed,omitempty"`
	ToolName     string            `json:"toolName,omitempty"`
	// OutputDiff marks a rebuilt shell result whose whole output is a unified
	// diff, re-derived by the server so a reopened session renders it as one.
	OutputDiff bool `json:"outputDiff,omitempty"`
}

type HistoryToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

func (c *Client) History(ctx context.Context) ([]HistoryMessage, error) {
	var out []HistoryMessage
	err := c.do(ctx, http.MethodGet, "/history", nil, &out)
	return out, err
}

// Status is the subset of /status the TUI draws.
type Status struct {
	Label            string `json:"label"`
	Running          bool   `json:"running"`
	ToolApprovalMode string `json:"toolApprovalMode"`
	ModelRef         string `json:"modelRef"`
	Effort           string `json:"effort"`
	Used             int    `json:"used"`
	Window           int    `json:"window"`
	CacheHit         int    `json:"cacheHit"`
	CacheMiss        int    `json:"cacheMiss"`
	Cwd              string `json:"cwd"`
	SessionPath      string `json:"sessionPath"`
	WorkspaceRoot    string `json:"workspaceRoot"`
	Plan             bool   `json:"plan"`
	// Jobs is how many background jobs the session runs; only the count shows.
	Jobs      []json.RawMessage `json:"jobs"`
	LastUsage *struct {
		CacheHitTokens  int
		CacheMissTokens int
	} `json:"lastUsage"`
	SessionCostQuote *CostQuote `json:"sessionCostQuote"`
}

// CostQuote is the session's spend as the kernel priced it.
type CostQuote struct {
	Original     Money  `json:"original"`
	Selected     *Money `json:"selected"`
	CostComplete bool   `json:"costComplete"`
	RateBand     string `json:"rateBand"`
}

type Money struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

// Balance is the provider wallet as the kernel reads it; ok is false when no
// wallet is configured for the active provider.
func (c *Client) Balance(ctx context.Context) (display string, ok bool, err error) {
	var out struct {
		Display string `json:"display"`
	}
	if err := c.do(ctx, http.MethodGet, "/balance", nil, &out); err != nil {
		return "", false, err
	}
	return out.Display, out.Display != "", nil
}

// GitInfo is the workspace's repository identity as the kernel reads it; Repo
// is false when the workspace is not version-controlled.
type GitInfo struct {
	Repo      bool   `json:"repo"`
	Name      string `json:"name"`
	Branch    string `json:"branch"`
	Detached  bool   `json:"detached"`
	Added     int    `json:"added"`
	Removed   int    `json:"removed"`
	Untracked int    `json:"untracked"`
}

func (c *Client) WorkspaceGit(ctx context.Context) (GitInfo, error) {
	var out GitInfo
	err := c.do(ctx, http.MethodGet, "/workspace/git", nil, &out)
	return out, err
}

// Compaction is where the session folds its history.
type Compaction struct {
	Ratio   float64 `json:"ratio"`
	Trigger int     `json:"trigger"`
	Window  int     `json:"context_window"`
}

func (c *Client) Compaction(ctx context.Context) (Compaction, error) {
	var out Compaction
	err := c.do(ctx, http.MethodGet, "/compaction", nil, &out)
	return out, err
}

func (c *Client) SetPlan(ctx context.Context, on bool) error {
	return c.do(ctx, http.MethodPost, "/plan", map[string]bool{"on": on}, nil)
}

func (c *Client) Status(ctx context.Context) (Status, error) {
	var out Status
	err := c.do(ctx, http.MethodGet, "/status", nil, &out)
	return out, err
}

// Completion is what /complete offers for the token under the cursor. From
// and To are UTF-16 offsets into the line, as the kernel counts them.
type Completion struct {
	Kind  string           `json:"kind"`
	From  int              `json:"from"`
	To    int              `json:"to"`
	Query string           `json:"query,omitempty"`
	Items []CompletionItem `json:"items"`
	// Typed is the command the line already spells in full, kept out of Items.
	Typed *CompletionItem `json:"typed,omitempty"`
}

type CompletionItem struct {
	Label   string `json:"label"`
	Insert  string `json:"insert"`
	Hint    string `json:"hint,omitempty"`
	Descend bool   `json:"descend,omitempty"`
	Kind    string `json:"kind,omitempty"`
}

// Complete asks for completions at cursor, a UTF-16 offset into line.
func (c *Client) Complete(ctx context.Context, line string, cursor int) (Completion, error) {
	var out Completion
	q := url.Values{"line": {line}, "cursor": {strconv.Itoa(cursor)}}
	err := c.do(ctx, http.MethodGet, "/complete?"+q.Encode(), nil, &out)
	return out, err
}

// TodoItem is one step of the kernel's canonical task list.
type TodoItem struct {
	Content    string `json:"content"`
	Status     string `json:"status"`
	ActiveForm string `json:"activeForm,omitempty"`
	Level      int    `json:"level,omitempty"`
}

func (c *Client) Todos(ctx context.Context) ([]TodoItem, error) {
	var out []TodoItem
	err := c.do(ctx, http.MethodGet, "/todos", nil, &out)
	return out, err
}
