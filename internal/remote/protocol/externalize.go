package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// RehydratedExternalizedField is contentRef data after Base64, SHA-256, byte
// count, and UTF-8 verification. It is not a Remote RPC DTO.
type RehydratedExternalizedField struct {
	JSONPointer string
	Value       string
}

// ExternalizableString is one concrete string value reachable through a field
// marked externalizable in the protocol schema. JSONPointer is relative to the
// supplied owner and uses concrete array indexes (rather than schema '*'
// patterns).
//
// This descriptor is intentionally read-only: content storage decides which
// values to externalize, while the owner's MarshalJSON implementation remains
// the sole authority for writing the required null placeholders.
type ExternalizableString struct {
	JSONPointer string
	Value       string
}

// ExternalizableStrings walks the same externalizable struct tags used by the
// generated schema and null-placeholder marshaler. Nil string pointers are not
// values and therefore are not returned.
func ExternalizableStrings(value any) ([]ExternalizableString, error) {
	if value == nil {
		return nil, validationError("externalizable owner is nil")
	}
	var out []ExternalizableString
	if err := collectExternalizableStrings(reflect.ValueOf(value), reflect.TypeOf(value), "", &out); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].JSONPointer < out[j].JSONPointer })
	return out, nil
}

// RehydrateExternalizedJSON replaces explicit null placeholders at fields
// marked externalizable on T. It preserves JSON numbers exactly and rejects
// non-externalizable, missing, non-null, or malformed RFC 6901 targets.
func RehydrateExternalizedJSON[T any](raw []byte, fields []RehydratedExternalizedField) ([]byte, error) {
	typ := typeOf[T]()
	root, err := decodeJSONTree(raw)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(fields))
	for _, field := range fields {
		segments, err := parseExternalizablePointer(typ, field.JSONPointer)
		if err != nil {
			return nil, err
		}
		if seen[field.JSONPointer] {
			return nil, validationError("externalized replacement pointers must be unique")
		}
		seen[field.JSONPointer] = true
		if err := replaceJSONPointer(root, segments, field.Value, true); err != nil {
			return nil, err
		}
	}
	if err := rejectExternalizedNulls(root, typ, ""); err != nil {
		return nil, err
	}
	return json.Marshal(root)
}

// DecodeRehydratedJSON performs the required two-phase decode: externalized
// nulls are replaced first, then the ordinary strict typed decoder runs.
func DecodeRehydratedJSON[T any](raw []byte, fields []RehydratedExternalizedField) (T, error) {
	var zero T
	rehydrated, err := RehydrateExternalizedJSON[T](raw, fields)
	if err != nil {
		return zero, err
	}
	decoded, err := decodeAndValidate(rehydrated, typeOf[T]())
	if err != nil {
		return zero, err
	}
	return decoded.(T), nil
}

type sessionEventJSON SessionEvent

func (e SessionEvent) MarshalJSON() ([]byte, error) {
	return marshalExternalizedOwner(sessionEventJSON(e), reflect.TypeOf(e), e.Externalized)
}

type sessionSnapshotJSON SessionSnapshot

func (s SessionSnapshot) MarshalJSON() ([]byte, error) {
	return marshalExternalizedOwner(sessionSnapshotJSON(s), reflect.TypeOf(s), s.Externalized)
}

type historyPageJSON HistoryPage

func (p HistoryPage) MarshalJSON() ([]byte, error) {
	return marshalExternalizedOwner(historyPageJSON(p), reflect.TypeOf(p), p.Externalized)
}

func marshalExternalizedOwner(value any, typ reflect.Type, fields []ExternalizedField) ([]byte, error) {
	if err := validateExternalizedPointers(typ, fields); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if len(fields) == 0 {
		return raw, nil
	}
	root, err := decodeJSONTree(raw)
	if err != nil {
		return nil, err
	}
	for _, field := range fields {
		segments, err := parseExternalizablePointer(typ, field.JSONPointer)
		if err != nil {
			return nil, err
		}
		if err := replaceJSONPointer(root, segments, nil, false); err != nil {
			return nil, err
		}
	}
	return json.Marshal(root)
}

