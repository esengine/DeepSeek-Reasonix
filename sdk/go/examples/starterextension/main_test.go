package main

import (
	"context"
	"encoding/json"
	"testing"
)

func TestInterceptInputPreservesAndAnnotatesText(t *testing.T) {
	for _, text := range []string{
		"explain sidecars",
		"<workspace>\n/work\n</workspace>\n\nordinary input",
		"第一行\n第二行：starter: stays literal",
	} {
		t.Run(text, func(t *testing.T) {
			payload, err := json.Marshal(map[string]string{"text": text})
			if err != nil {
				t.Fatal(err)
			}
			result, err := interceptInput(context.Background(), "input.receive", payload)
			if err != nil {
				t.Fatal(err)
			}
			if result == nil || result.Decision != "replace" {
				t.Fatalf("result = %#v, want replace", result)
			}
			var replacement struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal(result.Replacement, &replacement); err != nil {
				t.Fatal(err)
			}
			if want := text + " [rewritten by starter-extension]"; replacement.Text != want {
				t.Fatalf("replacement = %q, want %q", replacement.Text, want)
			}
		})
	}
}

func TestInterceptInputContinuesInvalidOrEmptyText(t *testing.T) {
	for _, payload := range []string{"{", "{}", `{"text":""}`} {
		result, err := interceptInput(context.Background(), "input.receive", json.RawMessage(payload))
		if err != nil {
			t.Fatal(err)
		}
		if result == nil || result.Decision != "continue" {
			t.Fatalf("result = %#v, want continue for %q", result, payload)
		}
	}
}
