package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func decodeFrame(t *testing.T, body string) (UIFramePayload, error) {
	t.Helper()
	out, err := DecodeUIPublishPayload(UISurfaceFrame, json.RawMessage(body))
	if err != nil {
		return UIFramePayload{}, err
	}
	frame, ok := out.(UIFramePayload)
	if !ok {
		t.Fatalf("decoded %T, want UIFramePayload", out)
	}
	return frame, nil
}

func TestFrameDecodesADeclaredDocumentWithProps(t *testing.T) {
	frame, err := decodeFrame(t, `{"frame":"board","slot":"pane.tab","title":"Task board","fallback":"3 cards in progress","props":{"cards":[{"id":"a","title":"Ship"}]}}`)
	if err != nil {
		t.Fatal(err)
	}
	if frame.Frame != "board" || frame.Slot != "pane.tab" || frame.Title != "Task board" || frame.Fallback != "3 cards in progress" {
		t.Fatalf("frame = %+v", frame)
	}
	if string(frame.Props) != `{"cards":[{"id":"a","title":"Ship"}]}` {
		t.Fatalf("props = %s", frame.Props)
	}
}

func TestFrameRefusesWhatItCannotCarry(t *testing.T) {
	big := `"` + strings.Repeat("x", MaxFramePropsBytes) + `"`
	cases := map[string]string{
		"anchor":         `{"frame":"board","title":"t","fallback":"f","anchor":"tool:c1"}`,
		"markup":         `{"frame":"board","title":"t","fallback":"f","html":"<script>"}`,
		"no fallback":    `{"frame":"board","title":"t"}`,
		"no title":       `{"frame":"board","fallback":"f"}`,
		"bad id":         `{"frame":"../board","title":"t","fallback":"f"}`,
		"upper id":       `{"frame":"Board","title":"t","fallback":"f"}`,
		"long id":        `{"frame":"` + strings.Repeat("a", 33) + `","title":"t","fallback":"f"}`,
		"long slot":      `{"frame":"board","slot":"` + strings.Repeat("s", MaxViewSlotID+1) + `","title":"t","fallback":"f"}`,
		"long title":     `{"frame":"board","title":"` + strings.Repeat("t", MaxViewLabel+1) + `","fallback":"f"}`,
		"long fallback":  `{"frame":"board","title":"t","fallback":"` + strings.Repeat("f", MaxFrameFallbackBytes+1) + `"}`,
		"array props":    `{"frame":"board","title":"t","fallback":"f","props":[1]}`,
		"string props":   `{"frame":"board","title":"t","fallback":"f","props":"x"}`,
		"oversize props": `{"frame":"board","title":"t","fallback":"f","props":{"x":` + big + `}}`,
	}
	for name, body := range cases {
		if _, err := decodeFrame(t, body); err == nil {
			t.Errorf("%s: accepted %s", name, body[:min(len(body), 80)])
		}
	}
}
