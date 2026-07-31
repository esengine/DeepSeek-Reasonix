package types

import (
	"errors"
	"net/url"
	"testing"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
)

func TestClassifyError(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantCode  FeishuChannelErrorCode
		wantRetry bool
	}{
		{
			name:      "nil error",
			err:       nil,
			wantCode:  "",
			wantRetry: true, // Should return nil, we'll handle this specially in test
		},
		{
			name: "target revoked",
			err: &larkcore.CodeError{
				Code: 230020,
				Msg:  "target revoked",
			},
			wantCode:  ErrCodeTargetRevoked,
			wantRetry: false,
		},
		{
			name: "permission denied",
			err: &larkcore.CodeError{
				Code: 99991400,
				Msg:  "permission denied",
			},
			wantCode:  ErrCodePermissionDenied,
			wantRetry: false,
		},
		{
			name: "permission denied for target chat visibility",
			err: &larkcore.CodeError{
				Code: 230002,
				Msg:  "Bot/User can NOT be out of the chat.",
			},
			wantCode:  ErrCodePermissionDenied,
			wantRetry: false,
		},
		{
			name: "format error",
			err: &larkcore.CodeError{
				Code: 230001,
				Msg:  "format error",
			},
			wantCode:  ErrCodeFormatError,
			wantRetry: false,
		},
		{
			name:      "rate limited",
			err:       errors.New("API returned status 429"),
			wantCode:  ErrCodeRateLimited,
			wantRetry: true,
		},
		{
			name:      "timeout",
			err:       &url.Error{Err: errors.New("timeout"), URL: "https://example.com"},
			wantCode:  ErrCodeSendTimeout,
			wantRetry: false,
		},
		{
			name:      "unknown error",
			err:       errors.New("some random error"),
			wantCode:  ErrCodeUnknown,
			wantRetry: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyError(tt.err)
			if tt.err == nil {
				if got != nil {
					t.Errorf("ClassifyError() got = %v, want nil", got)
				}
				return
			}
			if got.Code != tt.wantCode {
				t.Errorf("ClassifyError() got Code = %v, want %v", got.Code, tt.wantCode)
			}
			if IsRetryable(got) != tt.wantRetry {
				t.Errorf("IsRetryable() got = %v, want %v", IsRetryable(got), tt.wantRetry)
			}
		})
	}
}

func TestIsFunctions(t *testing.T) {
	formatErr := &FeishuChannelError{Code: ErrCodeFormatError}
	if !IsFormatError(formatErr) {
		t.Errorf("IsFormatError() expected true")
	}

	revokedErr := &FeishuChannelError{Code: ErrCodeTargetRevoked}
	if !IsReplyTargetGone(revokedErr) {
		t.Errorf("IsReplyTargetGone() expected true")
	}
}
