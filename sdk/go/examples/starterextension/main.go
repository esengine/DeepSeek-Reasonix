// Command starterextension is the smallest installable Reasonix code
// extension. It preserves and annotates the composed input so developers can
// verify the manifest -> sidecar -> intercept path before adding capabilities.
package main

import (
	"context"
	"encoding/json"
	"os"

	extension "github.com/esengine/DeepSeek-Reasonix/sdk/go"
)

type starter struct{}

func (starter) Initialize(context.Context, extension.InitializeParams) (*extension.InitializeResult, error) {
	return &extension.InitializeResult{
		Subscriptions: []string{"input.receive"},
	}, nil
}

func interceptInput(_ context.Context, _ string, payload json.RawMessage) (*extension.InterceptResult, error) {
	var input struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(payload, &input); err != nil || input.Text == "" {
		return extension.Continue(), nil
	}
	return extension.Replace(map[string]string{
		"text": input.Text + " [rewritten by starter-extension]",
	})
}

func main() {
	err := extension.Serve(context.Background(), starter{}, extension.Options{
		Name:    "starter-extension",
		Version: "0.1.0",
		Interceptors: map[string]extension.InterceptorFunc{
			"input.receive": interceptInput,
		},
	})
	if err != nil {
		os.Exit(1)
	}
}
