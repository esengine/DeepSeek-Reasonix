package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/contract/eventwire"
	"reasonix/internal/frontend/termrender"
)

const tuiSpec = `{"spec_version":1,"title":"Sales","data":{"columns":[{"name":"month","type":"string"},{"name":"revenue","type":"number"}],"rows":[["Jan",10],["Feb",null],["Mar",1234.5]]},"marks":[{"type":"bar","x":"month","y":["revenue"]}]}`

func chartTool(t *testing.T, spec string, over func(*eventwire.Tool)) *eventwire.Tool {
	t.Helper()
	args, err := json.Marshal(map[string]any{"action": "call", "capability_id": "tool:render_chart", "arguments": json.RawMessage(spec)})
	if err != nil {
		t.Fatal(err)
	}
	tool := &eventwire.Tool{ID: "c1", Name: "use_capability", ResolvedName: "render_chart", Args: string(args), Output: "chart_id: chart-abc"}
	if over != nil {
		over(tool)
	}
	return tool
}

func plain(it *Item, width int) []string {
	return strings.Split(ansi.Strip(renderTool(it, width)), "\n")
}

func TestSettledChartCallShowsItsDataAsAlignedTable(t *testing.T) {
	lines := plain(&Item{Kind: ItemTool, Tool: chartTool(t, tuiSpec, nil)}, 80)
	out := strings.Join(lines, "\n")
	for _, want := range []string{"Sales", "bar month × revenue", "month", "revenue", "Jan", "Mar"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
	header, row := -1, -1
	for i, l := range lines {
		if strings.Contains(l, "revenue") && strings.Contains(l, "month") {
			header = i
		}
		if strings.Contains(l, "1234.5") {
			row = i
		}
	}
	if header < 0 || row < 0 {
		t.Fatalf("no table in:\n%s", out)
	}
	end := func(l, s string) int { return strings.Index(l, s) + len(s) }
	if end(lines[header], "revenue") != end(lines[row], "1234.5") {
		t.Fatalf("numbers are not right-aligned under their header:\n%s", out)
	}
	if strings.Contains(out, "lines") {
		t.Fatalf("the generic line count replaced the table:\n%s", out)
	}
}

func TestChartTableStaysInsideTheWidth(t *testing.T) {
	spec := strings.Replace(tuiSpec, `"Jan"`, `"一个非常非常非常非常长的月份名称一个非常非常非常长"`, 1)
	for _, width := range []int{80, 40, 24} {
		for _, l := range plain(&Item{Kind: ItemTool, Tool: chartTool(t, spec, nil)}, width) {
			if w := termrender.VisibleWidth(l); w > width {
				t.Fatalf("width %d: %q is %d cells", width, l, w)
			}
		}
	}
}

func TestChartTableCapsRowsAndOpensWithTheFold(t *testing.T) {
	rows := make([]string, 40)
	for i := range rows {
		rows[i] = `["r",1]`
	}
	spec := strings.Replace(tuiSpec, `[["Jan",10],["Feb",null],["Mar",1234.5]]`, "["+strings.Join(rows, ",")+"]", 1)
	shut := strings.Join(plain(&Item{Kind: ItemTool, Tool: chartTool(t, spec, nil)}, 80), "\n")
	if !strings.Contains(shut, "30 more") {
		t.Fatalf("a capped table must say what it left out:\n%s", shut)
	}
	open := strings.Join(plain(&Item{Kind: ItemTool, Fold: foldOpen, Tool: chartTool(t, spec, nil)}, 80), "\n")
	if strings.Contains(open, "more") || strings.Count(open, "\n") <= strings.Count(shut, "\n") {
		t.Fatalf("opening the fold must show every row:\n%s", open)
	}
}

func TestChartCallThatIsNotSettledOrWasRefusedDrawsNoTable(t *testing.T) {
	for name, over := range map[string]func(*eventwire.Tool){
		"refused": func(tool *eventwire.Tool) { tool.Err = "chart.schema_invalid" },
		"unnamed": func(tool *eventwire.Tool) { tool.ResolvedName = "" },
	} {
		out := strings.Join(plain(&Item{Kind: ItemTool, Tool: chartTool(t, tuiSpec, over)}, 80), "\n")
		if strings.Contains(out, "revenue") && strings.Contains(out, "Mar") {
			t.Fatalf("%s: drew a table:\n%s", name, out)
		}
	}
	running := strings.Join(plain(&Item{Kind: ItemTool, Running: true, Tool: chartTool(t, tuiSpec, func(tool *eventwire.Tool) { tool.Output = "" })}, 80), "\n")
	if strings.Contains(running, "Mar") {
		t.Fatalf("a running call drew a table:\n%s", running)
	}
	bad := strings.Join(plain(&Item{Kind: ItemTool, Tool: chartTool(t, `{"spec_version":1}`, nil)}, 80), "\n")
	if strings.Contains(bad, "Sales") {
		t.Fatalf("an invalid spec drew a table:\n%s", bad)
	}
}

func TestOtherToolsKeepTheirLineCount(t *testing.T) {
	it := &Item{Kind: ItemTool, Tool: &eventwire.Tool{Name: "read_file", Args: `{"path":"a.go"}`, Output: "a\nb\nc"}}
	if out := strings.Join(plain(it, 80), "\n"); !strings.Contains(out, "3 lines") {
		t.Fatalf("read_file lost its summary:\n%s", out)
	}
}

func TestRestoredSessionDrawsTheSameChart(t *testing.T) {
	tool := chartTool(t, tuiSpec, nil)
	var tr Transcript
	tr.Restore([]HistoryMessage{
		{Role: "user", Content: "chart it"},
		{Role: "assistant", ToolCalls: []HistoryToolCall{{ID: "c1", Name: tool.Name, Arguments: tool.Args, ResolvedName: tool.ResolvedName}}},
		{Role: "tool", ToolCallID: "c1", Content: tool.Output},
	})
	live := strings.Join(plain(&Item{Kind: ItemTool, Tool: tool}, 80), "\n")
	var restored *Item
	for i := range tr.Items {
		if tr.Items[i].Kind == ItemTool {
			restored = &tr.Items[i]
		}
	}
	if restored == nil {
		t.Fatal("restore dropped the tool call")
	}
	if got := strings.Join(plain(restored, 80), "\n"); got != live {
		t.Fatalf("a reopened session draws a different chart:\n%s\nvs live\n%s", got, live)
	}
}

func TestOnlyALongChartTableIsFoldable(t *testing.T) {
	rows := make([]string, 30)
	for i := range rows {
		rows[i] = `["r",1]`
	}
	long := strings.Replace(tuiSpec, `[["Jan",10],["Feb",null],["Mar",1234.5]]`, "["+strings.Join(rows, ",")+"]", 1)
	if !(&block{row: &Item{Kind: ItemTool, Tool: chartTool(t, long, nil)}}).foldable() {
		t.Fatal("a chart past its preview must open with the fold key")
	}
	if (&block{row: &Item{Kind: ItemTool, Tool: chartTool(t, tuiSpec, nil)}}).foldable() {
		t.Fatal("a chart that fits has nothing to open")
	}
}

func TestLiveFramesMergeIntoOneChart(t *testing.T) {
	tool := chartTool(t, tuiSpec, nil)
	var tr Transcript
	tr.Apply(eventwire.Event{Kind: "tool_dispatch", Tool: &eventwire.Tool{ID: "c1", Name: tool.Name, Args: tool.Args}})
	tr.Apply(eventwire.Event{Kind: "tool_result", Tool: &eventwire.Tool{ID: "c1", Name: tool.Name, ResolvedName: tool.ResolvedName, CapabilityID: "tool:render_chart", Output: tool.Output}})
	if len(tr.Items) != 1 {
		t.Fatalf("one call became %d rows", len(tr.Items))
	}
	if out := strings.Join(plain(&tr.Items[0], 80), "\n"); !strings.Contains(out, "revenue") || !strings.Contains(out, "Mar") {
		t.Fatalf("the settled call lost its resolved name between frames:\n%s", out)
	}
}

func wideSpec(cols int, cell string) string {
	cs := make([]string, cols)
	row := make([]string, cols)
	for i := range cs {
		cs[i] = `{"name":"c` + string(rune('a'+i%26)) + string(rune('a'+i/26)) + `","type":"number"}`
		row[i] = cell
	}
	rows := "[" + strings.Join(row, ",") + "]"
	return `{"spec_version":1,"title":"Wide","data":{"columns":[` + strings.Join(cs, ",") + `],"rows":[` + rows + `,` + rows + `]},"marks":[{"type":"bar","x":"caa","y":["cba"]}]}`
}

func TestNoChartLineExceedsTheWidthAtAnyColumnCount(t *testing.T) {
	for _, cols := range []int{2, 12, 32} {
		for _, width := range []int{80, 40, 24, 12} {
			for _, l := range plain(&Item{Kind: ItemTool, Tool: chartTool(t, wideSpec(cols, "1234567"), nil)}, width) {
				if w := termrender.VisibleWidth(l); w > width {
					t.Fatalf("%d columns at width %d: %q is %d cells", cols, width, l, w)
				}
			}
		}
	}
}

func TestColumnsThatDoNotFitAreDroppedAndCounted(t *testing.T) {
	out := strings.Join(plain(&Item{Kind: ItemTool, Tool: chartTool(t, wideSpec(12, "1234567"), nil)}, 40), "\n")
	if !strings.Contains(out, "more columns") {
		t.Fatalf("dropped columns must be announced:\n%s", out)
	}
	if !strings.Contains(out, "1234567") {
		t.Fatalf("a kept number was cut short:\n%s", out)
	}
	if all := strings.Join(plain(&Item{Kind: ItemTool, Tool: chartTool(t, wideSpec(3, "1"), nil)}, 80), "\n"); strings.Contains(all, "more columns") {
		t.Fatalf("nothing was dropped:\n%s", all)
	}
}

func TestNumbersKeepTheirWholeValueBeforeTextIsCut(t *testing.T) {
	spec := strings.Replace(tuiSpec, `1234.5`, `1234567890`, 1)
	spec = strings.Replace(spec, `"Jan"`, `"a-long-month-label-that-must-give-way"`, 1)
	out := strings.Join(plain(&Item{Kind: ItemTool, Tool: chartTool(t, spec, nil)}, 32), "\n")
	if !strings.Contains(out, "1234567890") {
		t.Fatalf("a number was truncated while text could shrink:\n%s", out)
	}
}

func TestFoldHintOnlyWhileTheFoldIsShut(t *testing.T) {
	rows := make([]string, 300)
	for i := range rows {
		rows[i] = `["r",1]`
	}
	spec := strings.Replace(tuiSpec, `[["Jan",10],["Feb",null],["Mar",1234.5]]`, "["+strings.Join(rows, ",")+"]", 1)
	hint := func(f outputFold) bool {
		out := strings.Join(plain(&Item{Kind: ItemTool, Fold: f, Tool: chartTool(t, spec, nil)}, 80), "\n")
		return strings.Contains(out, "(Ctrl+B)")
	}
	if !hint(foldShut) || hint(foldOpen) || hint(foldFixed) {
		t.Fatalf("hint shut=%v open=%v fixed=%v", hint(foldShut), hint(foldOpen), hint(foldFixed))
	}
}
