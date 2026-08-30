package tool

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
)

const maxArgumentViolations = 8

// ArgumentValidator adds call-dependent checks that cannot be represented by
// one provider-visible JSON Schema. JSON Schema validation always runs first.
type ArgumentValidator interface {
	ValidateArguments(json.RawMessage) []ArgumentViolation
}

// CapabilityArgumentContract is the effective inner contract exposed when a
// stable proxy injects target-specific fields such as a skill name.
type CapabilityArgumentContract struct {
	Schema  json.RawMessage `json:"input_schema"`
	Example json.RawMessage `json:"call_example,omitempty"`
}

// CapabilityArgumentProvider lets an inspect action show the actual inner
// contract instead of the proxy's generic arguments object.
type CapabilityArgumentProvider interface {
	CapabilityArguments(capabilityID string) (CapabilityArgumentContract, bool)
}

// ArgumentViolation is a value-free description of one invalid argument. It
// intentionally contains schema expectations, never the supplied value.
type ArgumentViolation struct {
	Path     string `json:"path"`
	Keyword  string `json:"keyword"`
	Expected string `json:"expected"`
}

// ArgumentValidationResult is the host-side result for one concrete target.
// Skipped is only safe for third-party MCP schemas that cannot be compiled;
// built-in schema failures are returned in CompileErr.
type ArgumentValidationResult struct {
	Fingerprint string
	Violations  []ArgumentViolation
	Skipped     bool
	CompileErr  error
}

type compiledArgumentSchema struct {
	schema *jsonschema.Schema
	err    error
}

var argumentSchemaCache sync.Map // map[string]compiledArgumentSchema

// NormalizeArguments preserves the historical empty/null-to-object
// compatibility without guessing fields, coercing types, or rewriting values.
func NormalizeArguments(raw json.RawMessage) json.RawMessage {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return json.RawMessage(`{}`)
	}
	return append(json.RawMessage(nil), trimmed...)
}

// ValidateArguments validates args against the concrete tool's real schema and
// then runs its optional conditional validator. Compiled validators are shared
// by schema fingerprint and never resolve filesystem or network references.
func ValidateArguments(target Tool, raw json.RawMessage) ArgumentValidationResult {
	if target == nil {
		return ArgumentValidationResult{CompileErr: fmt.Errorf("argument validation target is nil")}
	}
	result := ValidateJSONSchemaValue(target.Schema(), NormalizeArguments(raw))
	if result.CompileErr != nil {
		if _, thirdParty := target.(MCPMetadata); thirdParty {
			result.Skipped = true
			result.CompileErr = nil
			return result
		}
		return result
	}

	if conditional, ok := target.(ArgumentValidator); ok && len(result.Violations) < maxArgumentViolations {
		remaining := maxArgumentViolations - len(result.Violations)
		extra := conditional.ValidateArguments(NormalizeArguments(raw))
		if len(extra) > remaining {
			extra = extra[:remaining]
		}
		result.Violations = append(result.Violations, extra...)
	}
	return result
}

// ValidateJSONSchemaValue validates an arbitrary JSON value against a schema.
// It is used for third-party MCP outputSchema telemetry as well as argument
// contracts; callers decide whether a compile failure is fatal or advisory.
func ValidateJSONSchemaValue(schemaRaw, raw json.RawMessage) ArgumentValidationResult {
	schemaRaw = bytes.TrimSpace(schemaRaw)
	fingerprint := schemaFingerprint(schemaRaw)
	result := ArgumentValidationResult{Fingerprint: fingerprint}
	compiled := loadCompiledArgumentSchema(fingerprint, schemaRaw)
	if compiled.err != nil {
		result.CompileErr = compiled.err
		return result
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		result.Violations = []ArgumentViolation{{Path: "", Keyword: "json", Expected: "one valid JSON object"}}
		return result
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		result.Violations = []ArgumentViolation{{Path: "", Keyword: "json", Expected: "one valid JSON object"}}
		return result
	}
	if err := compiled.schema.Validate(value); err != nil {
		result.Violations = validationViolations(err)
	}
	return result
}

