package acp

import (
	"strings"
	"unicode/utf8"

	"reasonix/internal/secrets"
)

func clipStatusText(value string, limit int) string {
	value = strings.TrimSpace(secrets.Redact(value))
	return clipStatusValue(value, limit)
}

func clipStatusCredentialText(value string, limit int) string {
	value = strings.TrimSpace(secrets.RedactCredentials(value))
	return clipStatusValue(value, limit)
}

func clipStatusValue(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	value = strings.ToValidUTF8(value, "\uFFFD")
	if len(value) <= limit {
		return value
	}
	end := limit
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}
	return value[:end]
}

func clipStatusError(err error, limit int) string {
	if err == nil {
		return ""
	}
	return clipStatusCredentialText(err.Error(), limit)
}
