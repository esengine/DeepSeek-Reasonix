package builtin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"reasonix/internal/contract/chartspec"
	"reasonix/internal/contract/tool"
)

func init() { tool.RegisterBuiltin(renderChart{}) }

// ChartToolName is the name a frontend recognises a chart call by.
const ChartToolName = "render_chart"

// renderChart validates a chart spec and answers with its identity and a text
// summary. The spec itself rides in the call's arguments, which the session
// already stores; nothing here draws, fetches or writes.
type renderChart struct{}

func (renderChart) Name() string { return ChartToolName }

func (renderChart) Description() string {
	return "Show the user a chart inline. Rows hold only numbers, strings and nulls; no URLs, markup or colours. Returns a chart_id and a text summary, not the rows."
}

func (renderChart) ReadOnly() bool { return true }

func (renderChart) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"required":["spec_version","title","data","marks"],"properties":{` +
		`"spec_version":{"type":"integer","minimum":1,"maximum":1},"title":{"type":"string"},` +
		`"data":{"type":"object","required":["columns","rows"],"properties":{"columns":{"type":"array","items":{"type":"object","required":["name","type"],"properties":{"name":{"type":"string"},"type":{"type":"string","enum":["number","string","date"]}}}},"rows":{"type":"array","items":{"type":"array"}}}},` +
		`"marks":{"type":"array","items":{"type":"object","required":["type","x","y"],"properties":{"type":{"type":"string","enum":["bar","line","pie"]},"x":{"type":"string"},"y":{"type":"array","items":{"type":"string"}},"color":{"type":"string"},"stacked":{"type":"boolean"},"donut":{"type":"boolean"}}}},` +
		`"x_axis":{"type":"object"},"y_axis":{"type":"object"}}}`)
}

// Execute refuses with a *chartspec.Error, whose code is the identity a caller
// branches on; the spec is never echoed back.
func (renderChart) Execute(_ context.Context, args json.RawMessage) (string, error) {
	spec, err := chartspec.Parse(args)
	if err != nil {
		return "", err
	}
	return "chart_id: " + ChartID(spec) + "\n" + spec.Summary(), nil
}

// ChartID names a spec by its canonical bytes, so the same chart has one id
// wherever it is rebuilt from.
func ChartID(spec *chartspec.Spec) string {
	b, err := spec.Canonical()
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return "chart-" + hex.EncodeToString(sum[:6])
}
