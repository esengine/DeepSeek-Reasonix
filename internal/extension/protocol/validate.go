package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

type protocolValidatable interface {
	Validate() error
}

type validationFailure struct{ message string }

func (e *validationFailure) Error() string { return e.message }

func validationError(message string) error { return &validationFailure{message: message} }

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// enumTypes freezes the allowed wire values of every string enum DTO type.
// The strict decoder rejects anything outside these sets; the schema
// generator emits them as JSON Schema enums.
var enumTypes = map[reflect.Type][]string{
	reflect.TypeFor[Direction]():         values(DirectionHostToExtensionRequest, DirectionExtensionToHostRequest, DirectionHostToExtensionNotification, DirectionExtensionToHostNotification),
	reflect.TypeFor[OperationClass]():    values(ClassLifecycle, ClassIntercept, ClassObservation, ClassProvider, ClassUI, ClassContent),
	reflect.TypeFor[InterceptEvent]():    interceptEventValues(),
	reflect.TypeFor[InterceptDecision](): values(DecisionContinue, DecisionBlock, DecisionReplace, DecisionAllow, DecisionDeny),
	reflect.TypeFor[UIHostKind]():        values(UIHostTUI, UIHostDesktop, UIHostACP, UIHostHeadless),
	reflect.TypeFor[UISurfaceKind]():     values(UISurfaceStatus, UISurfaceCard, UISurfaceForm, UISurfaceNotification),
	reflect.TypeFor[UIRequestKind]():     values(UIRequestConfirm, UIRequestInput, UIRequestSelect, UIRequestMultiselect),
	reflect.TypeFor[UIFieldKind]():       values(UIFieldConfirm, UIFieldInput, UIFieldSelect, UIFieldMultiselect),
	reflect.TypeFor[UISeverity]():        values(UISeverityInfo, UISeverityWarn, UISeverityError),
	reflect.TypeFor[ProviderRole]():      values(ProviderRoleSystem, ProviderRoleUser, ProviderRoleAssistant, ProviderRoleTool),
	reflect.TypeFor[ProviderChunkType](): values(ChunkText, ChunkReasoning, ChunkToolCallStart, ChunkToolCallDelta, ChunkToolCall, ChunkUsage, ChunkDone, ChunkError),
	reflect.TypeFor[ProviderErrorCode](): values(ProviderFailed, ProviderInterrupted),
	reflect.TypeFor[ContentEncoding]():   values(ContentUTF8),
}

func init() {
	contracts := ErrorContracts()
	reasons := make([]string, len(contracts))
	for i := range contracts {
		reasons[i] = string(contracts[i].Reason)
	}
	enumTypes[reflect.TypeFor[ErrorReason]()] = reasons
}

// EnumValues returns the frozen wire values of every string enum DTO type,
// keyed by the Go type name (e.g. "InterceptEvent" → the 17 hook points). It
// is the exported form of enumTypes for code generators: the strict decoder,
// the JSON Schema, and the SDK DTO mirror all draw from this one table.
func EnumValues() map[string][]string {
	out := make(map[string][]string, len(enumTypes))
	for typ, allowed := range enumTypes {
		out[typ.Name()] = append([]string(nil), allowed...)
	}
	return out
}

func interceptEventValues() []string {
	return InterceptEvents()
}

func values[T ~string](in ...T) []string {
	out := make([]string, len(in))
	for i := range in {
		out[i] = string(in[i])
	}
	return out
}

// decodeAndValidate is the single strict decoder every direction helper
// shares: required-field presence, DisallowUnknownFields, tag validation, and
// semantic Validate methods.
func decodeAndValidate(raw json.RawMessage, typ reflect.Type) (any, error) {
	if typ.Kind() != reflect.Struct {
		return nil, errors.New("protocol registry params must be structs")
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if err := validateRequiredJSON(raw, typ, "params"); err != nil {
		return nil, err
	}
	ptr := reflect.New(typ)
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(ptr.Interface()); err != nil {
		return nil, validationError("params do not match the registered type")
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, validationError("params contain trailing JSON")
	}
	value := ptr.Elem().Interface()
	if err := validateDecoded(value); err != nil {
		return nil, err
	}
	return value, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("extra JSON value")
	}
	return err
}

