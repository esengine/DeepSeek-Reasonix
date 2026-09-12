package transcript

import (
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/provider"
)

func TestPresentedFilesJSONRoundTripAndOmitEmpty(t *testing.T) {
	want := []provider.PresentedFile{{Path: "game.html", Description: "Playable game"}}
	encoded, err := json.Marshal(Message{Role: "tool", ToolCallID: "present-1", PresentedFiles: want})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"presentedFiles"`) {
		t.Fatalf("trusted metadata missing from transcript JSON: %s", encoded)
	}
	var decoded Message
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.PresentedFiles) != 1 || decoded.PresentedFiles[0] != want[0] {
		t.Fatalf("round trip = %#v, want %#v", decoded.PresentedFiles, want)
	}
	empty, err := json.Marshal(Message{Role: "tool", ToolCallID: "ordinary"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(empty), `"presentedFiles"`) {
		t.Fatalf("empty metadata was not omitted: %s", empty)
	}
}
