package normalize

import (
	"github.com/larksuite/oapi-sdk-go/v3/channel/types"
	"reflect"
	"testing"
)

func TestComposeMentionsTextPrefix(t *testing.T) {
	mentions := []types.Mention{
		{UserID: "ou_123", Name: "Alice"},
		{UserID: "ou_456", Name: "Bob"},
	}

	prefix := ComposeMentionsTextPrefix(mentions)
	expected := `<at user_id="ou_123">Alice</at> <at user_id="ou_456">Bob</at> `
	if prefix != expected {
		t.Errorf("Expected %q, got %q", expected, prefix)
	}

	empty := ComposeMentionsTextPrefix(nil)
	if empty != "" {
		t.Errorf("Expected empty string, got %q", empty)
	}
}

func TestComposePostMentionElements(t *testing.T) {
	mentions := []types.Mention{
		{UserID: "ou_123", Name: "Alice"},
	}

	elements := ComposePostMentionElements(mentions)
	if len(elements) != 1 {
		t.Fatalf("Expected 1 element, got %d", len(elements))
	}

	expected := postElement{
		Tag:      "at",
		UserID:   "ou_123",
		UserName: "Alice",
	}

	if !reflect.DeepEqual(elements[0], expected) {
		t.Errorf("Expected %+v, got %+v", expected, elements[0])
	}
}
