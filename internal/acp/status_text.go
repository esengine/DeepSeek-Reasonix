package acp

import (
	"strings"

	"reasonix/internal/secrets"
)

func clipStatusText(value string, limit int) string {
	value = strings.TrimSpace(secrets.Redact(value))
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func clipStatusError(err error, limit int) string {
	value := strings.TrimSpace(secrets.RedactError(err))
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