func schemaFingerprint(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// SchemaFingerprint returns a deterministic digest for diagnostics and cache
// invalidation without returning the schema itself.
func SchemaFingerprint(raw json.RawMessage) string {
	return schemaFingerprint(bytes.TrimSpace(raw))
}

// InvalidateArgumentSchemas drops compiled validators after a catalog change.
func InvalidateArgumentSchemas(fingerprints []string) {
	for _, fingerprint := range fingerprints {
		if fingerprint != "" {
			argumentSchemaCache.Delete(fingerprint)
		}
	}
}

func loadCompiledArgumentSchema(fingerprint string, raw []byte) compiledArgumentSchema {
	if cached, ok := argumentSchemaCache.Load(fingerprint); ok {
		return cached.(compiledArgumentSchema)
	}
	compiled := compileArgumentSchema(fingerprint, raw)
	actual, _ := argumentSchemaCache.LoadOrStore(fingerprint, compiled)
	return actual.(compiledArgumentSchema)
}

func compileArgumentSchema(fingerprint string, raw []byte) compiledArgumentSchema {
	var doc any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&doc); err != nil {
		return compiledArgumentSchema{err: fmt.Errorf("invalid JSON schema: %w", err)}
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return compiledArgumentSchema{err: fmt.Errorf("invalid JSON schema: multiple values")}
	}
	obj, ok := doc.(map[string]any)
	if !ok {
		return compiledArgumentSchema{err: fmt.Errorf("JSON schema root must be an object")}
	}
	_, explicitDialect := obj["$schema"]
	compile := func(draft *jsonschema.Draft) (*jsonschema.Schema, error) {
		compiler := jsonschema.NewCompiler()
		compiler.UseLoader(nil)
		compiler.DefaultDraft(draft)
		resource := "urn:reasonix:argument-schema:" + fingerprint
		if err := compiler.AddResource(resource, doc); err != nil {
			return nil, err
		}
		return compiler.Compile(resource)
	}
	compiled, err := compile(jsonschema.Draft2020)
	if err != nil && !explicitDialect {
		compiled, err = compile(jsonschema.Draft7)
	}
	if err != nil {
		return compiledArgumentSchema{err: fmt.Errorf("compile JSON schema: %w", err)}
	}
	return compiledArgumentSchema{schema: compiled}
}

func validationViolations(err error) []ArgumentViolation {
	var validationErr *jsonschema.ValidationError
	if !errors.As(err, &validationErr) {
		return []ArgumentViolation{{Path: "", Keyword: "schema", Expected: "arguments satisfying the tool schema"}}
	}
	leaves := make([]*jsonschema.ValidationError, 0, maxArgumentViolations)
	collectValidationLeaves(validationErr, &leaves)
	violations := make([]ArgumentViolation, 0, len(leaves))
	for _, leaf := range leaves {
		keyword := "schema"
		if path := leaf.ErrorKind.KeywordPath(); len(path) > 0 {
			keyword = path[len(path)-1]
		}
		violations = append(violations, ArgumentViolation{
			Path:     jsonPointer(leaf.InstanceLocation),
			Keyword:  keyword,
			Expected: expectedForErrorKind(leaf.ErrorKind),
		})
	}
	if len(violations) == 0 {
		violations = append(violations, ArgumentViolation{Path: "", Keyword: "schema", Expected: "arguments satisfying the tool schema"})
	}
	return violations
}

func collectValidationLeaves(err *jsonschema.ValidationError, out *[]*jsonschema.ValidationError) {
	if err == nil || len(*out) >= maxArgumentViolations {
		return
	}
	if len(err.Causes) == 0 {
		*out = append(*out, err)
		return
	}
	for _, cause := range err.Causes {
		collectValidationLeaves(cause, out)
		if len(*out) >= maxArgumentViolations {
			return
		}
	}
}

func expectedForErrorKind(errorKind jsonschema.ErrorKind) string {
	switch k := errorKind.(type) {
	case *kind.Type:
		return strings.Join(k.Want, " or ")
	case *kind.Required:
		return "required properties: " + strings.Join(k.Missing, ", ")
	case *kind.AdditionalProperties:
		return "only declared properties; remove: " + strings.Join(k.Properties, ", ")
	case *kind.Enum:
		return "one of: " + boundedSchemaValues(k.Want)
	case *kind.Const:
		return "constant: " + boundedSchemaValue(k.Want)
	case *kind.MinProperties:
		return "at least " + strconv.Itoa(k.Want) + " properties"
	case *kind.MaxProperties:
		return "at most " + strconv.Itoa(k.Want) + " properties"
	case *kind.MinItems:
		return "at least " + strconv.Itoa(k.Want) + " items"
	case *kind.MaxItems:
		return "at most " + strconv.Itoa(k.Want) + " items"
	default:
		return "value satisfying " + lastKeyword(errorKind.KeywordPath())
	}
}

func boundedSchemaValues(values []any) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, boundedSchemaValue(value))
		if len(strings.Join(parts, ", ")) >= 512 {
			break
		}
	}
	return truncateASCII(strings.Join(parts, ", "), 512)
}

func boundedSchemaValue(value any) string {
	b, err := json.Marshal(value)
	if err != nil {
		return "declared schema value"
	}
	return truncateASCII(string(b), 256)
}

func lastKeyword(path []string) string {
	if len(path) == 0 {
		return "the schema"
	}
	return path[len(path)-1]
}

func jsonPointer(tokens []string) string {
	var b strings.Builder
	for _, token := range tokens {
		b.WriteByte('/')
		b.WriteString(strings.ReplaceAll(strings.ReplaceAll(token, "~", "~0"), "/", "~1"))
	}
	return b.String()
}

func truncateASCII(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	if limit <= 3 {
		return value[:limit]
	}
	return value[:limit-3] + "..."
}
