package outbound

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	lark "github.com/larksuite/oapi-sdk-go/v3"
)

type MockUploader struct {
	UploadImagePathFunc func(ctx context.Context, imageType string, path string) (string, error)
	UploadFilePathFunc  func(ctx context.Context, fileType string, path string) (string, error)
}

func (m *MockUploader) UploadImagePath(ctx context.Context, imageType string, path string) (string, error) {
	if m.UploadImagePathFunc != nil {
		return m.UploadImagePathFunc(ctx, imageType, path)
	}
	return "", nil
}

func (m *MockUploader) UploadFilePath(ctx context.Context, fileType string, path string) (string, error) {
	if m.UploadFilePathFunc != nil {
		return m.UploadFilePathFunc(ctx, fileType, path)
	}
	return "", nil
}

func TestUploader(t *testing.T) {
	mockHttp := &MockHttpClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			respBody := `{"code":0,"msg":"success","data":{"image_key":"img_123"}}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(respBody)),
				Header:     make(http.Header),
			}, nil
		},
	}

	client := lark.NewClient("appId", "appSecret", lark.WithHttpClient(mockHttp))
	uploader := NewUploader(client)

	// Just test instantiation since actual upload involves reading files
	if uploader == nil {
		t.Errorf("expected uploader to be non-nil")
	}
}

// MockHttpClient is a mock HTTP client for testing.
type MockHttpClient struct {
	DoFunc func(req *http.Request) (*http.Response, error)
}

func (m *MockHttpClient) Do(req *http.Request) (*http.Response, error) {
	if m.DoFunc != nil {
		return m.DoFunc(req)
	}
	return nil, nil
}
