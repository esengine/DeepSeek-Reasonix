package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"reasonix/internal/contract/chartspec"
	"reasonix/internal/contract/tool"
)

const chartArgs = `{"spec_version":1,"title":"Sales","data":{"columns":[{"name":"m","type":"string"},{"name":"v","type":"number"}],"rows":[["jan",1],["feb",3]]},"marks":[{"type":"bar","x":"m","y":["v"]}]}`

func TestRenderChartResultCarriesIDAndSummaryNotRows(t *testing.T) {
	tl, ok := tool.LookupBuiltin(ChartToolName)
	if !ok {
		t.Fatal("render_chart is not a registered built-in")
	}
	out, err := tl.Execute(context.Background(), []byte(chartArgs))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "chart_id: chart-") || !strings.Contains(out, `series "v" n=2 min=1 max=3`) {
		t.Fatalf("unexpected result:\n%s", out)
	}
	if strings.Contains(out, "jan") {
		t.Fatalf("rows must not be echoed:\n%s", out)
	}
	again, _ := tl.Execute(context.Background(), []byte(chartArgs))
	if again != out {
		t.Fatal("same spec must give the same result")
	}
}

func TestRenderChartRefusesWithTypedCode(t *testing.T) {
	tl, _ := tool.LookupBuiltin(ChartToolName)
	for name, raw := range map[string]string{
		"unknown field": strings.Replace(chartArgs, `"title"`, `"url":"http://x","title"`, 1),
		"script label":  strings.Replace(chartArgs, `"Sales"`, `"a‮b"`, 1),
		"not json":      `{`,
	} {
		out, err := tl.Execute(context.Background(), []byte(raw))
		if !errors.Is(err, chartspec.ErrSchemaInvalid) || out != "" {
			t.Errorf("%s: want chart.schema_invalid and no result, got %q %v", name, out, err)
		}
	}
}

func TestRenderChartIsReadOnlyAndSchemaIsJSON(t *testing.T) {
	tl, _ := tool.LookupBuiltin(ChartToolName)
	if !tl.ReadOnly() {
		t.Fatal("render_chart has no side effects")
	}
	var v map[string]any
	if err := json.Unmarshal(tl.Schema(), &v); err != nil {
		t.Fatalf("schema is not JSON: %v", err)
	}
}
