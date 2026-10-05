package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/contract/eventwire"
)

func screenRows(m *model) []string {
	var rows []string
	for i := range m.scr.blocks {
		for _, l := range m.scr.blocks[i].at(80, false) {
			rows = append(rows, strings.TrimRight(ansi.Strip(l), " "))
		}
	}
	return rows
}

// An answer streamed in pieces reads as one document: blocks keep the blank
// line between them, a code block keeps its indentation, and a list that
// resumes after it keeps counting.
func TestStreamedAnswerKeepsItsLayout(t *testing.T) {
	m, _ := testModel(t)
	m.tr.AddUser("go")
	apply(m, eventwire.Event{Kind: "turn_started"})
	for _, piece := range []string{
		"## Overview\n\n",
		"A small program.\n\n",
		"1. add go.mod\n2. print the version:\n\n",
		"```go\nfunc main() {\n\tprintln(1)\n}\n```\n\n",
		"3. add a test\n",
	} {
		apply(m, eventwire.Event{Kind: "text", Text: piece})
	}
	apply(m, eventwire.Event{Kind: "message", Text: ""}, eventwire.Event{Kind: "turn_done"})

	rows := screenRows(m)
	joined := strings.Join(rows, "\n")
	find := func(s string) int {
		for i, r := range rows {
			if strings.Contains(r, s) {
				return i
			}
		}
		t.Fatalf("%q not on screen:\n%s", s, joined)
		return -1
	}
	if h, p := find("Overview"), find("A small program."); p-h != 2 || rows[h+1] != "" {
		t.Fatalf("heading and paragraph should be one blank line apart:\n%s", joined)
	}
	if l, c := find("print the version"), find("func main()"); c-l < 2 || strings.TrimSpace(rows[l+1]) != "" {
		t.Fatalf("the list and the code block after it should be a blank line apart:\n%s", joined)
	}
	body := rows[find("println(1)")]
	if !strings.Contains(body, "    println(1)") {
		t.Fatalf("the tab in the code block was lost: %q", body)
	}
	if r := rows[find("add a test")]; !strings.Contains(r, "3.") {
		t.Fatalf("the list after the code block restarted its numbering: %q", r)
	}
}

// A row cut to fit is measured in cells: text with wide runes stays on its row
// instead of spilling onto the next one without its indent.
func TestOneLineFitsWideRunes(t *testing.T) {
	s := oneLine(strings.Repeat("读入口文件 cannot start ", 20), 40)
	if w := ansi.StringWidth(s); w > 40 {
		t.Fatalf("width %d > 40: %q", w, s)
	}
}
