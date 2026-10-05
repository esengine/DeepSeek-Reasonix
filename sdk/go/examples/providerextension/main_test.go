package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	extension "github.com/esengine/DeepSeek-Reasonix/sdk/go"
)

func TestEchoProviderManifestAndCatalog(t *testing.T) {
	raw, err := os.ReadFile("reasonix-plugin.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Name     string                     `json:"name"`
		Provides []extension.CapabilityWire `json:"provides"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	p := echoProvider{id: manifest.Name}
	result, err := p.Initialize(context.Background(), extension.InitializeParams{
		Manifest: extension.ManifestExpectation{Provides: manifest.Provides},
	})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := p.Catalog(context.Background())
	if err != nil || len(catalog) != 1 || len(result.Providers) != 1 || len(result.Provides) != 1 {
		t.Fatalf("initialize=%+v catalog=%+v err=%v", result, catalog, err)
	}
	capability := result.Provides[0]
	if capability.Namespace+"/"+capability.ID != catalog[0].Ref || capability.Kind != "provider" || catalog[0].Tools || catalog[0].Reasoning || catalog[0].Vision {
		t.Fatalf("manifest capability=%+v catalog=%+v", capability, catalog)
	}
	encoded, err := json.Marshal(catalog[0])
	if err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf("sha256:%x", sha256.Sum256(encoded)); capability.SchemaHash != want {
		t.Fatalf("descriptor fingerprint=%s, want %s", capability.SchemaHash, want)
	}
	if result.Providers[0].Ref != catalog[0].Ref {
		t.Fatalf("initialize ref=%q catalog ref=%q", result.Providers[0].Ref, catalog[0].Ref)
	}
	if got := (echoProvider{id: "renamed"}).descriptor().Ref; got != "plugin/renamed/offline/echo" {
		t.Fatalf("renamed provider ref=%q", got)
	}
}

func TestEchoProviderStreamsLastUserContent(t *testing.T) {
	for _, tc := range []struct {
		name     string
		messages []extension.ProviderMessage
		want     string
	}{
		{"no user", []extension.ProviderMessage{{Role: extension.ProviderRoleSystem, Content: "system"}}, ""},
		{"unicode", []extension.ProviderMessage{{Role: extension.ProviderRoleUser, Content: "第一行\nsecond line"}}, "第一行\nsecond line"},
		{"last user", []extension.ProviderMessage{
			{Role: extension.ProviderRoleUser, Content: "old"},
			{Role: extension.ProviderRoleAssistant, Content: "answer"},
			{Role: extension.ProviderRoleUser, Content: "<context>host context</context>\nnew"},
			{Role: extension.ProviderRoleTool, Content: "tool result"},
		}, "<context>host context</context>\nnew"},
		{"empty last user", []extension.ProviderMessage{{Role: extension.ProviderRoleUser, Content: "old"}, {Role: extension.ProviderRoleUser}}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			p := echoProvider{id: "echo-provider"}
			stream, err := p.Stream(ctx, extension.StreamRequest{
				ProviderRef: p.descriptor().Ref, Request: extension.ProviderRequest{Messages: tc.messages},
			})
			if err != nil {
				t.Fatal(err)
			}
			var text string
			var types []extension.ProviderChunkType
			for chunk := range stream {
				types = append(types, chunk.Type)
				text += chunk.Text
			}
			if text != "Offline echo: "+tc.want || len(types) != 3 || types[0] != extension.ChunkText || types[1] != extension.ChunkText || types[2] != extension.ChunkDone {
				t.Fatalf("text=%q types=%v", text, types)
			}
		})
	}
}

func TestEchoProviderCancellationAndUnknownRef(t *testing.T) {
	p := echoProvider{id: "echo-provider"}
	if _, err := p.Stream(context.Background(), extension.StreamRequest{ProviderRef: "plugin/other/offline/echo"}); err == nil {
		t.Fatal("accepted another provider's ref")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Stream(ctx, extension.StreamRequest{ProviderRef: p.descriptor().Ref}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Stream=%v", err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	stream, err := p.Stream(ctx, extension.StreamRequest{ProviderRef: p.descriptor().Ref})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	select {
	case chunk := <-stream:
		if chunk.Type != extension.ChunkText || chunk.Text != "Offline echo: " {
			t.Fatalf("first chunk=%+v", chunk)
		}
	case <-time.After(time.Second):
		t.Fatal("producer did not send its first chunk")
	}
	cancel()
	deadline := time.After(time.Second)
	for {
		select {
		case _, ok := <-stream:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("producer did not stop after cancellation")
		}
	}
}
