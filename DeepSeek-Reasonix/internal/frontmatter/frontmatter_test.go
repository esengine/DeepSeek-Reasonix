package frontmatter

import (
	"strings"
	"testing"
)

func TestSplitNoFence(t *testing.T) {
	fm, body := Split("just body text\nno fence")
	if len(fm) != 0 {
		t.Errorf("expected empty fm, got %v", fm)
	}
	if !strings.Contains(body, "just body text") {
		t.Errorf("body = %q", body)
	}
}

func TestSplitUnclosedFence(t *testing.T) {
	fm, body := Split("---\nkey: val\n\nno closing fence")
	if len(fm) != 0 {
		t.Errorf("unclosed fence should return empty fm, got %v", fm)
	}
	if !strings.Contains(body, "---") {
		t.Errorf("body should contain original content: %q", body)
	}
}

func TestSplitEmptyBody(t *testing.T) {
	fm, body := Split("---\nkey: val\n---\n")
	if fm["key"] != "val" {
		t.Errorf("key = %q", fm["key"])
	}
	if strings.TrimSpace(body) != "" {
		t.Errorf("expected empty body, got %q", body)
	}
}

func TestSplitNestedMetadata(t *testing.T) {
	fm, body := Split("---\nname: test\ndescription: desc\nmetadata:\n  type: user\n---\n\nbody here")
	if fm["name"] != "test" {
		t.Errorf("name = %q", fm["name"])
	}
	if fm["description"] != "desc" {
		t.Errorf("description = %q", fm["description"])
	}
	if fm["type"] != "user" {
		t.Errorf("type = %q, expected flattened from metadata", fm["type"])
	}
	if !strings.Contains(body, "body here") {
		t.Errorf("body = %q", body)
	}
}

func TestSplitCRLF(t *testing.T) {
	fm, body := Split("---\r\nname: test\r\n---\r\nbody\r\n")
	if fm["name"] != "test" {
		t.Errorf("name = %q", fm["name"])
	}
	if !strings.Contains(body, "body") {
		t.Errorf("body = %q", body)
	}
}

func TestSplitQuotedValues(t *testing.T) {
	fm, _ := Split("---\nname: test\ndescription: \"quoted desc\"\n---\n")
	if fm["description"] != "quoted desc" {
		t.Errorf("description should be unquoted: %q", fm["description"])
	}
}

func TestSplitSingleQuotes(t *testing.T) {
	fm, _ := Split("---\nname: test\ndescription: 'single quoted'\n---\n")
	if fm["description"] != "single quoted" {
		t.Errorf("description should be unquoted: %q", fm["description"])
	}
}

func TestSplitEmptyInput(t *testing.T) {
	fm, body := Split("")
	if len(fm) != 0 {
		t.Errorf("empty input should return empty fm, got %v", fm)
	}
	if body != "" {
		t.Errorf("body = %q", body)
	}
}

func TestSplitOnlyFence(t *testing.T) {
	fm, body := Split("---\n---\n")
	if len(fm) != 0 {
		t.Errorf("empty fence should return empty fm, got %v", fm)
	}
	if strings.TrimSpace(body) != "" {
		t.Errorf("body = %q", body)
	}
}

func TestSplitMultipleKeys(t *testing.T) {
	fm, _ := Split("---\na: 1\nb: 2\nc: 3\n---\n")
	if fm["a"] != "1" || fm["b"] != "2" || fm["c"] != "3" {
		t.Errorf("fm = %v", fm)
	}
}

func TestSplitCaseInsensitive(t *testing.T) {
	fm, _ := Split("---\nName: Test\nDESCRIPTION: desc\n---\n")
	if fm["name"] != "Test" {
		t.Errorf("name = %q", fm["name"])
	}
	if fm["description"] != "desc" {
		t.Errorf("description = %q", fm["description"])
	}
}
