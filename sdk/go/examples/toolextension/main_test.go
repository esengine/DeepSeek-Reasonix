package main

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestCountWords(t *testing.T) {
	for _, tc := range []struct {
		text string
		want string
	}{
		{"", "0"},
		{" \t\n\u3000", "0"},
		{"hello world again", "3"},
		{"hello\t世界\u3000again\n", "3"},
		{"你好世界", "1"},
	} {
		args, err := json.Marshal(map[string]string{"text": tc.text})
		if err != nil {
			t.Fatal(err)
		}
		got, err := countWords(context.Background(), args)
		if err != nil || got != tc.want {
			t.Fatalf("countWords(%q) = %q, %v; want %q", tc.text, got, err, tc.want)
		}
	}
}

func TestCountWordsRejectsInvalidInput(t *testing.T) {
	for _, args := range []string{`{}`, `{"text":null}`, `{"text":3}`, `not JSON`} {
		if _, err := countWords(context.Background(), json.RawMessage(args)); err == nil {
			t.Fatalf("countWords(%s) accepted invalid text", args)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := countWords(ctx, json.RawMessage(`{"text":"hello"}`)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled countWords = %v", err)
	}
}
