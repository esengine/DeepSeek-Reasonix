package browseripc

import (
	"encoding/json"
	"testing"
)

type schemaDocument struct {
	Format          string `json:"format"`
	ProtocolVersion int    `json:"protocolVersion"`
	Limits          struct {
		FrameBytes         int `json:"frameBytes"`
		PendingRequests    int `json:"pendingRequests"`
		ResponseTimeoutMs  int `json:"responseTimeoutMs"`
		ShutdownGraceMs    int `json:"shutdownGraceMs"`
		MaxTextChars       int `json:"maxTextChars"`
		MaxScreenshotBytes int `json:"maxScreenshotBytes"`
		MaxRequestIDBytes  int `json:"maxRequestIDBytes"`
		MaxOwnerIDBytes    int `json:"maxOwnerIDBytes"`
		MaxMethodBytes     int `json:"maxMethodBytes"`
		MaxTabIDBytes      int `json:"maxTabIDBytes"`
		MaxOriginBytes     int `json:"maxOriginBytes"`
		MaxURLBytes        int `json:"maxUrlBytes"`
	} `json:"limits"`
	ErrorCodes []string `json:"errorCodes"`
	Methods    []struct {
		Name      string          `json:"name"`
		Direction string          `json:"direction"`
		Params    schemaType      `json:"params"`
		Result    schemaType      `json:"result"`
		raw       json.RawMessage `json:"-"`
	} `json:"methods"`
	Events []struct {
		Name string     `json:"name"`
		Data schemaType `json:"data"`
	} `json:"events"`
}

type schemaType struct {
	Type       string                `json:"type"`
	Properties map[string]schemaProp `json:"properties"`
	Required   []string              `json:"required"`
	Enum       []string              `json:"enum"`
	Items      *schemaType           `json:"items"`
}

type schemaProp struct {
	Type  string      `json:"type"`
	Enum  []string    `json:"enum"`
	Items *schemaType `json:"items"`
}

func loadSchema(t *testing.T) schemaDocument {
	t.Helper()
	var doc schemaDocument
	if err := json.Unmarshal(schemaJSON, &doc); err != nil {
		t.Fatalf("schema.json does not parse: %v", err)
	}
	if doc.Format != "reasonix.browser.ipc.v1" {
		t.Fatalf("schema format %q", doc.Format)
	}
	if doc.ProtocolVersion != ProtocolVersion {
		t.Fatalf("schema protocolVersion %d != Go %d", doc.ProtocolVersion, ProtocolVersion)
	}
	return doc
}

// TestSchemaParity is the golden guard between schema.json and the hand-written
// Go wire surface. It proves limits, error codes, method/event names, and that
// the Go validator accepts every schema-legal params document.
func TestSchemaParity(t *testing.T) {
	doc := loadSchema(t)

	limits := []struct {
		name string
		want int
		got  int
	}{
		{"frameBytes", doc.Limits.FrameBytes, FrameMaxBytes},
		{"pendingRequests", doc.Limits.PendingRequests, MaxPendingRequests},
		{"responseTimeoutMs", doc.Limits.ResponseTimeoutMs, ResponseTimeoutMs},
		{"shutdownGraceMs", doc.Limits.ShutdownGraceMs, ShutdownGraceMs},
		{"maxTextChars", doc.Limits.MaxTextChars, MaxTextChars},
		{"maxScreenshotBytes", doc.Limits.MaxScreenshotBytes, MaxScreenshotBytes},
		{"maxRequestIDBytes", doc.Limits.MaxRequestIDBytes, MaxRequestIDBytes},
		{"maxOwnerIDBytes", doc.Limits.MaxOwnerIDBytes, MaxOwnerIDBytes},
		{"maxMethodBytes", doc.Limits.MaxMethodBytes, MaxMethodBytes},
		{"maxTabIDBytes", doc.Limits.MaxTabIDBytes, MaxTabIDBytes},
		{"maxOriginBytes", doc.Limits.MaxOriginBytes, MaxOriginBytes},
		{"maxUrlBytes", doc.Limits.MaxURLBytes, MaxURLBytes},
	}
	for _, l := range limits {
		if l.want != l.got {
			t.Errorf("schema %s=%d but Go constant=%d", l.name, l.want, l.got)
		}
	}

	if len(doc.ErrorCodes) != len(ErrorCodes) {
		t.Fatalf("schema has %d error codes, Go has %d", len(doc.ErrorCodes), len(ErrorCodes))
	}
	for i, code := range doc.ErrorCodes {
		if code != string(ErrorCodes[i]) {
			t.Errorf("error code %d: schema %q != Go %q", i, code, ErrorCodes[i])
		}
	}

	if len(doc.Methods) != len(MethodNames) {
		t.Fatalf("schema has %d methods, Go has %d", len(doc.Methods), len(MethodNames))
	}
	for i, m := range doc.Methods {
		if m.Name != MethodNames[i] {
			t.Errorf("method %d: schema %q != Go %q", i, m.Name, MethodNames[i])
		}
		if m.Direction != "client_request" {
			t.Errorf("method %s: direction %q", m.Name, m.Direction)
		}
		if m.Params.Type != "object" {
			t.Errorf("method %s: params must be object", m.Name)
		}
		if m.Result.Type != "object" {
			t.Errorf("method %s: result must be object", m.Name)
		}
	}

	if len(doc.Events) != len(EventNames) {
		t.Fatalf("schema has %d events, Go has %d", len(doc.Events), len(EventNames))
	}
	for i, e := range doc.Events {
		if e.Name != EventNames[i] {
			t.Errorf("event %d: schema %q != Go %q", i, e.Name, EventNames[i])
		}
	}
}

// TestSchemaSamplesValidate proves the Go validator accepts a params document
// generated from every schema method (all properties filled, required only
// enforced by the schema). A mismatch means the Go structs drifted from the
// schema.
func TestSchemaSamplesValidate(t *testing.T) {
	doc := loadSchema(t)
	for _, m := range doc.Methods {
		params := sampleParams(m.Params)
		req := Request{
			ProtocolVersion: ProtocolVersion,
			RequestID:       "r1",
			OwnerID:         "owner",
			Method:          m.Name,
			Params:          mustJSON(t, params),
		}
		if err := ValidateRequest(req); err != nil {
			t.Errorf("method %s: schema-legal params rejected: %v", m.Name, err)
		}
	}
}

func sampleParams(s schemaType) map[string]any {
	out := make(map[string]any, len(s.Properties))
	for name, prop := range s.Properties {
		out[name] = sampleValue(name, prop)
	}
	return out
}

func sampleValue(name string, p schemaProp) any {
	if len(p.Enum) > 0 {
		return p.Enum[0]
	}
	switch p.Type {
	case "string":
		if name == "url" {
			// ValidateRequest enforces the http(s) URL contract.
			return "https://example.com"
		}
		return "x"
	case "integer":
		return 1
	case "boolean":
		return true
	case "array":
		if p.Items != nil {
			item := schemaProp{Type: p.Items.Type, Enum: p.Items.Enum, Items: p.Items.Items}
			return []any{sampleValue("item", item)}
		}
		return []any{}
	default:
		return map[string]any{}
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal sample: %v", err)
	}
	return b
}
