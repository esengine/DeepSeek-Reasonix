package historycatalog

import (
	"strings"
	"testing"
	"unicode/utf8"

	"reasonix/internal/provider"
)

func TestTruncateToolText(t *testing.T) {
	t.Parallel()
	patterned := strings.Repeat("0123456789", 10240) // 100KB, position-varying
	tests := []struct {
		name      string
		text      string
		truncated bool
	}{
		{"short unchanged", strings.Repeat("a", 100), false},
		{"at limit unchanged", patterned[:toolTextMaxBytes], false},
		{"one byte over truncated", patterned[:toolTextMaxBytes+1], true},
		{"large truncated", patterned, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := truncateToolText(tt.text)
			if !tt.truncated {
				if got != tt.text {
					t.Fatalf("text changed: got %d bytes, want unchanged %d", len(got), len(tt.text))
				}
				return
			}
			if !strings.Contains(got, toolTextTruncationMarker) {
				t.Fatalf("missing truncation marker in %q…", got[:64])
			}
			if !strings.HasPrefix(got, tt.text[:toolTextHeadBytes]) {
				t.Fatal("head bytes altered")
			}
			if !strings.HasSuffix(got, tt.text[len(tt.text)-toolTextTailBytes:]) {
				t.Fatal("tail bytes altered")
			}
			if len(got) > toolTextMaxBytes+len(toolTextTruncationMarker) {
				t.Fatalf("truncated text still %d bytes", len(got))
			}
		})
	}
}

func TestTruncateToolTextKeepsRuneBoundary(t *testing.T) {
	t.Parallel()
	prefix := strings.Repeat("x", toolTextHeadBytes-1)
	// Byte toolTextHeadBytes lands inside the multi-byte rune 世.
	text := prefix + "世界" + strings.Repeat("y", 64*1024)
	got := truncateToolText(text)
	if !utf8.ValidString(got) {
		t.Fatal("truncated text is not valid UTF-8")
	}
	if !strings.HasPrefix(got, prefix) {
		t.Fatal("head lost bytes before the boundary rune")
	}
}

func TestDocumentsTruncateToolPayloadsOnly(t *testing.T) {
	t.Parallel()
	filler := strings.Repeat("filler ", 4000) // 28KB
	toolContent := filler[:10000] + " midmarkertoken " + filler[10000:] + " tailmarkertoken"
	userContent := filler + " endusermarkertoken"
	docs := documents([]provider.Message{
		{Role: provider.RoleUser, Content: userContent},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "1", Name: "bash", Arguments: toolContent}}},
		{Role: provider.RoleTool, ToolCallID: "1", Name: "bash", Content: toolContent},
	})
	byKind := map[string]indexedDocument{}
	for _, doc := range docs {
		byKind[doc.kind] = doc
	}
	user, ok := byKind["user_text"]
	if !ok || !strings.Contains(user.terms, "endusermarkertoken") {
		t.Fatalf("user_text must stay untruncated: %#v", user)
	}
	for _, kind := range []string{"tool_input", "tool_output"} {
		doc, ok := byKind[kind]
		if !ok {
			t.Fatalf("missing %s document", kind)
		}
		if !strings.Contains(doc.terms, "tailmarkertoken") {
			t.Fatalf("%s lost its tail token", kind)
		}
		if strings.Contains(doc.terms, "midmarkertoken") {
			t.Fatalf("%s kept elided middle token", kind)
		}
		if !strings.Contains(doc.terms, "truncated") {
			t.Fatalf("%s is missing the truncation marker", kind)
		}
	}
}
