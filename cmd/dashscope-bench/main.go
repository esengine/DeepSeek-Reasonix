// Benchmark: DashScope Chat Completions vs Responses API vs Anthropic Messages API
// Measures TTFT, total latency, token usage, and cache hit rates.
// Usage: source ~/.reasonix/.env && go run ./cmd/dashscope-bench/
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

var (
	apiKey        = os.Getenv("QWEN_TOKEN_PLAN_CN_API_KEY")
	baseURL       = "https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1"
	anthropicURL  = "https://token-plan.cn-beijing.maas.aliyuncs.com/apps/anthropic"
	model         = "qwen3.7-plus"
	client        = &http.Client{Timeout: 120 * time.Second}
)

// envOr returns env var v or fallback.
func envOr(k, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return fallback
}

func main() {
	if v := envOr("BENCH_BASE_URL", ""); v != "" {
		baseURL = v
	}
	if v := envOr("BENCH_MODEL", ""); v != "" {
		model = v
	}
	if v := envOr("BENCH_API_KEY", ""); v != "" {
		apiKey = v
	}
	if apiKey == "" {
		fmt.Println("ERROR: QWEN_TOKEN_PLAN_CN_API_KEY (or BENCH_API_KEY) not set")
		os.Exit(1)
	}
	if strings.Contains(baseURL, "deepseek.com") {
		fmt.Println("NOTE: DeepSeek Responses API is stateless — no previous_response_id")
	}

	systemPrompt := "You are a helpful coding assistant. You help users write, debug, and optimize code. Always provide clear explanations and working code examples. " + strings.Repeat("Context padding for cache testing. ", 100)

	fmt.Println("=== Responses/Completions API Format Benchmark ===")
	fmt.Printf("Base URL: %s\nModel: %s\n", baseURL, model)
	fmt.Printf("System prompt: ~%d chars\n", len(systemPrompt))
	fmt.Println()

	// Chat Completions: 3-turn conversation (sends full history each turn)
	fmt.Println("=== Chat Completions API (full history per turn) ===")
	chatCompletionsBench(systemPrompt, 3)
	fmt.Println()

	// Responses API: 3-turn conversation (uses previous_response_id)
	fmt.Println("=== Responses API ===")
	responsesAPIBench(systemPrompt, 3)
	fmt.Println()
}

func chatCompletionsBench(systemPrompt string, turns int) {
	messages := []map[string]any{
		{"role": "system", "content": systemPrompt},
	}
	questions := []string{
		"Write a Go function to reverse a linked list.",
		"Now add error handling for nil nodes.",
		"Add unit tests for both cases.",
		"Optimize the memory allocation.",
		"Explain the time complexity.",
	}

	for i := 0; i < turns; i++ {
		messages = append(messages, map[string]any{"role": "user", "content": questions[i]})

		body := map[string]any{
			"model":    model,
			"messages": messages,
			"stream":   true,
			"stream_options": map[string]any{"include_usage": true},
			"enable_thinking": true,
			"max_tokens": 200,
		}
		jsonBody, _ := json.Marshal(body)

		start := time.Now()
		ttft, promptTokens, cachedTokens, outputTokens, text, err := streamChatCompletions(jsonBody)
		elapsed := time.Since(start)

		if err != nil {
			fmt.Printf("  Turn %d: ERROR %v\n", i+1, err)
			return
		}

		messages = append(messages, map[string]any{"role": "assistant", "content": text})

		fmt.Printf("  Turn %d: TTFT=%4dms  Total=%5dms  in=%5d cached=%5d out=%4d  Body=%6dB\n",
			i+1, ttft.Milliseconds(), elapsed.Milliseconds(), promptTokens, cachedTokens, outputTokens, len(jsonBody))
	}
}

