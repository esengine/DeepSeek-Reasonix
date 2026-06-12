package openai

import (
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/provider"
)

func TestBuildRequestEmbedsImagesForVisionModel(t *testing.T) {
	c := &client{model: "gpt-4o", vision: true}
	req := c.buildRequest(provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "what is this", Images: []string{"data:image/png;base64,AAAA"}},
		},
	})
	parts, ok := req.Messages[0].Content.([]chatContentPart)
	if !ok {
		t.Fatalf("vision user content = %T, want []chatContentPart", req.Messages[0].Content)
	}
	if len(parts) != 2 || parts[0].Type != "text" || parts[1].Type != "image_url" {
		t.Fatalf("parts = %+v, want [text, image_url]", parts)
	}
	if parts[1].ImageURL == nil || parts[1].ImageURL.URL != "data:image/png;base64,AAAA" {
		t.Fatalf("image_url = %+v, want the data URL", parts[1].ImageURL)
	}
	body, _ := json.Marshal(req.Messages[0])
	if !strings.Contains(string(body), `"type":"image_url"`) {
		t.Errorf("serialized content missing image_url part: %s", body)
	}
}

func TestBuildRequestSkipsImagesWithoutVision(t *testing.T) {
	c := &client{model: "deepseek-v4"} // vision unset
	req := c.buildRequest(provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "ignore the image", Images: []string{"data:image/png;base64,AAAA"}},
		},
	})
	if s, ok := req.Messages[0].Content.(string); !ok || s != "ignore the image" {
		t.Fatalf("non-vision content = %#v, want plain string", req.Messages[0].Content)
	}
}
