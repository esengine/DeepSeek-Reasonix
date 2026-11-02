package provider

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
)

type uploadNote struct {
	provider string
	bytes    int64
}

func observeUploads(t *testing.T) *[]uploadNote {
	t.Helper()
	var mu sync.Mutex
	var got []uploadNote
	SetUploadObserver(func(provider string, n int64) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, uploadNote{provider, n})
	})
	t.Cleanup(func() { SetUploadObserver(nil) })
	return &got
}

func TestSendWithRetryReportsEachAttemptsBodyToTheProvider(t *testing.T) {
	notes := observeUploads(t)
	body := strings.Repeat("x", 4096)
	calls := 0
	client := &http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return statusResp(http.StatusServiceUnavailable, nil), nil
		}
		return statusResp(http.StatusOK, nil), nil
	})}
	newReq := func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodPost, "http://x/y", bytes.NewReader([]byte(body)))
	}
	resp, err := SendWithRetry(context.Background(), client, SendOptions{Provider: "deepseek"}, newReq)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	want := []uploadNote{{"deepseek", 4096}, {"deepseek", 4096}}
	if len(*notes) != len(want) || (*notes)[0] != want[0] || (*notes)[1] != want[1] {
		t.Fatalf("uploads = %+v, want %+v: a retried attempt sends the body again, so it counts again", *notes, want)
	}
}

func TestARequestWithoutAKnownBodyReportsNothing(t *testing.T) {
	notes := observeUploads(t)
	client := &http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) { return statusResp(http.StatusOK, nil), nil })}
	resp, err := SendWithRetry(context.Background(), client, SendOptions{Provider: "p"}, newDummyReq)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(*notes) != 0 {
		t.Fatalf("uploads = %+v, want none for a request with no body", *notes)
	}
}
