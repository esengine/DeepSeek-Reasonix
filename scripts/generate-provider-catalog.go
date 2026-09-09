//go:build ignore

// Run from the repository root: go run scripts/generate-provider-catalog.go
package main

import (
	"encoding/json"
	"os"
	"reasonix/internal/config"
)

func main() {
	catalogs := map[string]config.ProviderCatalog{}
	templates := []map[string]any{}
	extended := map[string]bool{"doubao-chat": true, "doubao-responses": true, "baidu-cloud": true, "ppio": true, "qiniu": true, "xai-chat": true, "xai-responses": true, "cerebras": true, "together": true, "fireworks-chat": true, "fireworks-anthropic": true, "fireworks-responses": true, "amd-gpu-cloud": true, "deepseek-chat": true, "openai-responses": true, "openai-chat": true, "anthropic": true, "gemini": true, "siliconflow": true, "openrouter": true, "groq": true, "mistral": true, "ollama-local": true, "lmstudio": true}
	for _, p := range config.CuratedProviderPresets() {
		catalogs[p.ID] = config.CatalogForProviderPreset(p)
		if !extended[p.ID] {
			continue
		}
		e := p.Entries[0]
		templates = append(templates, map[string]any{"id": p.ID, "label": p.Label, "description": p.Description, "keyEnv": p.KeyEnv, "provider": map[string]any{"name": e.Name, "kind": e.Kind, "baseUrl": e.BaseURL, "models": e.Models, "default": e.Default, "apiKeyEnv": e.APIKeyEnv, "webSearch": false}})
	}
	data, err := json.MarshalIndent(map[string]any{"catalogs": catalogs, "templates": templates}, "", "  ")
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile("desktop/frontend/src/lib/providerCatalog.generated.json", append(data, '\n'), 0644); err != nil {
		panic(err)
	}
}
