package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"

	extension "github.com/esengine/DeepSeek-Reasonix/sdk/go"
)

type counter struct{}

func (counter) Initialize(context.Context, extension.InitializeParams) (*extension.InitializeResult, error) {
	return &extension.InitializeResult{}, nil
}

func countWords(ctx context.Context, args json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var input struct {
		Text *string `json:"text"`
	}
	if err := json.Unmarshal(args, &input); err != nil || input.Text == nil {
		return "", errors.New("text must be provided as a string")
	}
	return strconv.Itoa(len(strings.Fields(*input.Text))), nil
}

func main() {
	err := extension.Serve(context.Background(), counter{}, extension.Options{
		Name:    "word-counter",
		Version: "0.1.0",
		Tools:   map[string]extension.ToolFunc{"count_words": countWords},
	})
	if err != nil {
		os.Exit(1)
	}
}
