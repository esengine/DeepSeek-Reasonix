// Command frameextension draws a task board in a sandboxed frame. The board's
// HTML ships in the package (frames/board.html); this sidecar owns the cards,
// publishes them as props, and moves one when the frame sends "advance".
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"sync/atomic"

	extension "github.com/esengine/DeepSeek-Reasonix/sdk/go"
)

var columns = []string{"todo", "doing", "done"}

type card struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type board struct {
	mu      sync.Mutex
	cards   map[string][]card
	session atomic.Pointer[extension.SessionContext]
	ui      extension.HostUI
}

func newBoard() *board {
	return &board{cards: map[string][]card{
		"todo":  {{ID: "c", Title: "Write the slot docs"}},
		"doing": {{ID: "a", Title: "Refactor slots.ts"}, {ID: "b", Title: "Add slot tests"}},
		"done":  {},
	}}
}

func (b *board) Initialize(_ context.Context, params extension.InitializeParams) (*extension.InitializeResult, error) {
	session := params.Session
	b.session.Store(&session)
	return &extension.InitializeResult{
		Subscriptions: []string{"session.start"},
		UIActions:     []extension.UIActionDecl{{ActionID: "advance", Label: "Advance a card"}, {ActionID: "refresh", Label: "Refresh the board"}},
		Provides:      append([]extension.CapabilityWire(nil), params.Manifest.Provides...),
	}, nil
}

// props is what the frame document is handed: the columns in order.
func (b *board) props() (json.RawMessage, string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := map[string][]card{}
	for _, c := range columns {
		out[c] = append([]card{}, b.cards[c]...)
	}
	raw, _ := json.Marshal(map[string]any{"columns": out})
	return raw, fmt.Sprintf("todo %d · doing %d · done %d", len(out["todo"]), len(out["doing"]), len(out["done"]))
}

// advance moves one card to the next column; the last column keeps it.
func (b *board) advance(id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, col := range columns[:len(columns)-1] {
		for j, c := range b.cards[col] {
			if c.ID == id {
				b.cards[col] = append(b.cards[col][:j:j], b.cards[col][j+1:]...)
				b.cards[columns[i+1]] = append(b.cards[columns[i+1]], c)
				return nil
			}
		}
	}
	return fmt.Errorf("task-board: no movable card %q", id)
}

func (b *board) publish(ctx context.Context) error {
	session := b.session.Load()
	if session == nil {
		return nil
	}
	props, fallback := b.props()
	return b.ui.PublishFrame(ctx, session.SessionID, session.Generation, "board", extension.UIFramePayload{
		Frame: "board", Slot: "pane.tab", Title: "Task board", Fallback: fallback, Props: props,
	})
}

func (b *board) action(ctx context.Context, actionID string, args map[string]string) error {
	switch actionID {
	case "advance":
		if err := b.advance(args["card"]); err != nil {
			return err
		}
	case "refresh":
	default:
		return fmt.Errorf("task-board: unknown action %q", actionID)
	}
	return b.publish(ctx)
}

func main() {
	b := newBoard()
	err := extension.Serve(context.Background(), b, extension.Options{
		Name:    "task-board",
		Version: "0.1.0",
		Observer: func(ctx context.Context, event string, _ json.RawMessage) {
			if event == "session.start" {
				_ = b.publish(ctx)
			}
		},
		UI: extension.UIHandler{Action: b.action},
	})
	if err != nil {
		os.Exit(1)
	}
}
