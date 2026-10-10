package conformance

import (
	"encoding/json"
	"testing"

	"reasonix/internal/ext/extension/protocol"
)

func TestToolBeforeMalformedArgumentsContinue(t *testing.T) {
	client := startExample(t, nil, nil)
	for _, args := range []string{"null", " \nnull\t ", "[]", "true", `"text"`, "{"} {
		t.Run(args, func(t *testing.T) {
			payload, err := json.Marshal(map[string]string{"name": "read", "arguments": args})
			if err != nil {
				t.Fatal(err)
			}
			result := intercept(t, client, protocol.EventToolBefore, string(payload))
			if result.Decision != protocol.DecisionContinue {
				t.Fatalf("decision = %q, want continue", result.Decision)
			}
			if len(result.Replacement) != 0 {
				t.Fatalf("unexpected replacement: %s", result.Replacement)
			}
		})
	}

	rewritten := intercept(t, client, protocol.EventToolBefore, `{"name":"read","arguments":"{\"path\":\"/fixture\",\"sandbox\":false}"}`)
	var replacement struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}
	decodeReplacement(t, rewritten, &replacement)
	var args map[string]any
	if err := json.Unmarshal([]byte(replacement.Arguments), &args); err != nil {
		t.Fatal(err)
	}
	if replacement.Name != "read" || len(args) != 2 || args["path"] != "/fixture" || args["sandbox"] != true {
		t.Fatalf("replacement = %+v, arguments = %v", replacement, args)
	}
}
