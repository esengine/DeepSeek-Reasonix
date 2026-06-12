package anthropic

import (
	"testing"

	"reasonix/internal/provider"
)

func TestBuildRequestEmbedsImageBlockForVisionModel(t *testing.T) {
	c := &client{model: "claude-opus-4-8", vision: true}
	req := c.buildRequest(provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "describe", Images: []string{"data:image/jpeg;base64,ZZZZ"}},
		},
	})
	blocks := req.Messages[0].Content
	if len(blocks) != 2 || blocks[0].Type != "text" || blocks[1].Type != "image" {
		t.Fatalf("blocks = %+v, want [text, image]", blocks)
	}
	src := blocks[1].Source
	if src == nil || src.Type != "base64" || src.MediaType != "image/jpeg" || src.Data != "ZZZZ" {
		t.Fatalf("image source = %+v, want base64 / image/jpeg / ZZZZ", src)
	}
}

func TestBuildRequestSkipsImageBlockWithoutVision(t *testing.T) {
	c := &client{model: "claude-opus-4-8"} // vision unset
	req := c.buildRequest(provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "describe", Images: []string{"data:image/jpeg;base64,ZZZZ"}},
		},
	})
	blocks := req.Messages[0].Content
	if len(blocks) != 1 || blocks[0].Type != "text" {
		t.Fatalf("blocks = %+v, want [text] only when vision is off", blocks)
	}
}
