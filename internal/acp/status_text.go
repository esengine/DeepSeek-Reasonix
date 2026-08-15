package acp

import (
	"strings"

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
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func clipStatusError(err error, limit int) string {
	if err == nil {
		return ""
	}
	return clipStatusCredentialText(err.Error(), limit)
}
