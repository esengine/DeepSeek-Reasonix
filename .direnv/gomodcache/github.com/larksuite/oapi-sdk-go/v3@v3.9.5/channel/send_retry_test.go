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

func TestSend_Retry(t *testing.T) {
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

			attempts++
			if attempts < 3 {
				t.Logf("Mocking attempt %d: returning 200 with unknown error", attempts)
				// Return a business error that will be classified as unknown
				respBody := `{"code":999999,"msg":"some random error"}`
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(strings.NewReader(respBody)),
					Header:     header,
				}, nil
			}

			t.Logf("Mocking attempt %d: returning 200", attempts)
			// Mock send message response on 3rd attempt
			respBody := `{"code":0,"msg":"success","data":{"message_id":"om_retry_123"}}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(respBody)),
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

	res, err := ch.Send(context.Background(), &types.SendInput{
		ReceiveID: "ou_123", // Using ReceiveID this time to test routing as well
		Text:      "hello",
	})

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if res.MessageID != "om_retry_123" {
		t.Errorf("expected message_id om_retry_123, got %s", res.MessageID)
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}
