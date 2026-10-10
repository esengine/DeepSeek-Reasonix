package tui

import (
	"context"
	"net/http"
	"net/url"
)

// InboxItem is one queued entry as the kernel's inbox lists it: metadata only,
// never the body.
type InboxItem struct {
	ID      string `json:"id"`
	Intent  string `json:"intent"`
	State   string `json:"state"`
	Preview string `json:"preview"`
}

// InboxSnapshot is the durable queue as GET /inbox answers it.
type InboxSnapshot struct {
	Revision   int64       `json:"revision"`
	Paused     bool        `json:"paused"`
	Recovered  bool        `json:"recovered"`
	RecoveredN int         `json:"recoveredCount"`
	Items      []InboxItem `json:"items"`
}

// InboxReceipt says how the kernel settled an enqueue.
type InboxReceipt struct {
	ItemID      string `json:"itemId"`
	Disposition string `json:"disposition"`
}

// Dispositions an enqueue receipt carries that the commands tell apart.
const (
	dispositionSteerAccepted  = "steer_accepted"
	dispositionQueuedFollowup = "queued_followup"
)

func (c *Client) Inbox(ctx context.Context) (InboxSnapshot, error) {
	var out InboxSnapshot
	err := c.do(ctx, http.MethodGet, "/inbox", nil, &out)
	return out, err
}

// Enqueue persists input in the kernel's inbox. A steer is applied at the
// running turn's next tool boundary, or falls back to a follow-up.
func (c *Client) Enqueue(ctx context.Context, input string, steer bool) (InboxReceipt, error) {
	intent := "followup"
	if steer {
		intent = "steer"
	}
	var out InboxReceipt
	err := c.do(ctx, http.MethodPost, "/inbox/items", map[string]string{"input": input, "intent": intent}, &out)
	return out, err
}

// InboxBody reads the full text of one queued item.
func (c *Client) InboxBody(ctx context.Context, id string) (string, error) {
	var out struct {
		Envelope struct {
			SubmitText string `json:"submitText"`
		} `json:"envelope"`
	}
	err := c.do(ctx, http.MethodGet, "/inbox/items/"+url.PathEscape(id), nil, &out)
	return out.Envelope.SubmitText, err
}

func (c *Client) InboxUpdate(ctx context.Context, id, input string) error {
	return c.do(ctx, http.MethodPatch, "/inbox/items/"+url.PathEscape(id), map[string]string{"input": input}, nil)
}

func (c *Client) InboxDelete(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/inbox/items/"+url.PathEscape(id), nil, nil)
}

// InboxMove puts an item at a zero-based position.
func (c *Client) InboxMove(ctx context.Context, id string, to int) error {
	return c.do(ctx, http.MethodPost, "/inbox/move", map[string]any{"id": id, "toIndex": to}, nil)
}

func (c *Client) InboxPause(ctx context.Context, paused bool) error {
	path := "/inbox/resume"
	if paused {
		path = "/inbox/pause"
	}
	return c.do(ctx, http.MethodPost, path, nil, nil)
}

func (c *Client) InboxRetry(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/inbox/items/"+url.PathEscape(id)+"/retry", nil, nil)
}

func (c *Client) InboxRefresh(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/inbox/items/"+url.PathEscape(id)+"/refresh", nil, nil)
}

// ExportConversation has the kernel save the conversation as markdown in its
// workspace; messages is 0 when there was nothing to save.
func (c *Client) ExportConversation(ctx context.Context) (path string, messages int, err error) {
	var out struct {
		Path     string `json:"path"`
		Messages int    `json:"messages"`
	}
	err = c.do(ctx, http.MethodPost, "/sessions/export", nil, &out)
	return out.Path, out.Messages, err
}