func responsesAPIBench(systemPrompt string, turns int) {
	questions := []string{
		"Write a Go function to reverse a linked list.",
		"Now add error handling for nil nodes.",
		"Add unit tests for both cases.",
		"Optimize the memory allocation.",
		"Explain the time complexity.",
	}

	var prevResponseID string

	for i := 0; i < turns; i++ {
		body := map[string]any{
			"model":        model,
			"input":        questions[i],
			"instructions": systemPrompt,
			"stream":       true,
			"max_output_tokens": 200,
		}
		if prevResponseID != "" {
			body["previous_response_id"] = prevResponseID
		}
		jsonBody, _ := json.Marshal(body)

		start := time.Now()
		ttft, inputTokens, cachedTokens, outputTokens, responseID, err := streamResponses(jsonBody)
		elapsed := time.Since(start)

		if err != nil {
			fmt.Printf("  Turn %d: ERROR %v\n", i+1, err)
			return
		}
		prevResponseID = responseID

		fmt.Printf("  Turn %d: TTFT=%4dms  Total=%5dms  in=%5d cached=%5d out=%4d  Body=%6dB\n",
			i+1, ttft.Milliseconds(), elapsed.Milliseconds(), inputTokens, cachedTokens, outputTokens, len(jsonBody))
	}
}

func anthropicBench(systemPrompt string, turns int) {
	messages := []map[string]any{}
	questions := []string{
		"Write a Go function to reverse a linked list.",
		"Now add error handling for nil nodes.",
		"Add unit tests for both cases.",
		"Optimize the memory allocation.",
		"Explain the time complexity.",
	}

	for i := 0; i < turns; i++ {
		messages = append(messages, map[string]any{"role": "user", "content": questions[i]})

		// cache breakpoint test: system as content blocks with ephemeral cache_control,
		// plus a breakpoint on the last block of the most recent user message so the
		// conversation prefix can be cached across turns (mirrors anthropic.go:317-331).
		lastMsg := messages[len(messages)-1]
		lastMsg["content"] = []map[string]any{
			{"type": "text", "text": lastMsg["content"], "cache_control": map[string]any{"type": "ephemeral"}},
		}

		body := map[string]any{
			"model":      model,
			"max_tokens": 200,
			"system": []map[string]any{
				{"type": "text", "text": systemPrompt, "cache_control": map[string]any{"type": "ephemeral"}},
			},
			"messages":   messages,
			"stream":     true,
		}
		jsonBody, _ := json.Marshal(body)

		start := time.Now()
		ttft, inputTokens, cachedTokens, creationTokens, outputTokens, text, err := streamAnthropic(jsonBody)
		elapsed := time.Since(start)

		if err != nil {
			fmt.Printf("  Turn %d: ERROR %v\n", i+1, err)
			return
		}

		messages = append(messages, map[string]any{"role": "assistant", "content": text})

		fmt.Printf("  Turn %d: TTFT=%4dms  Total=%5dms  in=%5d cached=%5d create=%5d out=%4d  Body=%6dB\n",
			i+1, ttft.Milliseconds(), elapsed.Milliseconds(), inputTokens, cachedTokens, creationTokens, outputTokens, len(jsonBody))
	}
}

func streamAnthropic(jsonBody []byte) (ttft time.Duration, inputTokens, cachedTokens, creationTokens, outputTokens int, text string, err error) {
	req, err := http.NewRequest("POST", anthropicURL+"/v1/messages", bytes.NewReader(jsonBody))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("anthropic-beta", "prompt-caching-2024-07-31")

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		trunc := string(b)
		if len(trunc) > 300 {
			trunc = trunc[:300]
		}
		err = fmt.Errorf("HTTP %d: %s", resp.StatusCode, trunc)
		return
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)
	firstToken := true
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimPrefix(line, "data:")
		data = strings.TrimSpace(data)

		var event struct {
			Type  string `json:"type"`
			Delta *struct {
				Type     string `json:"type"`
				Text     string `json:"text"`
				Thinking string `json:"thinking"`
			} `json:"delta"`
			Message *struct {
				Usage *struct {
					InputTokens              int `json:"input_tokens"`
					OutputTokens             int `json:"output_tokens"`
					CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
					CacheReadInputTokens     int `json:"cache_read_input_tokens"`
				} `json:"usage"`
			} `json:"message"`
			Usage *struct {
				InputTokens              int `json:"input_tokens"`
				OutputTokens             int `json:"output_tokens"`
				CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
				CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal([]byte(data), &event) != nil {
			continue
		}

		switch event.Type {
		case "content_block_delta":
			if event.Delta != nil {
				if firstToken && event.Delta.Type == "text_delta" {
					ttft = time.Since(start)
					firstToken = false
				}
				text += event.Delta.Text
			}
		case "message_start":
			if event.Message != nil && event.Message.Usage != nil {
				inputTokens = event.Message.Usage.InputTokens
				cachedTokens = event.Message.Usage.CacheReadInputTokens
				creationTokens = event.Message.Usage.CacheCreationInputTokens
			}
		case "message_delta":
			if event.Usage != nil {
				outputTokens = event.Usage.OutputTokens
			}
		case "message_stop":
			goto done
		}
	}
done:
	return
}

