package protocol

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// SchemaDraft202012 is the JSON Schema dialect of the generated document.
const SchemaDraft202012 = "https://json-schema.org/draft/2020-12/schema"

// SchemaTitle is the generated document's human title.
const SchemaTitle = "Reasonix Extension Protocol v1"

var rawMessageType = reflect.TypeFor[json.RawMessage]()

// BuildSchemaDocument reflection-walks the frozen registry and produces the
// canonical JSON Schema (draft 2020-12) document: one methods object keyed by
// sorted method name and one $defs entry per wire DTO. The returned map
// marshals deterministically because encoding/json sorts map keys.
func BuildSchemaDocument() (map[string]any, error) {
	defs := map[string]any{}
	methods := map[string]any{}
	for _, spec := range Registry() {
		paramsRef, err := buildJSONSchema(defs, spec.ParamsType)
		if err != nil {
			return nil, fmt.Errorf("%s params: %w", spec.Name, err)
		}
		var result any
		if spec.Notification() {
			result = nil
		} else {
			result, err = buildJSONSchema(defs, spec.ResultType)
			if err != nil {
				return nil, fmt.Errorf("%s result: %w", spec.Name, err)
			}
		}
		methods[string(spec.Name)] = map[string]any{
			"direction":    string(spec.Direction),
			"class":        string(spec.Class),
			"params":       paramsRef,
			"result":       result,
			"notification": spec.Notification(),
		}
	}

	limits := FrozenLimits()
	errors := make([]any, 0, len(frozenErrorSpecs))
	for _, contract := range ErrorContracts() {
		errors = append(errors, map[string]any{
			"reason":      string(contract.Reason),
			"jsonRpcCode": contract.JSONRPCCode,
			"message":     contract.Message,
			"retryable":   contract.Retryable,
		})
	}
	interceptEvents := InterceptEvents()
	events := make([]any, len(interceptEvents))
	for i, event := range interceptEvents {
		events[i] = event
	}

	return map[string]any{
		"$schema":       SchemaDraft202012,
		"$id":           ProtocolID,
		"title":         SchemaTitle,
		"protocol":      ProtocolID,
		"protocolID":    ProtocolID,
		"protocolMajor": ProtocolMajor,
		"limits": map[string]any{
			"frameBytes":            limits.FrameBytes,
			"externalizeFieldBytes": limits.ExternalizeFieldBytes,
			"contentRefChunkBytes":  limits.ContentRefChunkBytes,
			"contentRefObjectBytes": limits.ContentRefObjectBytes,
		},
		"interceptEvents": events,
		"errors":          errors,
		"methods":         methods,
		"$defs":           defs,
	}, nil
}

// buildJSONSchema maps a Go wire type to a JSON Schema. Named structs become
// $defs entries referenced by name; unconstrained JSON (json.RawMessage, any)
// becomes the boolean schema true.
func buildJSONSchema(defs map[string]any, typ reflect.Type) (any, error) {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ == rawMessageType {
		return true, nil
	}
	if allowed, ok := enumTypes[typ]; ok {
		values := append([]string(nil), allowed...)
		sort.Strings(values)
		enum := make([]any, len(values))
		for i, value := range values {
			enum[i] = value
		}
		return map[string]any{"type": "string", "enum": enum}, nil
	}
	switch typ.Kind() {
	case reflect.Struct:
		name := typ.Name()
		if name == "" {
			return nil, fmt.Errorf("anonymous struct %v is not a named wire DTO", typ)
		}
		if _, registered := defs[name]; !registered {
			// Reserve the name before walking fields so self-referencing DTOs
			// terminate instead of recursing forever.
			defs[name] = true
			object, err := buildObjectSchema(defs, typ)
			if err != nil {
				return nil, fmt.Errorf("$defs.%s: %w", name, err)
			}
			defs[name] = object
		}
		return map[string]any{"$ref": "#/$defs/" + name}, nil
	case reflect.Slice, reflect.Array:
		if typ.Elem().Kind() == reflect.Uint8 {
			return map[string]any{"type": "string"}, nil
		}
		items, err := buildJSONSchema(defs, typ.Elem())
		if err != nil {
			return nil, err
		}
		return map[string]any{"type": "array", "items": items}, nil
	case reflect.String:
		return map[string]any{"type": "string"}, nil
	case reflect.Bool:
		return map[string]any{"type": "boolean"}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}, nil
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}, nil
	case reflect.Map:
		if typ.Key().Kind() != reflect.String {
			return nil, fmt.Errorf("unsupported wire map key type %v", typ.Key())
		}
		additional, err := buildJSONSchema(defs, typ.Elem())
		if err != nil {
			return nil, err
		}
		return map[string]any{"type": "object", "additionalProperties": additional}, nil
	case reflect.Interface:
		if typ.NumMethod() == 0 {
			return true, nil
		}
	}
	return nil, fmt.Errorf("unsupported wire type %v", typ)
}

