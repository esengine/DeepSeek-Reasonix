package provider

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

const toolSchemaResource = "urn:reasonix:tool-schema"

// ValidateToolSchema compiles a provider-visible tool parameter schema without
// resolving external resources. MCP schemas default to draft-07 when they do
// not declare a dialect; explicit $schema declarations still take precedence.
func ValidateToolSchema(raw json.RawMessage) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var doc any
	if err := decoder.Decode(&doc); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("invalid JSON: multiple values")
		}
		return fmt.Errorf("invalid JSON: %w", err)
	}
	obj, ok := doc.(map[string]any)
	if !ok {
		return fmt.Errorf("root must be an object")
	}
	// MCP tools/list and the Anthropic tool contract both require the root
	// schema to describe an object; anything else can 400 the whole request.
	// CanonicalizeSchema makes an omitted root type explicit before validation,
	// so a missing or non-"object" type here is a genuinely incompatible schema.
	switch typ := obj["type"].(type) {
	case string:
		if typ != "object" {
			return fmt.Errorf("root type must be %q, got %q", "object", typ)
		}
	case nil:
		return fmt.Errorf("root schema must declare type %q", "object")
	default:
		return fmt.Errorf("root type must be %q, got %s", "object", schemaJSONString(typ))
	}

	compiler := jsonschema.NewCompiler()
	// The default loader resolves file:// refs from local disk. Externally
	// supplied MCP schemas must never reach the filesystem or network, so
	// drop the loader entirely: registered resources and the embedded
	// metaschemas still resolve, every other URL fails compilation.
	compiler.UseLoader(nil)
	compiler.DefaultDraft(jsonschema.Draft7)
	if err := compiler.AddResource(toolSchemaResource, doc); err != nil {
		return fmt.Errorf("load schema: %w", err)
	}
	if _, err := compiler.Compile(toolSchemaResource); err != nil {
		return fmt.Errorf("compile schema: %w", err)
	}
	return nil
}