func validateRequiredJSON(raw json.RawMessage, typ reflect.Type, at string) error {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return validationError(at + " must be a JSON object")
	}
	return validateRequiredObject(object, typ, at)
}

func validateRequiredObject(object map[string]json.RawMessage, typ reflect.Type, at string) error {
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
			if err := validateRequiredObject(object, field.Type, at); err != nil {
				return err
			}
			continue
		}
		fieldRaw, present := object[name]
		if !omitEmpty && !present {
			return validationError(fmt.Sprintf("%s.%s is required", at, name))
		}
		if !present {
			continue
		}
		if bytes.Equal(bytes.TrimSpace(fieldRaw), []byte("null")) {
			if field.Tag.Get("nullable") == "true" || field.Tag.Get("externalizable") == "true" {
				continue
			}
			return validationError(fmt.Sprintf("%s.%s must not be null", at, name))
		}
		if err := validateNestedRequired(fieldRaw, field.Type, at+"."+name); err != nil {
			return err
		}
	}
	return nil
}

func validateNestedRequired(raw json.RawMessage, typ reflect.Type, at string) error {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ == reflect.TypeFor[json.RawMessage]() {
		if len(bytes.TrimSpace(raw)) == 0 || !json.Valid(raw) {
			return validationError(at + " must contain valid JSON")
		}
		return nil
	}
	switch typ.Kind() {
	case reflect.Struct:
		return validateRequiredJSON(raw, typ, at)
	case reflect.Slice, reflect.Array:
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil
		}
		for i, item := range items {
			if err := validateNestedRequired(item, typ.Elem(), at+"["+strconv.Itoa(i)+"]"); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateDecoded(value any) error {
	if err := validateValue(reflect.ValueOf(value), "params", false); err != nil {
		return err
	}
	if validatable, ok := value.(protocolValidatable); ok {
		return validatable.Validate()
	}
	return nil
}

func validateValue(value reflect.Value, at string, omitEmpty bool) error {
	if !value.IsValid() {
		return nil
	}
	if value.Kind() == reflect.Interface {
		return validateValue(value.Elem(), at, omitEmpty)
	}
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil
		}
		return validateValue(value.Elem(), at, false)
	}
	typ := value.Type()
	if typ == reflect.TypeFor[json.RawMessage]() {
		raw := value.Interface().(json.RawMessage)
		if len(bytes.TrimSpace(raw)) == 0 {
			// An empty RawMessage is the zero value of an omitempty field and
			// never serializes; a present field was already JSON-checked.
			return nil
		}
		if !json.Valid(raw) {
			return validationError(at + " must contain valid JSON")
		}
		return nil
	}
	if allowed, enum := enumTypes[typ]; enum {
		if value.String() == "" && omitEmpty {
			return nil
		}
		if !contains(allowed, value.String()) {
			return validationError(fmt.Sprintf("%s has invalid enum value %q", at, value.String()))
		}
		return nil
	}
	switch value.Kind() {
	case reflect.Struct:
		for i := range value.NumField() {
			field := typ.Field(i)
			if field.PkgPath != "" {
				continue
			}
			name, fieldOmitEmpty, skip := jsonField(field)
			if skip {
				continue
			}
			childAt := at
			if name != "" {
				childAt += "." + name
			}
			if err := validateValue(value.Field(i), childAt, fieldOmitEmpty); err != nil {
				return err
			}
			if err := validateTag(value.Field(i), field.Tag.Get("validate"), childAt, fieldOmitEmpty); err != nil {
				return err
			}
			child := value.Field(i)
			if child.Kind() == reflect.Pointer && child.IsNil() {
				continue
			}
			if child.Kind() == reflect.Pointer {
				child = child.Elem()
			}
			if child.CanInterface() {
				if validatable, ok := child.Interface().(protocolValidatable); ok {
					if err := validatable.Validate(); err != nil {
						return validationError(childAt + ": " + err.Error())
					}
				}
			}
		}
	case reflect.Slice, reflect.Array:
		for i := range value.Len() {
			if err := validateValue(value.Index(i), fmt.Sprintf("%s[%d]", at, i), false); err != nil {
				return err
			}
			item := value.Index(i)
			if item.Kind() == reflect.Pointer && !item.IsNil() {
				item = item.Elem()
			}
			if item.CanInterface() {
				if validatable, ok := item.Interface().(protocolValidatable); ok {
					if err := validatable.Validate(); err != nil {
						return validationError(fmt.Sprintf("%s[%d]: %v", at, i, err))
					}
				}
			}
		}
	}
	return nil
}

