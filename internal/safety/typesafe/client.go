// Package typesafe implements TypeSafe AI's System One decision protocol.
package typesafe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const DefaultBaseURL = "https://api.typesafe.ai"

type Question struct {
	Type         string `json:"type"`
	Instructions any    `json:"instructions"`
	Criteria     any    `json:"criteria,omitempty"`
}

type Request struct {
	State     any                 `json:"state"`
	Model     string              `json:"model"`
	Questions map[string]Question `json:"questions"`
}

type Response struct {
	Model   string                     `json:"model"`
	Answers map[string]json.RawMessage `json:"answers"`
	Usage   Usage                      `json:"usage"`
	Routing json.RawMessage            `json:"routing,omitempty"`
}

type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type HTTPError struct {
	Status int
	Body   string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("TypeSafe System One request failed (HTTP %d)", e.Status)
}

type Client struct {
	HTTP    *http.Client
	BaseURL string
	APIKey  func() string
}

func (c Client) Evaluate(ctx context.Context, request Request) (Response, error) {
	if err := validate(request); err != nil {
		return Response{}, err
	}
	key := ""
	if c.APIKey != nil {
		key = strings.TrimSpace(c.APIKey())
	}
	if key == "" {
		return Response{}, errors.New("TypeSafe API key is not configured")
	}
	body, err := json.Marshal(request)
	if err != nil {
		return Response{}, fmt.Errorf("encode TypeSafe request: %w", err)
	}
	base := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if base == "" {
		base = DefaultBaseURL
	}
	endpoint, err := url.Parse(base + "/v1/systemone")
	if err != nil || endpoint.Scheme == "" || endpoint.Host == "" {
		return Response{}, fmt.Errorf("invalid TypeSafe base URL %q", base)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("create TypeSafe request: %w", err)
	}
	httpRequest.Header.Set("Authorization", "Bearer "+key)
	httpRequest.Header.Set("Content-Type", "application/json")
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	httpResponse, err := client.Do(httpRequest)
	if err != nil {
		return Response{}, fmt.Errorf("call TypeSafe System One: %w", err)
	}
	defer httpResponse.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(httpResponse.Body, 4<<20))
	if err != nil {
		return Response{}, fmt.Errorf("read TypeSafe response: %w", err)
	}
	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		return Response{}, &HTTPError{Status: httpResponse.StatusCode, Body: strings.TrimSpace(string(responseBody))}
	}
	var response Response
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return Response{}, fmt.Errorf("decode TypeSafe response: %w", err)
	}
	return response, nil
}

func validate(request Request) error {
	if request.State == nil {
		return errors.New("TypeSafe state is required")
	}
	if strings.TrimSpace(request.Model) == "" {
		return errors.New("TypeSafe model is required")
	}
	if len(request.Questions) == 0 {
		return errors.New("TypeSafe questions are required")
	}
	for id, question := range request.Questions {
		if strings.TrimSpace(id) == "" {
			return errors.New("TypeSafe question id is required")
		}
		if question.Instructions == nil {
			return fmt.Errorf("TypeSafe question %q instructions are required", id)
		}
		switch strings.ToLower(strings.TrimSpace(question.Type)) {
		case "noul":
		case "choice":
			criteria, ok := question.Criteria.(map[string]any)
			if !ok || len(criteria) < 2 || len(criteria) > 255 {
				return fmt.Errorf("TypeSafe choice question %q requires 2 to 255 criteria options", id)
			}
		case "score":
			criteria, ok := question.Criteria.([]any)
			if !ok || len(criteria) < 2 || len(criteria) > 10 {
				return fmt.Errorf("TypeSafe score question %q requires 2 to 10 ordered criteria levels", id)
			}
		default:
			return fmt.Errorf("TypeSafe question %q has unsupported type %q", id, question.Type)
		}
	}
	return nil
}