func validateExternalizedPointers(typ reflect.Type, fields []ExternalizedField) error {
	seen := make(map[string]bool, len(fields))
	for _, field := range fields {
		if err := validateDecoded(field); err != nil {
			return err
		}
		if _, err := parseExternalizablePointer(typ, field.JSONPointer); err != nil {
			return err
		}
		if seen[field.JSONPointer] {
			return validationError("externalized jsonPointer values must be unique")
		}
		seen[field.JSONPointer] = true
	}
	return nil
}

func parseExternalizablePointer(typ reflect.Type, pointer string) ([]string, error) {
	if !validJSONPointer(pointer) {
		return nil, validationError("externalized jsonPointer must be RFC 6901")
	}
	segments := strings.Split(pointer[1:], "/")
	for i := range segments {
		segments[i] = strings.ReplaceAll(strings.ReplaceAll(segments[i], "~1", "/"), "~0", "~")
	}
	if !externalizablePointerType(typ, segments) {
		return nil, validationError("jsonPointer does not identify a schema-marked externalizable string")
	}
	return segments, nil
}

func externalizablePointerType(typ reflect.Type, segments []string) bool {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if len(segments) == 0 {
		return false
	}
	switch typ.Kind() {
	case reflect.Struct:
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if field.PkgPath != "" {
				continue
			}
			name, _, skip := jsonField(field)
			if skip {
				continue
			}
			if field.Anonymous && name == "" {
				if externalizablePointerType(field.Type, segments) {
					return true
				}
				continue
			}
			if name != segments[0] {
				continue
			}
			if len(segments) == 1 {
				fieldType := field.Type
				for fieldType.Kind() == reflect.Pointer {
					fieldType = fieldType.Elem()
				}
				return field.Tag.Get("externalizable") == "true" && fieldType.Kind() == reflect.String
			}
			return externalizablePointerType(field.Type, segments[1:])
		}
	case reflect.Slice, reflect.Array:
		if _, err := canonicalArrayIndex(segments[0]); err != nil {
			return false
		}
		return externalizablePointerType(typ.Elem(), segments[1:])
	}
	return false
}

func externalizableJSONPointerPatterns(typ reflect.Type) []string {
	var out []string
	collectExternalizablePatterns(typ, "", &out)
	sort.Strings(out)
	return out
}

func collectExternalizablePatterns(typ reflect.Type, prefix string, out *[]string) {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	switch typ.Kind() {
	case reflect.Struct:
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if field.PkgPath != "" {
				continue
			}
			name, _, skip := jsonField(field)
			if skip {
				continue
			}
			if field.Anonymous && name == "" {
				collectExternalizablePatterns(field.Type, prefix, out)
				continue
			}
			fieldPointer := prefix + "/" + escapeJSONPointerToken(name)
			if field.Tag.Get("externalizable") == "true" {
				*out = append(*out, fieldPointer)
				continue
			}
			collectExternalizablePatterns(field.Type, fieldPointer, out)
		}
	case reflect.Slice, reflect.Array:
		collectExternalizablePatterns(typ.Elem(), prefix+"/*", out)
	}
}

func collectExternalizableStrings(value reflect.Value, typ reflect.Type, prefix string, out *[]ExternalizableString) error {
	for typ.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil
		}
		typ = typ.Elem()
		value = value.Elem()
	}
	switch typ.Kind() {
	case reflect.Struct:
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if field.PkgPath != "" {
				continue
			}
			name, omitEmpty, skip := jsonField(field)
			if skip {
				continue
			}
			fieldValue := value.Field(i)
			if field.Anonymous && name == "" {
				if err := collectExternalizableStrings(fieldValue, field.Type, prefix, out); err != nil {
					return err
				}
				continue
			}
			fieldPointer := prefix + "/" + escapeJSONPointerToken(name)
			if field.Tag.Get("externalizable") == "true" {
				fieldType := field.Type
				for fieldType.Kind() == reflect.Pointer {
					if fieldValue.IsNil() {
						fieldType = fieldType.Elem()
						fieldValue = reflect.Value{}
						break
					}
					fieldType = fieldType.Elem()
					fieldValue = fieldValue.Elem()
				}
				if fieldType.Kind() != reflect.String {
					return validationError("externalizable field is not a string: " + fieldPointer)
				}
				if fieldValue.IsValid() && !(omitEmpty && field.Type.Kind() == reflect.String && fieldValue.Len() == 0) {
					*out = append(*out, ExternalizableString{JSONPointer: fieldPointer, Value: fieldValue.String()})
				}
				continue
			}
			if err := collectExternalizableStrings(fieldValue, field.Type, fieldPointer, out); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < value.Len(); i++ {
			if err := collectExternalizableStrings(value.Index(i), typ.Elem(), prefix+"/"+strconv.Itoa(i), out); err != nil {
				return err
			}
		}
	}
	return nil
}

