package computer

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"reasonix/internal/base/textutil"
)

type driverStatus struct {
	Effect string `json:"effect"`
	Code   string `json:"code"`
	Hint   string `json:"hint"`
	Reason string `json:"reason"`
	Error  *struct {
		Code   string `json:"code"`
		Hint   string `json:"hint"`
		Reason string `json:"reason"`
	} `json:"error"`
}

func checkDriverResult(result DriverResult) error {
	var status driverStatus
	if len(result.Structured) > 0 {
		if err := json.Unmarshal(result.Structured, &status); err != nil {
			return fail(CodeFailed, "invalid CUA structured result")
		}
	}
	if !result.IsError && status.Effect != "refused" {
		return nil
	}
	code, reason := status.Code, status.Hint
	if status.Error != nil {
		if status.Error.Code != "" {
			code = status.Error.Code
		}
		if status.Error.Hint != "" {
			reason = status.Error.Hint
		} else if status.Error.Reason != "" {
			reason = status.Error.Reason
		}
	}
	if reason == "" {
		reason = status.Reason
	}
	if reason == "" {
		for _, content := range result.Content {
			if content.Type == "text" && content.Text != "" {
				reason = content.Text
				break
			}
		}
	}
	detail := "CUA refused the operation"
	if code != "" {
		detail += " (" + ShownBundle(code) + ")"
	}
	if reason != "" {
		if len(reason) > 2048 {
			cut := 2048
			for cut > 0 && !utf8.RuneStart(reason[cut]) {
				cut--
			}
			reason = reason[:cut] + "…"
		}
		bounded, _ := textutil.BoundLiteral(reason, textutil.PreviewLimit{Graphemes: 2048, Lines: 1})
		detail += ": " + fmt.Sprintf("%q", strings.TrimSpace(bounded))
	}
	return &Failure{Code: CodeDriverRefused, Detail: detail}
}
