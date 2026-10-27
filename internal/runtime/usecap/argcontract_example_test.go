package usecap

import (
	"encoding/json"
	"strings"
	"testing"
)

const todoLikeSchema = `{"type":"object","properties":{"todos":{"type":"array","items":{"type":"object","properties":{` +
	`"content":{"type":"string"},"status":{"type":"string","enum":["pending","in_progress","completed"]},` +
	`"level":{"type":"integer","minimum":0},"activeForm":{"type":"string"}},"required":["content","status"]}}},"required":["todos"]}`

func TestMissingRequiredFieldCarriesAMinimalValidCall(t *testing.T) {
	c, ok := ReadArgumentContract(json.RawMessage(todoLikeSchema), json.RawMessage(`{"todos":[{"content":"write it"}]}`))
	if !ok || len(c.Missing) != 1 || c.Missing[0] != "status" {
		t.Fatalf("contract = %+v ok=%v", c, ok)
	}
	hint := c.Hint()
	const want = `{"todos":[{"content":"…","status":"pending"}]}`
	if !strings.Contains(hint, want) {
		t.Fatalf("hint %q does not carry the example call %s", hint, want)
	}
	var shown map[string]any
	start := strings.Index(hint, want)
	if err := json.Unmarshal([]byte(hint[start:start+len(want)]), &shown); err != nil {
		t.Fatalf("the example is not JSON: %v", err)
	}
	// The example itself satisfies the schema it was made from.
	if again, ok := ReadArgumentContract(json.RawMessage(todoLikeSchema), json.RawMessage(want)); !ok || again.Broken() {
		t.Fatalf("the example breaks its own schema: %+v", again)
	}
}

func TestExampleFollowsTheSchemaForEveryKind(t *testing.T) {
	schema := `{"type":"object","properties":{"path":{"type":"string"},"n":{"type":"integer","minimum":3},` +
		`"on":{"type":"boolean"},"mode":{"type":"string","enum":["a","b"]},"tags":{"type":"array","items":{"type":"string"}},` +
		`"opt":{"type":"string"}},"required":["path","n","on","mode","tags"]}`
	c, ok := ReadArgumentContract(json.RawMessage(schema), json.RawMessage(`{}`))
	if !ok || len(c.Missing) != 5 {
		t.Fatalf("contract = %+v", c)
	}
	const want = `{"mode":"a","n":3,"on":false,"path":"…","tags":["…"]}`
	if !strings.Contains(c.Hint(), want) {
		t.Fatalf("hint %q lacks %s", c.Hint(), want)
	}
}

func TestMistypedAloneCarriesNoExample(t *testing.T) {
	c, _ := ReadArgumentContract(json.RawMessage(todoLikeSchema), json.RawMessage(`{"todos":"nope"}`))
	if !c.Broken() || strings.Contains(c.Hint(), "valid call") {
		t.Fatalf("hint %q", c.Hint())
	}
}
