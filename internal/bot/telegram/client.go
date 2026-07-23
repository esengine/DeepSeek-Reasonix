package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultAPIBaseURL = "https://api.telegram.org"

// Probe verifies a Telegram Bot token with getMe without exposing it in errors.
func Probe(ctx context.Context, token string) error {
	return ProbeAPI(ctx, token, "")
}

func ProbeAPI(ctx context.Context, token, baseURL string) error {
	me, err := newClient(token, baseURL, nil).getMe(ctx)
	if err != nil {
		return err
	}
	if !me.IsBot {
		return fmt.Errorf("telegram getMe returned a non-bot account")
	}
	return nil
}

type client struct {
	baseURL string
	token   string
	http    HTTPDoer
}

func newClient(token, baseURL string, httpClient HTTPDoer) *client {
	if baseURL == "" {
		baseURL = defaultAPIBaseURL
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 35 * time.Second}
	}
	return &client{baseURL: strings.TrimRight(baseURL, "/"), token: token, http: httpClient}
}

func (c *client) getMe(ctx context.Context) (user, error) {
	var result user
	return result, c.call(ctx, "getMe", nil, &result)
}

func (c *client) getUpdates(ctx context.Context, offset int64) ([]update, error) {
	var result []update
	body := map[string]any{
		"offset":          offset,
		"timeout":         30,
		"allowed_updates": []string{"message"},
	}
	return result, c.call(ctx, "getUpdates", body, &result)
}

func (c *client) deleteWebhook(ctx context.Context) error {
	return c.call(ctx, "deleteWebhook", map[string]any{"drop_pending_updates": false}, nil)
}

func (c *client) sendMessage(ctx context.Context, chatID string, text string, replyTo int64, threadID int64) (message, error) {
	var result message
	body := map[string]any{"chat_id": chatID, "text": text}
	if replyTo != 0 {
		body["reply_to_message_id"] = replyTo
	}
	if threadID != 0 {
		body["message_thread_id"] = threadID
	}
	return result, c.call(ctx, "sendMessage", body, &result)
}

func (c *client) sendChatAction(ctx context.Context, chatID string) error {
	return c.call(ctx, "sendChatAction", map[string]any{"chat_id": chatID, "action": "typing"}, nil)
}

func redactString(value, token string) string {
	if token == "" {
		return value
	}
	return strings.ReplaceAll(value, token, "[redacted]")
}

func redactToken(err error, token string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s", redactString(err.Error(), token))
}

func (c *client) call(ctx context.Context, method string, body any, result any) error {
	endpoint, err := url.Parse(c.baseURL + "/bot" + c.token + "/" + method)
	if err != nil {
		return fmt.Errorf("telegram %s: invalid API base URL", method)
	}
	var req *http.Request
	if body == nil {
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	} else {
		data, marshalErr := json.Marshal(body)
		if marshalErr != nil {
			return fmt.Errorf("telegram %s: encode request: %w", method, marshalErr)
		}
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), strings.NewReader(string(data)))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
		}
	}
	if err != nil {
		return fmt.Errorf("telegram %s: create request: %w", method, err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("telegram %s: request failed: %w", method, redactToken(err, c.token))
	}
	defer resp.Body.Close()

	var envelope apiResponse[json.RawMessage]
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return fmt.Errorf("telegram %s: decode response: %w", method, err)
	}
	if !envelope.OK {
		code := envelope.ErrorCode
		if code == 0 {
			code = resp.StatusCode
		}
		return &apiError{code: code, description: redactString(envelope.Description, c.token), retryAfter: time.Duration(envelope.Parameters.RetryAfter) * time.Second}
	}
	if result != nil && len(envelope.Result) > 0 {
		if err := json.Unmarshal(envelope.Result, result); err != nil {
			return fmt.Errorf("telegram %s: decode result: %w", method, err)
		}
	}
	return nil
}