// validateTag enforces the protocol's validate tag vocabulary: nonempty,
// min=, max=, sha256.
func validateTag(value reflect.Value, tags, at string, omitEmpty bool) error {
	if tags == "" || (omitEmpty && value.IsZero()) {
		return nil
	}
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil
		}
		value = value.Elem()
	}
	for tag := range strings.SplitSeq(tags, ",") {
		switch {
		case tag == "nonempty":
			if value.Kind() == reflect.String && strings.TrimSpace(value.String()) == "" {
				return validationError(at + " must be non-empty")
			}
		case strings.HasPrefix(tag, "min="):
			minimum, _ := strconv.ParseFloat(strings.TrimPrefix(tag, "min="), 64)
			if numericValue(value) < minimum {
				return validationError(at + " is below its minimum")
			}
		case strings.HasPrefix(tag, "max="):
			maximum, _ := strconv.ParseFloat(strings.TrimPrefix(tag, "max="), 64)
			if numericValue(value) > maximum {
				return validationError(at + " exceeds its maximum")
			}
		case tag == "sha256":
			if !sha256Pattern.MatchString(value.String()) {
				return validationError(at + " must be a lowercase SHA-256 hex value")
			}
		}
	}
	return nil
}

func numericValue(value reflect.Value) float64 {
	switch value.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(value.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(value.Uint())
	case reflect.Float32, reflect.Float64:
		return value.Float()
	}
	return 0
}

func contains(items []string, value string) bool {
	return slices.Contains(items, value)
}

func jsonField(field reflect.StructField) (name string, omitEmpty, skip bool) {
	tag := field.Tag.Get("json")
	parts := strings.Split(tag, ",")
	if len(parts) > 0 && parts[0] == "-" {
		return "", false, true
	}
	if len(parts) > 0 {
		name = parts[0]
	}
	for _, option := range parts[1:] {
		if option == "omitempty" || option == "omitzero" {
			omitEmpty = true
		}
	}
	if name == "" && !field.Anonymous {
		name = field.Name
		name = strings.ToLower(name[:1]) + name[1:]
	}
	return name, omitEmpty, false
}

// ExternalizablePointers lists the schema-level JSON pointer patterns ('*'
// for array items) of fields tagged externalizable on typ. Payloads at these
// locations may travel as content refs instead of inline JSON when they
// exceed ExternalizeFieldBytes.
func ExternalizablePointers(typ reflect.Type) []string {
	var out []string
	collectExternalizablePointers(typ, "", &out)
	sort.Strings(out)
	return out
}

func collectExternalizablePointers(typ reflect.Type, prefix string, out *[]string) {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	switch typ.Kind() {
	case reflect.Struct:
		for i := range typ.NumField() {
			field := typ.Field(i)
			if field.PkgPath != "" {
				continue
			}
			name, _, skip := jsonField(field)
			if skip {
				continue
			}
			if field.Anonymous && name == "" {
				collectExternalizablePointers(field.Type, prefix, out)
				continue
			}
			fieldPointer := prefix + "/" + escapeJSONPointerToken(name)
			if field.Tag.Get("externalizable") == "true" {
				*out = append(*out, fieldPointer)
				continue
			}
			collectExternalizablePointers(field.Type, fieldPointer, out)
		}
	case reflect.Slice, reflect.Array:
		collectExternalizablePointers(typ.Elem(), prefix+"/*", out)
	}
}

func escapeJSONPointerToken(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}
