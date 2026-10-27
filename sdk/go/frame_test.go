package extension

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestHostUIPublishFrameGolden(t *testing.T) {
	host, err := uiCall(t, func(ctx context.Context) error {
		return HostUI{}.PublishFrame(ctx, "sess-1", 7, "kanban", UIFramePayload{
			Frame: "board", Slot: "pane.tab", Title: "Task board", Fallback: "3 cards", Props: json.RawMessage(`{"cards":3}`),
		})
	})
	if err != nil {
		t.Fatalf("PublishFrame: %v", err)
	}
	var golden map[string]any
	if err := json.Unmarshal(lastRawParams(t, host, MethodHostUIPublish), &golden); err != nil {
		t.Fatal(err)
	}
	assertJSONFields(t, golden, map[string]any{"surfaceId": "kanban", "sessionId": "sess-1", "kind": "frame"})
	payload, _ := golden["payload"].(map[string]any)
	assertJSONFields(t, payload, map[string]any{"frame": "board", "slot": "pane.tab", "title": "Task board", "fallback": "3 cards"})
	if props, _ := payload["props"].(map[string]any); props["cards"] != float64(3) {
		t.Fatalf("props = %v", payload["props"])
	}
}

func TestHostUIFrameValidationRunsBeforeTheConnection(t *testing.T) {
	ui := HostUI{}
	ctx := context.Background()
	ok := UIFramePayload{Frame: "board", Title: "t", Fallback: "f"}
	bad := []UIFramePayload{
		{Title: "t", Fallback: "f"},
		{Frame: "Board", Title: "t", Fallback: "f"},
		{Frame: "board", Fallback: "f"},
		{Frame: "board", Title: "t"},
		{Frame: "board", Title: "t", Fallback: strings.Repeat("f", 601)},
		{Frame: "board", Title: "t", Fallback: "f", Props: json.RawMessage(`[1]`)},
		{Frame: "board", Title: "t", Fallback: "f", Props: json.RawMessage(`{"x":"` + strings.Repeat("x", 64<<10) + `"}`)},
	}
	for i, p := range bad {
		err := ui.PublishFrame(ctx, "s", 1, "x", p)
		if err == nil || errors.Is(err, ErrNoConnection) {
			t.Fatalf("case %d: %v", i, err)
		}
	}
	if err := ui.PublishFrame(ctx, "s", 1, "x", ok); !errors.Is(err, ErrNoConnection) {
		t.Fatalf("a valid frame failed validation: %v", err)
	}
}
