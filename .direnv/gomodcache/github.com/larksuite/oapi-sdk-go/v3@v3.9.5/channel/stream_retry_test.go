package channel

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/larksuite/oapi-sdk-go/v3/channel/outbound"
	"github.com/larksuite/oapi-sdk-go/v3/channel/types"
)

func TestStream_Retry(t *testing.T) {
	attempts := 0
	mockHttp := &MockHttpClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			header := make(http.Header)
			header.Set("Content-Type", "application/json")

			if strings.Contains(req.URL.Path, "tenant_access_token") {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"code":0,"msg":"ok","tenant_access_token":"t-123","expire":7200}`)),
					Header:     header,
				}, nil
			}

			// For initial message send
			if req.Method == http.MethodPost {
				respBody := `{"code":0,"msg":"success","data":{"message_id":"om_stream123"}}`
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(respBody)),
					Header:     header,
				}, nil
			}

			// For message update
			if req.Method == http.MethodPatch || req.Method == http.MethodPut {
				attempts++
				if attempts < 3 {
					t.Logf("Mocking stream update attempt %d: returning 200 with unknown error", attempts)
					// Return a business error that will be classified as unknown
					respBody := `{"code":999999,"msg":"some random error"}`
					return &http.Response{
						StatusCode: 200,
						Body:       io.NopCloser(strings.NewReader(respBody)),
						Header:     header,
					}, nil
				}

				t.Logf("Mocking stream update attempt %d: returning 200", attempts)
				respBody := `{"code":0,"msg":"success","data":{}}`
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(respBody)),
					Header:     header,
				}, nil
			}

			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader("{}")),
				Header:     header,
			}, nil
		},
	}

	client := lark.NewClient("appId", "appSecret", lark.WithHttpClient(mockHttp))
	ch := NewChannel(client, nil)

	// Temporarily override DefaultRetryOptions to make test faster
	originalOpts := outbound.DefaultRetryOptions
	outbound.DefaultRetryOptions = outbound.RetryOptions{MaxAttempts: 3, BaseDelay: 10 * time.Millisecond}
	defer func() { outbound.DefaultRetryOptions = originalOpts }()

	// Create stream
	stream, err := ch.Stream(context.Background(), &types.SendInput{
		ReceiveID: "ou_123", // use ReceiveID to test routing again
		Title:     "Stream Retry Test",
	})
	if err != nil {
		t.Fatalf("expected no error on initial stream start, got %v", err)
	}

	// Trigger flush which will update the message
	err = stream.Flush(context.Background())
	if err != nil {
		t.Fatalf("Flush error: %v", err)
	}

	if attempts != 3 {
		t.Errorf("expected 3 stream update attempts, got %d", attempts)
	}
}
