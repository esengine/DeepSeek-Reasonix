package outbound

import (
	"context"
	"errors"
	"github.com/larksuite/oapi-sdk-go/v3/channel/types"
	"testing"
	"time"
)

func TestRetry(t *testing.T) {
	t.Run("success on first try", func(t *testing.T) {
		op := func(attempt int) (interface{}, error) {
			return "ok", nil
		}
		res, err := Retry(context.Background(), op, nil)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if res != "ok" {
			t.Fatalf("expected 'ok', got %v", res)
		}
	})

	t.Run("fail fast on non-retryable error", func(t *testing.T) {
		op := func(attempt int) (interface{}, error) {
			return nil, &types.FeishuChannelError{Code: types.ErrCodeFormatError}
		}
		_, err := Retry(context.Background(), op, nil)
		if !types.IsFormatError(err) {
			t.Fatalf("expected format error, got %v", err)
		}
	})

	t.Run("retry and success", func(t *testing.T) {
		attempts := 0
		op := func(attempt int) (interface{}, error) {
			attempts++
			if attempts < 3 {
				return nil, errors.New("rate_limited") // This will be classified as unknown/retryable
			}
			return "ok", nil
		}

		opts := &RetryOptions{MaxAttempts: 3, BaseDelay: 10 * time.Millisecond}
		res, err := Retry(context.Background(), op, opts)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if res != "ok" {
			t.Fatalf("expected 'ok', got %v", res)
		}
		if attempts != 3 {
			t.Fatalf("expected 3 attempts, got %v", attempts)
		}
	})

	t.Run("exceed max attempts", func(t *testing.T) {
		attempts := 0
		op := func(attempt int) (interface{}, error) {
			attempts++
			return nil, errors.New("some unknown error") // Unknown error is retryable
		}

		opts := &RetryOptions{MaxAttempts: 2, BaseDelay: 10 * time.Millisecond}
		_, err := Retry(context.Background(), op, opts)
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if attempts != 2 {
			t.Fatalf("expected 2 attempts, got %v", attempts)
		}
	})
}
