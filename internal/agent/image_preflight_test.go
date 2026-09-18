package agent

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"reasonix/internal/attachment"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

type imageIsolationRecorder struct {
	decisions []provider.ImageIsolationDecision
	err       error
}

func (*imageIsolationRecorder) CheckpointSession(context.Context, SessionCheckpointBoundary) error {
	return nil
}

func (r *imageIsolationRecorder) RecordSessionImageIsolation(_ context.Context, decision provider.ImageIsolationDecision) error {
	if r.err != nil {
		return r.err
	}
	r.decisions = append(r.decisions, decision)
	return nil
}

func validInlinePNG(t *testing.T) string {
	t.Helper()
	var data bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	if err := png.Encode(&data, img); err != nil {
		t.Fatal(err)
	}
	return attachment.DataURL("image/png", data.Bytes())
}

func TestImagePreflightIsolatesOnlyLocallyProvenBadImage(t *testing.T) {
	recorder := &imageIsolationRecorder{}
	a := New(&mockProvider{name: "p"}, echoRegistry(), NewSession(""), Options{}, event.Discard)
	a.SetSessionCheckpointer(recorder)
	valid := validInlinePNG(t)
	invalid := "data:image/png;base64,not-base64!"
	messages := []provider.Message{{
		ID: "history", Role: provider.RoleUser, Content: "inspect both",
		Images: []string{valid, invalid, "https://cdn.example.com/later.png"},
	}}
	projected, err := a.preflightRequestImages(t.Context(), messages)
	if err != nil {
		t.Fatal(err)
	}
	if len(recorder.decisions) != 1 || recorder.decisions[0].Identity.ImageOrdinal != 1 || recorder.decisions[0].Reason != "corrupt_image" {
		t.Fatalf("decisions=%+v", recorder.decisions)
	}
	if len(projected) != 1 || len(projected[0].Images) != 2 || projected[0].Images[0] != valid || projected[0].Images[1] != "https://cdn.example.com/later.png" {
		t.Fatalf("projected images=%#v", projected)
	}
	if projected[0].Content != "inspect both\n\n"+provider.ImageUnavailablePlaceholder {
		t.Fatalf("content=%q", projected[0].Content)
	}
	if len(messages[0].Images) != 3 || messages[0].Images[1] != invalid {
		t.Fatal("preflight mutated canonical input")
	}
}

type imageRetryProvider struct {
	requests []provider.Request
	identity provider.ImageIdentity
	always   bool
}

func (*imageRetryProvider) Name() string { return "p" }

func (p *imageRetryProvider) Stream(_ context.Context, request provider.Request) (<-chan provider.Chunk, error) {
	p.requests = append(p.requests, freezeProviderRequest(request))
	if len(p.requests) == 1 || p.always {
		return nil, &provider.ImageRequestError{
			APIError: &provider.APIError{Provider: "p", Status: 400, Body: "unsupported image format"},
			Reason:   "provider_rejected_image", Identity: p.identity,
			Scope: provider.ImageIsolationScope{Kind: "provider", Provider: "p"},
		}
	}
	chunks := make(chan provider.Chunk, 2)
	chunks <- provider.Chunk{Type: provider.ChunkText, Text: "recovered"}
	chunks <- provider.Chunk{Type: provider.ChunkDone}
	close(chunks)
	return chunks, nil
}

func TestImageRequestRecoveryPersistsAndRetriesExactlyOnce(t *testing.T) {
	image := validInlinePNG(t)
	identity := provider.ImageIdentity{MessageID: "historical", ImageOrdinal: 0, ContentDigest: provider.ImageContentDigest(image)}
	providerStub := &imageRetryProvider{identity: identity}
	recorder := &imageIsolationRecorder{}
	session := NewSession("").CloneWithMessages([]provider.Message{{ID: "historical", Role: provider.RoleUser, Content: "old image", Images: []string{image}}})
	a := New(providerStub, echoRegistry(), session, Options{}, event.Discard)
	a.SetSessionCheckpointer(recorder)
	if err := a.Run(withNoClosedLoop(t.Context()), "continue"); err != nil {
		t.Fatal(err)
	}
	if len(providerStub.requests) != 2 || len(recorder.decisions) != 1 {
		t.Fatalf("requests=%d decisions=%+v", len(providerStub.requests), recorder.decisions)
	}
	if len(providerStub.requests[0].Messages[0].Images) != 1 || len(providerStub.requests[1].Messages[0].Images) != 0 {
		t.Fatalf("request image counts first=%d second=%d", len(providerStub.requests[0].Messages[0].Images), len(providerStub.requests[1].Messages[0].Images))
	}
	if !strings.Contains(providerStub.requests[1].Messages[0].Content, provider.ImageUnavailablePlaceholder) {
		t.Fatalf("retry content=%q", providerStub.requests[1].Messages[0].Content)
	}
}

func TestImageRequestRecoveryDoesNotRetryTwice(t *testing.T) {
	image := validInlinePNG(t)
	providerStub := &imageRetryProvider{always: true, identity: provider.ImageIdentity{MessageID: "historical", ImageOrdinal: 0, ContentDigest: provider.ImageContentDigest(image)}}
	recorder := &imageIsolationRecorder{}
	session := NewSession("").CloneWithMessages([]provider.Message{{ID: "historical", Role: provider.RoleUser, Content: "old image", Images: []string{image}}})
	a := New(providerStub, echoRegistry(), session, Options{}, event.Discard)
	a.SetSessionCheckpointer(recorder)
	if err := a.Run(withNoClosedLoop(t.Context()), "continue"); err == nil {
		t.Fatal("second provider rejection unexpectedly recovered")
	}
	if len(providerStub.requests) != 2 || len(recorder.decisions) != 1 {
		t.Fatalf("requests=%d decisions=%d", len(providerStub.requests), len(recorder.decisions))
	}
}
