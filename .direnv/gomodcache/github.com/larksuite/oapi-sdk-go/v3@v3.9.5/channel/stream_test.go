package channel

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/larksuite/oapi-sdk-go/v3/channel/types"
)

func TestStream(t *testing.T) {
	var reqCount int
	mockHttp := &MockHttpClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			reqCount++
			header := make(http.Header)
			header.Set("Content-Type", "application/json")

			if strings.Contains(req.URL.Path, "messages") && req.Method == http.MethodPost {
				// Initial send message response
				respBody := `{"code":0,"msg":"success","data":{"message_id":"om_stream123"}}`
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(respBody)),
					Header:     header,
				}, nil
			} else if strings.Contains(req.URL.Path, "messages/om_stream123") || req.Method == http.MethodPatch || req.Method == http.MethodPut {
				// Update message response
				respBody := `{"code":0,"msg":"success","data":{}}`
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(respBody)),
					Header:     header,
				}, nil
			}

			// Default / Auth response
			respBody := `{"code":0,"msg":"success","tenant_access_token":"t-token"}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(respBody)),
				Header:     header,
			}, nil
		},
	}

	client := lark.NewClient("appId", "appSecret", lark.WithHttpClient(mockHttp))
	ch := NewChannel(client, nil)

	// Create stream
	stream, err := ch.Stream(context.Background(), &types.SendInput{
		ChatID: "oc_123",
		Title:  "Stream Test",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if stream == nil {
		t.Fatalf("expected stream to be non-nil")
	}

	// Append text
	err = stream.Append(context.Background(), "Hello ")
	if err != nil {
		t.Fatalf("Append error: %v", err)
	}

	// Flush
	err = stream.Flush(context.Background())
	if err != nil {
		t.Fatalf("Flush error: %v", err)
	}

	// Close
	err = stream.Close(context.Background())
	if err != nil {
		t.Fatalf("Close error: %v", err)
	}

	if reqCount < 2 {
		t.Errorf("expected at least 2 requests, got %d", reqCount)
	}
}

func TestThrottleController(t *testing.T) {
	var execCount int
	execFunc := func(ctx context.Context) error {
		execCount++
		return nil
	}

	tc := newThrottleController(50*time.Millisecond, execFunc)

	// Trigger immediately
	tc.Trigger(context.Background())
	if execCount != 1 {
		t.Errorf("expected 1 exec, got %d", execCount)
	}

	// Trigger again before interval, should be throttled
	tc.Trigger(context.Background())
	if execCount != 1 {
		t.Errorf("expected 1 exec, got %d", execCount)
	}

	// Flush should execute immediately
	tc.Flush(context.Background())
	if execCount != 2 {
		t.Errorf("expected 2 exec, got %d", execCount)
	}

	tc.Close(context.Background())
}
