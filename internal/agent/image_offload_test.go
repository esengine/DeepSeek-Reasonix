package agent

import (
	"context"
	"fmt"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func fileImageMessages(n int) []provider.Message {
	msgs := make([]provider.Message, 0, n)
	for i := range n {
		msgs = append(msgs, provider.Message{
			ID:      fmt.Sprintf("u%d", i),
			Role:    provider.RoleUser,
			Content: "img",
			Images:  []string{"file-api-abcd1234"},
		})
	}
	return msgs
}

func TestApplyImageOffloadDoesNotOmitUntilRecorded(t *testing.T) {
	a := New(nil, nil, NewSession(""), Options{}, event.Discard)
	var recorded provider.ImageOffloadPayload
	a.SetImageOffloadRecorder(func(p provider.ImageOffloadPayload) { recorded = p })
	msgs := fileImageMessages(9)
	got := a.applyImageOffload(msgs)
	if len(recorded.Targets) != 0 {
		t.Fatalf("proactive offload recorded: %+v", recorded)
	}
	if provider.ClassifyImage(got[0].Images[0]) == provider.ImageNone {
		t.Fatal("nine images must stay until a route budget fails")
	}
	if !a.recordImageOffloadCount(got, 2) || len(recorded.Targets) == 0 {
		t.Fatal("expected durable offload after a named count")
	}
	got = a.applyImageOffload(msgs)
	if provider.ClassifyImage(got[0].Images[0]) != provider.ImageNone || provider.ClassifyImage(got[1].Images[0]) != provider.ImageNone {
		t.Fatalf("oldest two should be omitted: %v %v", got[0].Images, got[1].Images)
	}
	if provider.ClassifyImage(got[8].Images[0]) == provider.ImageNone {
		t.Fatal("newest retained image was dropped")
	}
}

func TestEstimatedPromptTokensAddsV41ImageCost(t *testing.T) {
	a := &Agent{agentConfig: agentConfig{contextWindow: 1_000_000}}
	text := provider.Message{Role: provider.RoleUser, Content: "see"}
	withImage := text
	withImage.Images = []string{"data:image/png;base64,AA=="}
	plain := a.estimatedPromptTokens([]provider.Message{text})
	got := a.estimatedPromptTokens([]provider.Message{withImage})
	if got-plain != 1024 {
		t.Fatalf("image delta = %d, want 1024 for unknown pixels", got-plain)
	}
	offloaded := withImage
	offloaded.Images = []string{provider.ImageOffloadedRef}
	if a.estimatedPromptTokens([]provider.Message{offloaded}) != plain {
		t.Fatal("offloaded images must not add vision tokens")
	}
}

func TestPromptCalibrationDoesNotDoubleCountImages(t *testing.T) {
	a := &Agent{agentConfig: agentConfig{contextWindow: 1_000_000}}
	msgs := []provider.Message{{
		Role:    provider.RoleUser,
		Content: "hello",
		Images:  []string{"data:image/png;base64,AA=="},
	}}
	shape := requestCalibrationShapeOf(provider.Request{Messages: msgs})
	if shape.imageTokens != 1024 {
		t.Fatalf("imageTokens = %d, want 1024", shape.imageTokens)
	}
	a.setPromptTokenCalibration(shape.imageTokens+int(shape.requestChars), shape)
	got := a.estimatedPromptTokens(msgs)
	if got != int(shape.requestChars)+1024 {
		t.Fatalf("estimate = %d, want %d (text remainder + v41 image)", got, int(shape.requestChars)+1024)
	}
}

type countingVisionProvider struct {
	reqs []provider.Request
}

func (*countingVisionProvider) Name() string { return "vision" }
func (p *countingVisionProvider) ModelInfo() provider.ModelInfo {
	return provider.ModelInfo{InputModalities: []provider.ModelModality{provider.ModalityText, provider.ModalityImage}}
}
func (p *countingVisionProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.reqs = append(p.reqs, req)
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "ok"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestSamplingRecoversImageOffloadWithoutProviderRetry(t *testing.T) {
	prov := &countingVisionProvider{}
	a := New(prov, nil, NewSession(""), Options{ContextWindow: 1_000_000, CompactRatio: 2}, event.Discard)
	var recorded provider.ImageOffloadPayload
	a.SetImageOffloadRecorder(func(p provider.ImageOffloadPayload) { recorded = p })
	a.sess.conversation.Replace(fileImageMessages(provider.MaxImagesPerRequest + 1))
	got := a.streamWithSamplingRecovery(context.Background(), 1)
	if got.err != nil {
		t.Fatalf("recovery failed: %v", got.err)
	}
	if len(recorded.Targets) == 0 {
		t.Fatal("expected durable offload targets")
	}
	if len(prov.reqs) != 1 {
		t.Fatalf("provider calls = %d, want 1 after local offload", len(prov.reqs))
	}
	live := 0
	for _, msg := range prov.reqs[0].Messages {
		for _, ref := range msg.Images {
			if provider.ClassifyImage(ref) != provider.ImageNone {
				live++
			}
		}
	}
	if live != provider.MaxImagesPerRequest+1-provider.ImageOffloadCountQuantum {
		t.Fatalf("retained images = %d, want %d", live, provider.MaxImagesPerRequest+1-provider.ImageOffloadCountQuantum)
	}
}
