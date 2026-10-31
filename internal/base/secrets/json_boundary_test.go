package secrets

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestJSONMasksDecodedValuesAndKeysWithoutRewritingCleanBytes(t *testing.T) {
	watch("JSON_BOUNDARY", "unique-json-boundary-secret")
	watch("JSON_PIN", "6207")
	m := Masker()
	if m.JSON(nil) != nil {
		t.Fatal("an absent JSON schema became a non-nil invalid document")
	}
	for _, raw := range []string{
		`{"value":"\u0075nique-json-boundary-secret"}`,
		`{"unique-json-boundary-secret":"value"}`,
		`{"k":"\u0075nique-json-boundary-secret","k":"safe"}`,
		`{"k":"unique-json-boundary-secret","k":"safe"}`,
	} {
		out := m.JSON(json.RawMessage(raw))
		if strings.Contains(string(out), "unique-json-boundary-secret") || strings.Contains(string(out), `\u0075nique-json-boundary-secret`) {
			t.Fatalf("JSON leaked a shadowed secret: %s", out)
		}
		var decoded any
		if err := json.Unmarshal(out, &decoded); err != nil {
			t.Fatal(err)
		}
		normalized, _ := json.Marshal(decoded)
		if strings.Contains(string(normalized), "unique-json-boundary-secret") || !strings.Contains(string(out), "[secret:JSON_BOUNDARY]") {
			t.Fatalf("JSON leaked a decoded secret: %s", out)
		}
	}
	clean := json.RawMessage("{ \"z\": 12345678901234567890, \"a\": \"\\u0061\" }")
	if got := m.JSON(clean); string(got) != string(clean) {
		t.Fatalf("clean JSON bytes changed: %s", got)
	}
	if got := m.JSON(json.RawMessage(`{"pin":6207}`)); string(got) != `{"pin":"[secret:JSON_PIN]"}` {
		t.Fatalf("numeric PIN was not masked: %s", got)
	}
}

func TestMarksJSONIncludesEscapedShadowedKeys(t *testing.T) {
	watch("JSON_MARK", "unique-json-mark-secret")
	got := Masker().MarksJSON(json.RawMessage(`{"k":"\u005bsecret:JSON_MARK]","k":"safe"}`))
	if len(got) != 1 || got[0] != "JSON_MARK" {
		t.Fatalf("marks = %v", got)
	}
}
