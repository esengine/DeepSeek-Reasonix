package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	extension "github.com/esengine/DeepSeek-Reasonix/sdk/go"
)

type echoProvider struct{ id string }

func (p echoProvider) descriptor() extension.ProviderDescriptor {
	return extension.ProviderDescriptor{
		Ref:           "plugin/" + p.id + "/offline/echo",
		DisplayName:   "Offline echo (SDK example)",
		Model:         "echo",
		ContextWindow: 64000,
	}
}

func (p echoProvider) Initialize(_ context.Context, params extension.InitializeParams) (*extension.InitializeResult, error) {
	return &extension.InitializeResult{
		Providers: []extension.ProviderDescriptor{p.descriptor()},
		Provides:  append([]extension.CapabilityWire(nil), params.Manifest.Provides...),
	}, nil
}

func (p echoProvider) Catalog(context.Context) ([]extension.ProviderDescriptor, error) {
	return []extension.ProviderDescriptor{p.descriptor()}, nil
}

func (p echoProvider) Stream(ctx context.Context, req extension.StreamRequest) (<-chan extension.StreamChunk, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if req.ProviderRef != p.descriptor().Ref {
		return nil, fmt.Errorf("unknown offline provider ref %q", req.ProviderRef)
	}
	var text string
	for i := len(req.Request.Messages) - 1; i >= 0; i-- {
		if req.Request.Messages[i].Role == extension.ProviderRoleUser {
			text = req.Request.Messages[i].Content
			break
		}
	}
	chunks := make(chan extension.StreamChunk)
	go func() {
		defer close(chunks)
		for _, chunk := range []extension.StreamChunk{extension.TextChunk("Offline echo: "), extension.TextChunk(text), extension.DoneChunk()} {
			select {
			case <-ctx.Done():
				return
			case chunks <- chunk:
			}
		}
	}()
	return chunks, nil
}

func main() {
	id := strings.TrimSpace(os.Getenv("REASONIX_PLUGIN_NAME"))
	if id == "" {
		id = "echo-provider"
	}
	p := echoProvider{id: id}
	if err := extension.Serve(context.Background(), p, extension.Options{
		Name: id, Version: "0.1.0", Provider: p,
	}); err != nil {
		os.Exit(1)
	}
}
