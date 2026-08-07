package main

import "fmt"

const maxFileLines = 800

func checkSize(s *sourceFile) []Finding {
	if s.lines <= maxFileLines {
		return nil
	}
	rule := ruleFileSize
	if s.isTest() {
		rule = ruleTestSize
	}
	return []Finding{{s.rel, 1, rule,
		fmt.Sprintf("%d lines exceeds the %d-line ceiling", s.lines, maxFileLines), s.lines - maxFileLines}}
}