// buildObjectSchema renders one named DTO struct as a closed JSON Schema
// object: properties sorted (by map marshal), required from omitempty
// analysis, additionalProperties:false, plus tag-derived minLength and
// minimum/maximum constraints.
func buildObjectSchema(defs map[string]any, typ reflect.Type) (map[string]any, error) {
	properties := map[string]any{}
	var required []string
	for i := range typ.NumField() {
		field := typ.Field(i)
		if field.PkgPath != "" {
			continue
		}
		name, omitEmpty, skip := jsonField(field)
		if skip {
			continue
		}
		if field.Anonymous && name == "" {
			return nil, fmt.Errorf("embedded field %v is not supported in wire DTOs", field.Type)
		}
		schema, err := buildJSONSchema(defs, field.Type)
		if err != nil {
			return nil, fmt.Errorf("field %s: %w", name, err)
		}
		schema = applyFieldTags(schema, field)
		properties[name] = schema
		if !omitEmpty {
			required = append(required, name)
		}
	}
	for i := 1; i < len(required); i++ {
		if required[i-1] == required[i] {
			return nil, fmt.Errorf("duplicate JSON field %q", required[i])
		}
	}
	sort.Strings(required)
	object := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           properties,
	}
	if len(required) > 0 {
		object["required"] = required
	}
	return object, nil
}

// applyFieldTags folds the validate/externalizable struct tags into JSON
// Schema constraints: nonempty → minLength, min=/max= → minimum/maximum,
// externalizable → the x-externalizable annotation.
func applyFieldTags(schema any, field reflect.StructField) any {
	object, ok := schema.(map[string]any)
	if !ok {
		// Unconstrained JSON (json.RawMessage, any) is the boolean schema
		// true; an externalizable tag still needs its annotation, so upgrade
		// to an annotation-only object schema, which accepts the same values.
		if externalizable(field) {
			return map[string]any{"x-externalizable": true}
		}
		return schema
	}
	for tag := range strings.SplitSeq(field.Tag.Get("validate"), ",") {
		switch {
		case tag == "nonempty":
			if object["type"] == "string" {
				object["minLength"] = 1
			}
		case strings.HasPrefix(tag, "min="):
			if minimum, err := strconv.ParseFloat(strings.TrimPrefix(tag, "min="), 64); err == nil {
				object["minimum"] = minimum
			}
		case strings.HasPrefix(tag, "max="):
			if maximum, err := strconv.ParseFloat(strings.TrimPrefix(tag, "max="), 64); err == nil {
				object["maximum"] = maximum
			}
		}
	}
	if externalizable(field) {
		object["x-externalizable"] = true
	}
	return object
}

func externalizable(field reflect.StructField) bool {
	return field.Tag.Get("externalizable") == "true"
}

var (
	schemaOnce  sync.Once
	schemaBytes []byte
	schemaErr   error
)

// CanonicalSchemaBytes is the deterministic byte form of BuildSchemaDocument:
// one compact JSON document, identical across runs and processes.
func CanonicalSchemaBytes() ([]byte, error) {
	schemaOnce.Do(func() {
		document, err := BuildSchemaDocument()
		if err != nil {
			schemaErr = err
			return
		}
		schemaBytes, schemaErr = json.Marshal(document)
	})
	if schemaErr != nil {
		return nil, schemaErr
	}
	return append([]byte(nil), schemaBytes...), nil
}

// SchemaHash returns the committed SHA-256 of the canonical schema document.
// Handshake comparisons use it to prove both peers run the identical frozen
// contract.
func SchemaHash() string {
	return GeneratedSchemaHash
}
