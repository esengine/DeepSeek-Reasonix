package main

import (
	"fmt"
	"strings"
)

const maxFileLines = 800

// Translation tables are data, not code: they grow one entry per UI string, so
// a ceiling there fires on every new label without ever pointing at something
// worth splitting.
var sizeExemptPrefixes = []string{
	"desktop/frontend/src/locales/",
}

func sizeExempt(rel string) bool {
	for _, prefix := range sizeExemptPrefixes {
		if strings.HasPrefix(rel, prefix) {
			return true
		}
	}
	return false
}

func checkSize(s *sourceFile) []Finding {
	if s.lines <= maxFileLines || sizeExempt(s.rel) {
		return nil
	}
	rule := ruleFileSize
	if s.isTest() {
		rule = ruleTestSize
	}
	return []Finding{{s.rel, 1, rule,
		fmt.Sprintf("%d lines exceeds the %d-line ceiling", s.lines, maxFileLines), s.lines - maxFileLines}}
}
