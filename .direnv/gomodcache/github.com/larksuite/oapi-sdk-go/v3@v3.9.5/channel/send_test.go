package channel

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/larksuite/oapi-sdk-go/v3/channel/types"
)

type MockHttpClient struct {
	DoFunc func(req *http.Request) (*http.Response, error)
}

func (m *MockHttpClient) Do(req *http.Request) (*http.Response, error) {
	if m.DoFunc != nil {
		return m.DoFunc(req)
	}
	return nil, nil
}

func TestSend_Text(t *testing.T) {
	mockHttp := &MockHttpClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			// Mock send message response
			respBody := `{"code":0,"msg":"success","data":{"message_id":"om_123456"}}`
			header := make(http.Header)
			header.Set("Content-Type", "application/json")
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(respBody)),
				Header:     header,
			}, nil
		},
	}

	client := lark.NewClient("appId", "appSecret", lark.WithHttpClient(mockHttp))
	ch := NewChannel(client, nil)

	res, err := ch.Send(context.Background(), &types.SendInput{
		UserID: "u_123",
		Text:   "hello",
	})

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if res.MessageID != "om_123456" {
		t.Errorf("expected message_id om_123456, got %s", res.MessageID)
	}
}

func TestSend_NoUserIDOrChatID(t *testing.T) {
	client := lark.NewClient("appId", "appSecret")
	ch := NewChannel(client, nil)

	_, err := ch.Send(context.Background(), &types.SendInput{
		Text: "hello",
	})

	if err == nil || !strings.Contains(err.Error(), "ReceiveID, ChatID, or UserID must be provided") {
		t.Errorf("expected error 'ReceiveID, ChatID, or UserID must be provided', got %v", err)
	}
}
