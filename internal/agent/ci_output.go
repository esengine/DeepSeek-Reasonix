package agent

import (
	"regexp"
	"strings"
)

var (
	teamcityLine = regexp.MustCompile(`(?m)^##teamcity\[.*\]$`)
	failTestLine = regexp.MustCompile(`(?i)(FAIL|ERROR|error:|failed|exit status|exit code)`)
	exitCodeLine = regexp.MustCompile(`(?i)exit (status|code)[:= ]+(-?\d+)`)
)

func summarizeCIOutput(body string) string {
	if !looksLikeCILog(body) {
		return body
	}
	lines := strings.Split(body, "\n")
	var failures []string
	exit := ""
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		if exit == "" {
			if m := exitCodeLine.FindStringSubmatch(trim); len(m) == 3 {
				exit = m[2]
			}
		}
		if failTestLine.MatchString(trim) || strings.Contains(trim, "##teamcity") {
			if len(failures) < 12 {
				failures = append(failures, trim)
			}
		}
	}
	head, tail := ciHeadTail(body, 8, 8)
	var b strings.Builder
	b.WriteString("CI/log summary (full original retained locally):\n")
	if exit != "" {
		b.WriteString("exit_code: ")
		b.WriteString(exit)
		b.WriteByte('\n')
	}
	if len(failures) > 0 {
		b.WriteString("failures:\n")
		for _, line := range failures {
			b.WriteString("- ")
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	b.WriteString("head:\n")
	b.WriteString(head)
	b.WriteString("\n...\ntail:\n")
	b.WriteString(tail)
	return strings.TrimRight(b.String(), "\n")
}

func looksLikeCILog(body string) bool {
	if teamcityLine.MatchString(body) {
		return true
	}
	if strings.Contains(body, "TeamCity") || strings.Contains(body, "BUILD FAILED") {
		return true
	}
	return strings.Count(body, "FAIL") >= 3 && len(body) > 8<<10
}

func ciHeadTail(body string, headLines, tailLines int) (string, string) {
	lines := strings.Split(body, "\n")
	if len(lines) <= headLines+tailLines {
		return body, ""
	}
	return strings.Join(lines[:headLines], "\n"), strings.Join(lines[len(lines)-tailLines:], "\n")
}