func streamChatCompletions(jsonBody []byte) (ttft time.Duration, promptTokens, cachedTokens, outputTokens int, text string, err error) {
	req, err := http.NewRequest("POST", baseURL+"/chat/completions", bytes.NewReader(jsonBody))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("x-dashscope-session-cache", "enable")

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		trunc := string(b)
		if len(trunc) > 300 {
			trunc = trunc[:300]
		}
		err = fmt.Errorf("HTTP %d: %s", resp.StatusCode, trunc)
		return
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	firstToken := true
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		if firstToken {
			ttft = time.Since(start)
			firstToken = false
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
				PromptTokensDetails *struct {
					CachedTokens int `json:"cached_tokens"`
				} `json:"prompt_tokens_details"`
			} `json:"usage"`
		}
		if json.Unmarshal([]byte(data), &chunk) == nil {
			for _, c := range chunk.Choices {
				text += c.Delta.Content
			}
			if chunk.Usage != nil {
				promptTokens = chunk.Usage.PromptTokens
				outputTokens = chunk.Usage.CompletionTokens
				if chunk.Usage.PromptTokensDetails != nil {
					cachedTokens = chunk.Usage.PromptTokensDetails.CachedTokens
				}
			}
		}
	}
	return
}

func streamResponses(jsonBody []byte) (ttft time.Duration, inputTokens, cachedTokens, outputTokens int, responseID string, err error) {
	req, err := http.NewRequest("POST", baseURL+"/responses", bytes.NewReader(jsonBody))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("x-dashscope-session-cache", "enable")

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		trunc := string(b)
		if len(trunc) > 300 {
			trunc = trunc[:300]
		}
		err = fmt.Errorf("HTTP %d: %s", resp.StatusCode, trunc)
		return
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	firstToken := true
	for scanner.Scan() {
		line := scanner.Text()
		// Responses API uses "data:{...}" (no space); be lenient.
		var data string
		if strings.HasPrefix(line, "data: ") {
			data = strings.TrimPrefix(line, "data: ")
		} else if strings.HasPrefix(line, "data:") {
			data = strings.TrimPrefix(line, "data:")
		} else {
			continue
		}
		if data == "[DONE]" {
			break
		}
		if firstToken && (strings.Contains(data, "output_text.delta") || strings.Contains(data, "reasoning_text.delta")) {
			ttft = time.Since(start)
			firstToken = false
		}
		var event struct {
			Type     string `json:"type"`
			Response *struct {
				ID    string `json:"id"`
				Usage *struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
					InputTokensDetails *struct {
						CachedTokens int `json:"cached_tokens"`
					} `json:"input_tokens_details"`
				} `json:"usage"`
			} `json:"response"`
		}
		if json.Unmarshal([]byte(data), &event) == nil {
			if (event.Type == "response.completed" || event.Type == "response.incomplete") && event.Response != nil {
				responseID = event.Response.ID
				if event.Response.Usage != nil {
					inputTokens = event.Response.Usage.InputTokens
					outputTokens = event.Response.Usage.OutputTokens
					if event.Response.Usage.InputTokensDetails != nil {
						cachedTokens = event.Response.Usage.InputTokensDetails.CachedTokens
					}
				}
				break // Responses API sends no [DONE]; stop after completed.
			}
		}
	}
	return
}
