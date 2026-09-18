package openai

import (
	"errors"
	"testing"

	"reasonix/internal/provider"
)

func TestBuildRequestImageMapTracksUserAndSyntheticToolImages(t *testing.T) {
	c := &client{model: "gpt-4o", name: "openai", chatURL: "https://api.example/v1/chat/completions", vision: true}
	request, locations := c.buildRequestWithImageMap(provider.Request{Messages: []provider.Message{
		{ID: "user", Role: provider.RoleUser, Content: "inspect", Images: []string{"data:image/png;base64,AAAA", "data:image/png;base64,BBBB"}},
		{ID: "assistant", Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "call", Name: "shot", Arguments: "{}"}}},
		{ID: "tool", Role: provider.RoleTool, ToolCallID: "call", Name: "shot", Content: "image", Images: []string{"data:image/png;base64,CCCC"}},
	}})
	if len(request.Messages) != 4 || len(locations) != 3 {
		t.Fatalf("messages=%d locations=%+v", len(request.Messages), locations)
	}
	if locations[0].WireMessageIndex != 0 || locations[0].WireContentIndex != 1 || locations[0].Identity.MessageID != "user" || locations[0].Identity.ImageOrdinal != 0 {
		t.Fatalf("first location=%+v", locations[0])
	}
	if locations[1].WireMessageIndex != 0 || locations[1].WireContentIndex != 2 || locations[1].Identity.ImageOrdinal != 1 {
		t.Fatalf("second location=%+v", locations[1])
	}
	if locations[2].WireMessageIndex != 3 || locations[2].WireContentIndex != 1 || locations[2].Identity.MessageID != "tool" {
		t.Fatalf("tool location=%+v", locations[2])
	}
}

func TestClassifyImageRequestErrorRequiresExplicitUniqueLocation(t *testing.T) {
	c := &client{
		model: "deepseek-v4-flash", name: "deepseek", chatURL: "https://api.deepseek.com/v1/chat/completions?secret=nope",
		identity: provider.RequestIdentity{Protocol: "openai"}, vision: true,
	}
	_, locations := c.buildRequestWithImageMap(provider.Request{Messages: []provider.Message{{
		ID: "message", Role: provider.RoleUser, Content: "inspect",
		Images: []string{"data:image/png;base64,AAAA", "data:image/png;base64,BBBB"},
	}}})
	apiErr := &provider.APIError{Provider: "deepseek", Protocol: "openai", Status: 400, Body: `{"error":{"message":"unsupported image format at messages[0].content[2]"}}`, TraceID: "trace"}
	classified := c.classifyImageRequestError(apiErr, locations)
	var imageErr *provider.ImageRequestError
	if !errors.As(classified, &imageErr) || imageErr.Identity.MessageID != "message" || imageErr.Identity.ImageOrdinal != 1 {
		t.Fatalf("classified=%T %+v", classified, imageErr)
	}
	if imageErr.APIError != apiErr || imageErr.Scope.Endpoint != "https://api.deepseek.com/v1/chat/completions" || imageErr.Scope.Model != c.model {
		t.Fatalf("image error lost API identity: %+v", imageErr)
	}

	ambiguous := &provider.APIError{Status: 400, Body: `{"error":{"message":"corrupt image at messages[0]"}}`}
	classified = c.classifyImageRequestError(ambiguous, locations)
	imageErr = nil
	if !errors.As(classified, &imageErr) || len(imageErr.Candidates) != 2 {
		t.Fatalf("ambiguous classification=%T %+v", classified, imageErr)
	}
	if action := provider.ImageRecoveryForError(imageErr); action == nil || len(action.Candidates) != 2 {
		t.Fatalf("manual recovery action=%+v", action)
	}

	for name, candidate := range map[string]*provider.APIError{
		"ordinary_400": {Status: 400, Body: `{"error":{"message":"invalid temperature at messages[0]"}}`},
		"policy":       {Status: 400, Body: `{"error":{"message":"content policy rejected image at messages[0].content[1]"}}`},
	} {
		t.Run(name, func(t *testing.T) {
			if got := c.classifyImageRequestError(candidate, locations); !errors.Is(got, candidate) {
				t.Fatalf("unexpected classification: %T %v", got, got)
			}
		})
	}
}
