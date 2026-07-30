package protocol

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"

	"reasonix/internal/eventwire"
)

const SchemaFormat = "reasonix.remote.schema.v1"

type SchemaDocument struct {
	Format          string                `json:"format"`
	ProtocolVersion string                `json:"protocolVersion"`
	Methods         []SchemaMethod        `json:"methods"`
	ErrorData       SchemaType            `json:"errorData"`
	Errors          []ErrorContract       `json:"errors"`
	Event           EventSchema           `json:"event"`
	Features        FeatureContract       `json:"features"`
	Resources       FixedResourceContract `json:"resources"`
}

type SchemaMethod struct {
	Name      Method         `json:"name"`
	Direction Direction      `json:"direction"`
	Class     OperationClass `json:"class"`
	Params    SchemaType     `json:"params"`
	Result    SchemaType     `json:"result"`
}

type SchemaType struct {
	Type                 string            `json:"type"`
	Nullable             bool              `json:"nullable,omitempty"`
	Format               string            `json:"format,omitempty"`
	Enum                 []string          `json:"enum,omitempty"`
	Constraints          []string          `json:"constraints,omitempty"`
	Properties           []SchemaProperty  `json:"properties,omitempty"`
	Items                *SchemaType       `json:"items,omitempty"`
	AdditionalProperties *SchemaType       `json:"additionalProperties,omitempty"`
	Validation           *SchemaValidation `json:"validation,omitempty"`
}

// SchemaValidation records semantics enforced by custom Validate methods that
// cannot be inferred from field tags alone. Discriminator variants are also
// consumed by generated clients to retain strict union shapes.
type SchemaValidation struct {
	Invariants    []string             `json:"invariants,omitempty"`
	Discriminator *SchemaDiscriminator `json:"discriminator,omitempty"`
}

type SchemaDiscriminator struct {
	Property string          `json:"property"`
	Variants []SchemaVariant `json:"variants"`
}

type SchemaVariant struct {
	Values        []string `json:"values"`
	Required      []string `json:"required,omitempty"`
	Forbidden     []string `json:"forbidden,omitempty"`
	RequiredTrue  []string `json:"requiredTrue,omitempty"`
	RequiredFalse []string `json:"requiredFalse,omitempty"`
}

type SchemaProperty struct {
	Name           string     `json:"name"`
	Required       bool       `json:"required"`
	Externalizable bool       `json:"externalizable,omitempty"`
	Schema         SchemaType `json:"schema"`
}

type EventSchema struct {
	Payload                    SchemaType `json:"payload"`
	Kinds                      []string   `json:"kinds"`
	ExternalizableJSONPointers []string   `json:"externalizableJsonPointers"`
}

type FeatureContract struct {
	RequiredTrue  []string `json:"requiredTrue"`
	Dynamic       []string `json:"dynamic"`
	RequiredFalse []string `json:"requiredFalse"`
}

type FixedResourceContract struct {
	Protocol    ProtocolLimits    `json:"protocol"`
	Lease       LeaseLimits       `json:"lease"`
	Idempotency IdempotencyLimits `json:"idempotency"`
}

func BuildSchemaDocument() (SchemaDocument, error) {
	methods := make([]SchemaMethod, 0, len(frozenRegistry))
	for _, spec := range Registry() {
		params, err := buildSchemaType(spec.ParamsType)
		if err != nil {
			return SchemaDocument{}, fmt.Errorf("%s params: %w", spec.Name, err)
		}
		result, err := buildSchemaType(spec.ResultType)
		if err != nil {
			return SchemaDocument{}, fmt.Errorf("%s result: %w", spec.Name, err)
		}
		methods = append(methods, SchemaMethod{spec.Name, spec.Direction, spec.Class, params, result})
	}
	errorData, err := buildSchemaType(reflect.TypeOf(RemoteErrorData{}))
	if err != nil {
		return SchemaDocument{}, err
	}
	eventSchema, err := buildEventSchema(reflect.TypeOf(eventwire.Event{}))
	if err != nil {
		return SchemaDocument{}, err
	}
	return SchemaDocument{
		Format: SchemaFormat, ProtocolVersion: ProtocolVersion, Methods: methods,
		ErrorData: errorData, Errors: ErrorContracts(),
		Event: eventSchema,
		Features: FeatureContract{
			RequiredTrue:  sortedStrings("coreSession", "primaryFileQueries", "userShell", "jobCancel"),
			Dynamic:       sortedStrings("memory", "research"),
			RequiredFalse: sortedStrings("mediaPreview", "attachments", "clipboardImages", "sftp", "localPathOperations", "gitWrite", "pty", "deliveryWorktree"),
		},
		Resources: FixedResourceContract{
			Protocol:    FrozenProtocolLimits(),
			Lease:       LeaseLimits{TTLMillis: LeaseTTLMillis, PingIntervalMillis: LeasePingIntervalMillis},
			Idempotency: IdempotencyLimits{RetentionHours: IdempotencyRetentionHours, PerSessionEntries: IdempotencySessionEntries, PerHostEntries: IdempotencyHostEntries},
		},
	}, nil
}