func replaceJSONPointer(node any, segments []string, replacement any, requireNull bool) error {
	if len(segments) == 0 {
		return validationError("externalized pointer cannot replace the document root")
	}
	current := node
	for index, segment := range segments {
		last := index == len(segments)-1
		switch typed := current.(type) {
		case map[string]any:
			value, exists := typed[segment]
			if last {
				if !exists {
					return validationError("externalized pointer target is absent")
				}
				if requireNull && value != nil {
					return validationError("externalized replacement target is not null")
				}
				typed[segment] = replacement
				return nil
			}
			if !exists || value == nil {
				return validationError("externalized pointer parent is absent")
			}
			current = value
		case []any:
			arrayIndex, err := canonicalArrayIndex(segment)
			if err != nil || arrayIndex >= len(typed) {
				return validationError("externalized pointer array index is out of range")
			}
			if last {
				if requireNull && typed[arrayIndex] != nil {
					return validationError("externalized replacement target is not null")
				}
				typed[arrayIndex] = replacement
				return nil
			}
			if typed[arrayIndex] == nil {
				return validationError("externalized pointer parent is null")
			}
			current = typed[arrayIndex]
		default:
			return validationError("externalized pointer crosses a scalar value")
		}
	}
	return validationError("externalized pointer was not applied")
}

func rejectExternalizedNulls(node any, typ reflect.Type, at string) error {
	if node == nil {
		return nil
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	switch typ.Kind() {
	case reflect.Struct:
		object, ok := node.(map[string]any)
		if !ok {
			return validationError("rehydrated JSON has an invalid object shape")
		}
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if field.PkgPath != "" {
				continue
			}
			name, _, skip := jsonField(field)
			if skip {
				continue
			}
			if field.Anonymous && name == "" {
				if err := rejectExternalizedNulls(node, field.Type, at); err != nil {
					return err
				}
				continue
			}
			child, exists := object[name]
			if !exists {
				continue
			}
			childAt := at + "/" + escapeJSONPointerToken(name)
			if field.Tag.Get("externalizable") == "true" && child == nil {
				// A nil *string is ordinary domain state for optional fields such
				// as Session goal, Approval reason, or Ask option description. A
				// contentRef placeholder is unambiguous because its owner also
				// carries a descriptor and DecodeRehydratedJSON receives the
				// matching replacement. Only a concrete string can never remain
				// null after that replacement pass.
				if field.Type.Kind() != reflect.Pointer {
					return validationError("externalized field remains null after rehydration: " + childAt)
				}
				continue
			}
			if child != nil {
				if err := rejectExternalizedNulls(child, field.Type, childAt); err != nil {
					return err
				}
			}
		}
	case reflect.Slice, reflect.Array:
		array, ok := node.([]any)
		if !ok {
			return validationError("rehydrated JSON has an invalid array shape")
		}
		for i, child := range array {
			if child == nil {
				continue
			}
			if err := rejectExternalizedNulls(child, typ.Elem(), fmt.Sprintf("%s/%d", at, i)); err != nil {
				return err
			}
		}
	}
	return nil
}

func decodeJSONTree(raw []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var root any
	if err := decoder.Decode(&root); err != nil {
		return nil, validationError("externalized owner JSON is invalid")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, validationError("externalized owner JSON has trailing or invalid data")
	}
	return root, nil
}

func canonicalArrayIndex(raw string) (int, error) {
	if raw == "" || (len(raw) > 1 && raw[0] == '0') {
		return 0, validationError("array index is not canonical")
	}
	index, err := strconv.Atoi(raw)
	if err != nil || index < 0 {
		return 0, validationError("array index is invalid")
	}
	return index, nil
}

func escapeJSONPointerToken(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}
