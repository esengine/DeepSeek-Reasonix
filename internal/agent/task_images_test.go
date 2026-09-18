package agent

import (
	"context"
	"testing"

	"reasonix/internal/attachment"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type urlImageResolver struct{}

func (urlImageResolver) ResolveRequestImages(_ context.Context, msgs []provider.Message) ([]provider.Message, error) {
	out := append([]provider.Message(nil), msgs...)
	for i := range out {
		if len(out[i].ImageInputs) == 0 {
			continue
		}
		images := make([]string, 0, len(out[i].ImageInputs))
		for _, in := range out[i].ImageInputs {
			if in.Kind == attachment.KindURL {
				images = append(images, in.URL)
			}
		}
		out[i].Images = images
		out[i].ImageInputs = nil
	}
	return out, nil
}

func (urlImageResolver) PersistToolImages(context.Context, []string) ([]attachment.ImageInput, error) {
	return nil, nil
}

func TestTaskToolPropagatesSubagentImageInputsWithoutCombiningImages(t *testing.T) {
	sub := &mockProvider{name: "sub", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "image received"},
		{Type: provider.ChunkDone},
	}}
	task := newTestTaskTool(t, sub, tool.NewRegistry(), "sys", "", "", nil).WithImageRequestResolver(urlImageResolver{})
	inputs := []attachment.ImageInput{{Kind: attachment.KindURL, URL: "https://example.invalid/shot.png"}}
	ctx := WithSubagentImageInputs(testTaskContext(), inputs)
	ctx = WithSubagentImageCandidates(ctx, []string{"data:image/png;base64,AAAA"})
	if _, err := task.Execute(ctx, []byte(`{"prompt":"inspect the attached image"}`)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var got provider.Message
	for _, msg := range sub.lastReq.Messages {
		if msg.Role == provider.RoleUser {
			got = msg
		}
	}
	if len(got.Images) != 1 || got.Images[0] != "https://example.invalid/shot.png" {
		t.Fatalf("sub-agent images = %v, want the resolved ImageInput URL", got.Images)
	}
	if len(got.ImageInputs) != 0 {
		t.Fatalf("request ImageInputs = %+v, want resolved away", got.ImageInputs)
	}
}
