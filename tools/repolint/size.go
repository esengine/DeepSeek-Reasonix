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
	"desktop/frontend-next/src/i18n/",
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
	// A test file's length tracks the number of cases it covers, not the number
	// of concerns it carries; splitting one scatters a single subject's table
	// across files that then have to be read together.
	if s.isTest() {
		return nil
	}
	if s.lines <= maxFileLines || sizeExempt(s.rel) {
		return nil
	}
	return []Finding{{s.rel, 1, ruleFileSize,
		fmt.Sprintf("%d lines exceeds the %d-line ceiling", s.lines, maxFileLines), s.lines - maxFileLines}}
}
