// Command tool-surface-cache measures what moving a tool inside the schema
// costs against a provider's prefix cache. It answers one question with real
// requests: may the provider-visible tool set change between turns?
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

type usage struct {
	PromptTokens int `json:"prompt_tokens"`
	Hit          int `json:"prompt_cache_hit_tokens"`
	Miss         int `json:"prompt_cache_miss_tokens"`
}

func probe(n int, extra string) map[string]any {
	return map[string]any{"type": "function", "function": map[string]any{
		"name": fmt.Sprintf("probe_%d", n),
		"description": fmt.Sprintf("Probe tool number %d used to occupy schema tokens in a cache "+
			"experiment. It accepts a path and a mode and returns a string. %s", n, extra),
		"parameters": map[string]any{"type": "object", "properties": map[string]any{
			"path": map[string]any{"type": "string", "description": fmt.Sprintf("filesystem path for probe %d", n)},
			"mode": map[string]any{"type": "string", "description": fmt.Sprintf("operating mode for probe %d", n)},
		}, "required": []string{"path"}},
	}}
}

func main() {
	endpoint := flag.String("endpoint", "https://api.deepseek.com/chat/completions", "chat completions endpoint")
	model := flag.String("model", "deepseek-chat", "model to sample")
	keyEnv := flag.String("key-env", "DEEPSEEK_API_KEY", "environment variable holding the key")
	flag.Parse()
	key := os.Getenv(*keyEnv)
	if key == "" {
		fmt.Fprintf(os.Stderr, "%s is empty; export it before running\n", *keyEnv)
		os.Exit(2)
	}

	system := strings.Repeat("You are a coding agent. Principles: understand the request before acting; "+
		"verify with tools instead of guessing; keep edits minimal and reversible; report what was "+
		"actually observed rather than what was expected. ", 24)
	base := make([]map[string]any, 0, 10)
	for i := range 10 {
		base = append(base, probe(i, ""))
	}

	call := func(label string, tools []map[string]any) {
		payload, _ := json.Marshal(map[string]any{
			"model": *model,
			"messages": []map[string]string{
				{"role": "system", "content": system},
				{"role": "user", "content": "say ok"},
			},
			"tools": tools, "max_tokens": 1, "temperature": 0,
		})
		req, _ := http.NewRequest(http.MethodPost, *endpoint, bytes.NewReader(payload))
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", label, err)
			return
		}
		defer resp.Body.Close()
		var out struct {
			Usage usage `json:"usage"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", label, err)
			return
		}
		u := out.Usage
		rate := 0.0
		if u.PromptTokens > 0 {
			rate = 100 * float64(u.Hit) / float64(u.PromptTokens)
		}
		fmt.Printf("%-34s prompt=%5d  hit=%5d  miss=%4d  %5.1f%%\n", label, u.PromptTokens, u.Hit, u.Miss, rate)
		time.Sleep(2 * time.Second)
	}

	with := func(extra ...map[string]any) []map[string]any {
		out := append([]map[string]any(nil), base...)
		return append(out, extra...)
	}

	fmt.Println("== where a tool sits decides what a change costs ==")
	call("1. baseline (warm the cache)", base)
	call("2. baseline again", base)
	call("3. one tool appended at the tail", with(probe(99, "")))
	call("4. one tool inserted at the head", append([]map[string]any{probe(98, "")}, base...))
	mid := append(append(append([]map[string]any(nil), base[:5]...), probe(5, "CHANGED DESCRIPTION HERE.")), base[6:]...)
	call("5. the fifth tool's text edited", mid)
	call("6. baseline once more", base)

	fmt.Println("\n== a tail tool coming and going, which is what a contextual tool does ==")
	tail := with(probe(99, ""))
	for i := range 3 {
		call(fmt.Sprintf("%d. absent", 2*i+1), base)
		call(fmt.Sprintf("%d. present", 2*i+2), tail)
	}
}
