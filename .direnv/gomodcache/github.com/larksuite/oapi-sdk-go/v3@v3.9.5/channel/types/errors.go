package types

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
)

type FeishuChannelErrorCode string

const (
	ErrCodeTargetRevoked    FeishuChannelErrorCode = "target_revoked"
	ErrCodePermissionDenied FeishuChannelErrorCode = "permission_denied"
	ErrCodeFormatError      FeishuChannelErrorCode = "format_error"
	ErrCodeRateLimited      FeishuChannelErrorCode = "rate_limited"
	ErrCodeSSRFBlocked      FeishuChannelErrorCode = "ssrf_blocked"
	ErrCodeSendTimeout      FeishuChannelErrorCode = "send_timeout"
	ErrCodeUnknown          FeishuChannelErrorCode = "unknown"
)

type FeishuChannelError struct {
	Code    FeishuChannelErrorCode
	Message string
	Cause   error
	Context map[string]interface{}
}

func (e *FeishuChannelError) Error() string {
	msg := fmt.Sprintf("FeishuChannelError(code=%s): %s", e.Code, e.Message)
	if e.Cause != nil {
		msg += fmt.Sprintf(" | cause: %v", e.Cause)
	}
	return msg
}

func (e *FeishuChannelError) Unwrap() error {
	return e.Cause
}

// ClassifyError classifies a raw error into a FeishuChannelError with a stable code.
func ClassifyError(err error, ctx ...map[string]interface{}) *FeishuChannelError {
	if err == nil {
		return nil
	}
	var fce *FeishuChannelError
	if errors.As(err, &fce) {
		return fce
	}

	code := inferCode(err)
	message := extractMessage(err)

	var errorCtx map[string]interface{}
	if len(ctx) > 0 {
		errorCtx = ctx[0]
	}

	return &FeishuChannelError{
		Code:    code,
		Message: message,
		Cause:   err,
		Context: errorCtx,
	}
}

func inferCode(err error) FeishuChannelErrorCode {
	// 1. Try to unwrap larkcore.CodeError
	var apiErr *larkcore.CodeError
	if errors.As(err, &apiErr) {
		feishuCode := apiErr.Code
		// 230011: The message was withdrawn (target revoked)
		// 230040: The target message is not in the specified chat (often treated as target revoked in reply fallback)
		if feishuCode == 230020 || feishuCode == 230017 || feishuCode == 230011 || feishuCode == 230040 {
			return ErrCodeTargetRevoked
		}
		if feishuCode == 99991400 || feishuCode == 99991401 || feishuCode == 230002 {
			return ErrCodePermissionDenied
		}
		if feishuCode == 230001 {
			return ErrCodeFormatError
		}

		// Also check HTTP status or some custom status field if available
		// However, API errors from Lark usually have a Code. Let's fallback.
	}

	msg := strings.ToLower(err.Error())

	// Try to parse some HTTP codes if present in error message
	if strings.Contains(msg, "status 429") {
		return ErrCodeRateLimited
	}
	if strings.Contains(msg, "status 401") || strings.Contains(msg, "status 403") {
		return ErrCodePermissionDenied
	}
	if strings.Contains(msg, "status 400") {
		return ErrCodeFormatError
	}
	if strings.Contains(msg, "status 404") {
		return ErrCodeTargetRevoked
	}

	if strings.HasPrefix(msg, "ssrf_blocked") || strings.Contains(msg, "ssrf_blocked") {
		return ErrCodeSSRFBlocked
	}

	// Timeout check
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Timeout() {
		return ErrCodeSendTimeout
	}
	if strings.Contains(msg, "timeout") || strings.Contains(msg, "etimedout") || strings.Contains(msg, "econnaborted") || strings.Contains(msg, "context deadline exceeded") {
		return ErrCodeSendTimeout
	}

	return ErrCodeUnknown
}

func extractMessage(err error) string {
	var apiErr *larkcore.CodeError
	if errors.As(err, &apiErr) {
		return apiErr.Msg
	}
	return err.Error()
}

func IsRetryable(err error) bool {
	var fce *FeishuChannelError
	if errors.As(err, &fce) {
		return fce.Code == ErrCodeRateLimited || fce.Code == ErrCodeUnknown
	}
	// By default, unknown errors are retryable
	return true
}

func IsFormatError(err error) bool {
	var fce *FeishuChannelError
	if errors.As(err, &fce) {
		return fce.Code == ErrCodeFormatError
	}
	return false
}

func IsReplyTargetGone(err error) bool {
	var fce *FeishuChannelError
	if errors.As(err, &fce) {
		return fce.Code == ErrCodeTargetRevoked
	}
	return false
}
