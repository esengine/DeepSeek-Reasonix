package agent

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
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

func validTaskPNGDataURL(t *testing.T) string {
	t.Helper()
	var buf bytes.Buffer
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.SetNRGBA(0, 0, color.NRGBA{R: 1, G: 2, B: 3, A: 255})
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

func TestTaskToolPropagatesSubagentImageCandidates(t *testing.T) {
	sub := &mockProvider{name: "sub", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "image received"},
		{Type: provider.ChunkDone},
	}}
	task := newTestTaskTool(t, sub, tool.NewRegistry(), "sys", "", "", nil)
	imageURL := validTaskPNGDataURL(t)
	ctx := WithSubagentImageCandidates(testTaskContext(), []string{imageURL})
	if _, err := task.Execute(ctx, []byte(`{"prompt":"inspect the attached image"}`)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var images []string
	for _, msg := range sub.lastReq.Messages {
		if msg.Role == provider.RoleUser {
			images = msg.Images
		}
	}
	if len(images) != 1 || images[0] != imageURL {
		t.Fatalf("sub-agent images = %v, want the parent candidate", images)
	}
}
