package plugin

import (
	"bytes"
	"encoding/json"

	"reasonix/internal/tool"
)

// parseToolResultWithSchema keeps output-schema validation advisory. Third-party
// servers sometimes publish a stale or incompatible output schema; Reasonix
// records that mismatch without discarding an otherwise usable tool result.
func parseToolResultWithSchema(res, outputSchema json.RawMessage) (string, []string, error) {
	var envelope struct {
		StructuredContent json.RawMessage `json:"structuredContent"`
	}
	if len(outputSchema) > 0 && json.Unmarshal(res, &envelope) == nil {
		structured := bytes.TrimSpace(envelope.StructuredContent)
		if len(structured) > 0 && !bytes.Equal(structured, []byte("null")) {
			validation := tool.ValidateJSONSchemaValue(outputSchema, structured)
			if validation.CompileErr != nil || len(validation.Violations) > 0 {
				hostProtocol.outputSchemaMismatch.Add(1)
			}
		}
	}
	return parseToolResult(res)
}
