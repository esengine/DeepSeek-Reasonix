package main

import (
	"regexp"
	"strings"

	"reasonix/internal/control"
)

func rewriteDraftExternalFileRef(input, path, token string) string {
	old := "@" + control.EscapeRefPath(path)
	pattern := regexp.MustCompile(regexp.QuoteMeta(old) + `($|\s)`)
	return pattern.ReplaceAllStringFunc(input, func(match string) string {
		return "@" + control.EscapeRefPath(token) + strings.TrimPrefix(match, old)
	})
}