func buildEventSchema(typ reflect.Type) (EventSchema, error) {
	payload, err := buildSchemaType(typ)
	if err != nil {
		return EventSchema{}, err
	}
	kinds := eventwire.KindNames()
	sort.Strings(kinds)
	return EventSchema{
		Payload: payload, Kinds: kinds,
		ExternalizableJSONPointers: externalizableJSONPointerPatterns(typ),
	}, nil
}

func buildSchemaType(typ reflect.Type) (SchemaType, error) {
	nullable := false
	for typ.Kind() == reflect.Pointer {
		nullable = true
		typ = typ.Elem()
	}
	if typ == reflect.TypeOf(json.RawMessage{}) {
		return SchemaType{Type: "json", Nullable: nullable}, nil
	}
	if allowed, ok := enumTypes[typ]; ok {
		values := append([]string(nil), allowed...)
		sort.Strings(values)
		return SchemaType{Type: "string", Nullable: nullable, Enum: values}, nil
	}
	if _, ok := opaqueTypes[typ]; ok {
		return SchemaType{Type: "string", Nullable: nullable, Format: "opaque", Constraints: []string{"minLength=1"}}, nil
	}
	schema := SchemaType{Nullable: nullable}
	switch typ.Kind() {
	case reflect.Struct:
		schema.Type = "object"
		properties, err := schemaProperties(typ)
		if err != nil {
			return SchemaType{}, err
		}
		schema.Properties = properties
	case reflect.Slice, reflect.Array:
		schema.Type = "array"
		item, err := buildSchemaType(typ.Elem())
		if err != nil {
			return SchemaType{}, err
		}
		schema.Items = &item
	case reflect.String:
		schema.Type = "string"
	case reflect.Bool:
		schema.Type = "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		schema.Type = "integer"
	case reflect.Float32, reflect.Float64:
		schema.Type = "number"
	case reflect.Map:
		schema.Type = "object"
		additional, err := buildSchemaType(typ.Elem())
		if err != nil {
			return SchemaType{}, err
		}
		schema.AdditionalProperties = &additional
	default:
		return SchemaType{}, fmt.Errorf("unsupported wire type %v", typ)
	}
	validation, err := schemaValidationFor(typ)
	if err != nil {
		return SchemaType{}, err
	}
	if err := validateSchemaContract(schema, validation); err != nil {
		return SchemaType{}, fmt.Errorf("%v schema validation contract: %w", typ, err)
	}
	schema.Validation = validation
	return schema, nil
}

func schemaProperties(typ reflect.Type) ([]SchemaProperty, error) {
	properties := make([]SchemaProperty, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.PkgPath != "" {
			continue
		}
		name, omitEmpty, skip := jsonField(field)
		if skip {
			continue
		}
		if field.Anonymous && name == "" {
			embedded := field.Type
			for embedded.Kind() == reflect.Pointer {
				embedded = embedded.Elem()
			}
			if embedded.Kind() != reflect.Struct {
				return nil, fmt.Errorf("anonymous wire field %v is not a struct", field.Type)
			}
			nested, err := schemaProperties(embedded)
			if err != nil {
				return nil, err
			}
			properties = append(properties, nested...)
			continue
		}
		fieldSchema, err := buildSchemaType(field.Type)
		if err != nil {
			return nil, fmt.Errorf("field %s: %w", name, err)
		}
		fieldSchema.Constraints = append(fieldSchema.Constraints, schemaConstraints(field.Tag.Get("validate"))...)
		sort.Strings(fieldSchema.Constraints)
		// Go pointers model optional presence for most DTO fields; they do not
		// imply that an explicitly present JSON null is accepted. Nullability is
		// an explicit wire property reserved for nullable/externalizable tags.
		fieldSchema.Nullable = field.Tag.Get("nullable") == "true" || field.Tag.Get("externalizable") == "true"
		properties = append(properties, SchemaProperty{
			Name: name, Required: !omitEmpty, Externalizable: field.Tag.Get("externalizable") == "true", Schema: fieldSchema,
		})
	}
	sort.Slice(properties, func(i, j int) bool { return properties[i].Name < properties[j].Name })
	for i := 1; i < len(properties); i++ {
		if properties[i-1].Name == properties[i].Name {
			return nil, fmt.Errorf("duplicate JSON field %q", properties[i].Name)
		}
	}
	return properties, nil
}

func schemaConstraints(tag string) []string {
	if tag == "" {
		return nil
	}
	constraints := strings.Split(tag, ",")
	out := constraints[:0]
	for _, constraint := range constraints {
		if constraint != "" {
			out = append(out, constraint)
		}
	}
	return out
}

func sortedStrings(values ...string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

var (
	schemaOnce  sync.Once
	schemaBytes []byte
	schemaErr   error
)

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

func SchemaHash() string {
	return GeneratedSchemaHash
}
