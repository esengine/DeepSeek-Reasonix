package cli

import (
	"fmt"
	"strings"
)

const defaultViewWidth = 80

func viewWidth(width int) int {
	if width <= 0 {
		return defaultViewWidth
	}
	return width
}

func viewHeader(format string, args ...any) string {
	return accent(fmt.Sprintf(format, args...))
}

func viewSubhead(s string) string {
	return dim("  " + s)
}

func viewMeta(s string) string {
	return dim(s)
}

func viewStatus(s string) string {
	return accent(s)
}

func viewHint(s string) string {
	return dim("  " + s)
}

func viewMore(n int, noun string) string {
	if n <= 0 {
		return ""
	}
	return dim(fmt.Sprintf("  +%d more %s", n, noun))
}

func viewCompactPath(path string, width int) string {
	path = oneLineText(path)
	return compactMiddle(path, max(1, width))
}

func viewCompactText(s string, width int) string {
	s = oneLineText(s)
	return compactEnd(s, max(1, width))
}

func viewBodyPreview(body string, maxLines int) (string, int) {
	body = strings.TrimRight(body, "\n")
	if body == "" {
		return "", 0
	}
	lines := strings.Split(body, "\n")
	if maxLines <= 0 || len(lines) <= maxLines {
		return body, 0
	}
	return strings.Join(lines[:maxLines], "\n"), len(lines) - maxLines
}

func viewProtectLines(s string, width int) string {
	width = viewWidth(width)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = compactEnd(line, width)
	}
	return strings.Join(lines, "\n")
}

func viewPadWidth(s string, minWidth int) int {
	if w := visibleWidth(s); w > minWidth {
		return w
	}
	return minWidth
}

func viewBudget(width, used int) int {
	return max(1, viewWidth(width)-used)
}
