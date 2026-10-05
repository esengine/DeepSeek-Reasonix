package builtin

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"testing"

	"reasonix/internal/platform/browser"
)

type toolClosePage struct {
	in     chan []byte
	closed chan struct{}
	once   sync.Once
	mode   string
	closes int
}

func (p *toolClosePage) ReadMessage() ([]byte, error) {
	select {
	case msg := <-p.in:
		return msg, nil
	case <-p.closed:
		return nil, io.EOF
	}
}
func (p *toolClosePage) Close() error { p.once.Do(func() { close(p.closed) }); return nil }
func (p *toolClosePage) WriteMessage(raw []byte) error {
	var msg struct {
		ID     int
		Method string
	}
	if err := json.Unmarshal(raw, &msg); err != nil {
		return err
	}
	reply := map[string]any{"id": msg.ID}
	result := map[string]any{}
	switch msg.Method {
	case "Target.createTarget":
		result["targetId"] = "owned-page"
	case "Target.attachToTarget":
		result["sessionId"] = "owned-session"
	case "Page.getFrameTree":
		result["frameTree"] = map[string]any{"frame": map[string]string{"id": "frame-1", "url": "about:blank"}}
	case "Page.navigate":
		reply["error"] = map[string]any{"code": -32000, "message": "navigation refused"}
	case "Target.closeTarget":
		p.closes++
		if p.mode == "error" || p.mode == "gone" {
			reply["error"] = map[string]any{"code": -32000, "message": "close refused"}
		} else {
			result["success"] = p.mode != "refused"
		}
	case "Target.getTargets":
		targets := []map[string]string{}
		if p.mode != "gone" {
			targets = append(targets, map[string]string{"targetId": "owned-page"})
		}
		result["targetInfos"] = targets
	}
	reply["result"] = result
	encoded, _ := json.Marshal(reply)
	p.in <- encoded
	return nil
}

func TestBrowserOpenCloseRetainsFailuresAndCleansGoneTargets(t *testing.T) {
	for _, mode := range []string{"error", "refused", "gone"} {
		t.Run(mode, func(t *testing.T) {
			page := &toolClosePage{in: make(chan []byte, 64), closed: make(chan struct{}), mode: mode}
			pool := &browser.Pool{}
			pool.SetEndpoint(func(context.Context, string) (browser.Endpoint, error) { return page, nil })
			session := browser.NewSession(browser.Config{Pool: pool, Launch: browser.LaunchSpec{ProfileDir: "/profiles/tool-close"}})
			t.Cleanup(session.Close)
			_, _ = session.Visit(t.Context(), "https://example.com", "", true)
			tabs := session.Tabs()
			if len(tabs) != 1 {
				t.Fatalf("tabs = %+v", tabs)
			}
			args, _ := json.Marshal(map[string]any{"tab": tabs[0].ID, "close": true})
			op := browserOpen{session: session}
			out, err := op.Execute(t.Context(), args)
			if page.closes != 1 {
				t.Fatalf("native close calls = %d", page.closes)
			}
			if mode == "gone" {
				if err != nil || len(session.Tabs()) != 0 {
					t.Fatalf("gone close = %q, %v, %+v", out, err, session.Tabs())
				}
				return
			}
			if browser.CodeOf(err) != browser.CodeEngineFailed || out != "" || len(session.Tabs()) != 1 {
				t.Fatalf("refused close = %q, %v, %+v", out, err, session.Tabs())
			}
			page.mode = "accepted"
			out, err = op.Execute(t.Context(), args)
			if err != nil || len(session.Tabs()) != 0 || page.closes != 2 {
				t.Fatalf("retry = %q, %v, %+v", out, err, session.Tabs())
			}
		})
	}
}
