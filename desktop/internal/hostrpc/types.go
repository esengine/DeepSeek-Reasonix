package hostrpc

import (
	"encoding"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"
)

// Kind is the JSON shape a TypeRef describes.
type Kind string

const (
	KindString   Kind = "string"
	KindNumber   Kind = "number"
	KindInteger  Kind = "integer"
	KindBoolean  Kind = "boolean"
	KindAny      Kind = "any"
	KindArray    Kind = "array"
	KindMap      Kind = "map"
	KindObject   Kind = "object"
	KindNullable Kind = "nullable"
)

// TypeRef describes one JSON value shape. Elem is the element of an array,
// map or nullable; Key is a map's key shape; Ref names an entry of
// Contract.Types for objects.
type TypeRef struct {
	Kind Kind     `json:"kind"`
	Elem *TypeRef `json:"elem,omitempty"`
	Key  *TypeRef `json:"key,omitempty"`
	Ref  string   `json:"ref,omitempty"`
}

// Field is one member of an ObjectType. Optional marks members the encoder
// may omit (omitempty, omitzero) or emit as null (pointers).
type Field struct {
	Name     string  `json:"name"`
	Type     TypeRef `json:"type"`
	Optional bool    `json:"optional,omitempty"`
}

// ObjectType is a Go struct as encoding/json writes it.
type ObjectType struct {
	Fields []Field `json:"fields"`
}

var (
	timeType          = reflect.TypeFor[time.Time]()
	rawMessageType    = reflect.TypeFor[json.RawMessage]()
	textMarshalerType = reflect.TypeFor[encoding.TextMarshaler]()
	jsonMarshalerType = reflect.TypeFor[json.Marshaler]()
	errorType         = reflect.TypeFor[error]()
)

// typeCollector builds TypeRefs and gathers every named struct it crosses,
// keyed by pkg.Name, so a contract lists each DTO exactly once.
type typeCollector struct {
	types map[string]ObjectType
	named map[reflect.Type]string
}

func newTypeCollector() *typeCollector {
	return &typeCollector{types: map[string]ObjectType{}, named: map[reflect.Type]string{}}
}

// ref describes t. hint names an anonymous struct reached through t.
func (c *typeCollector) ref(t reflect.Type, hint string) (TypeRef, error) {
	switch {
	case t == timeType:
		return TypeRef{Kind: KindString}, nil
	case t == rawMessageType:
		return TypeRef{Kind: KindAny}, nil
	case !implements(t, jsonMarshalerType) && implements(t, textMarshalerType):
		return TypeRef{Kind: KindString}, nil
	}
	switch t.Kind() {
	case reflect.Interface:
		return TypeRef{Kind: KindAny}, nil
	case reflect.Pointer:
		return c.wrap(KindNullable, t.Elem(), hint)
	case reflect.String:
		return TypeRef{Kind: KindString}, nil
	case reflect.Bool:
		return TypeRef{Kind: KindBoolean}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return TypeRef{Kind: KindInteger}, nil
	case reflect.Float32, reflect.Float64:
		return TypeRef{Kind: KindNumber}, nil
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 && !implements(t.Elem(), jsonMarshalerType) {
			return TypeRef{Kind: KindString}, nil
		}
		return c.wrap(KindArray, t.Elem(), hint)
	case reflect.Array:
		return c.wrap(KindArray, t.Elem(), hint)
	case reflect.Map:
		return c.mapRef(t, hint)
	case reflect.Struct:
		return c.structRef(t, hint)
	}
	return TypeRef{}, fmt.Errorf("%s is not JSON-serialisable", t)
}

func (c *typeCollector) wrap(kind Kind, elem reflect.Type, hint string) (TypeRef, error) {
	ref, err := c.ref(elem, hint)
	if err != nil {
		return TypeRef{}, err
	}
	return TypeRef{Kind: kind, Elem: &ref}, nil
}

func (c *typeCollector) mapRef(t reflect.Type, hint string) (TypeRef, error) {
	var key TypeRef
	switch kt := t.Key(); {
	case kt.Kind() == reflect.String || implements(kt, textMarshalerType):
		key = TypeRef{Kind: KindString}
	case isIntegerKind(kt.Kind()):
		key = TypeRef{Kind: KindInteger}
	default:
		return TypeRef{}, fmt.Errorf("%s: map key %s is not JSON-serialisable", t, kt)
	}
	elem, err := c.ref(t.Elem(), hint)
	if err != nil {
		return TypeRef{}, err
	}
	return TypeRef{Kind: KindMap, Key: &key, Elem: &elem}, nil
}

func (c *typeCollector) structRef(t reflect.Type, hint string) (TypeRef, error) {
	name, seen := c.named[t]
	if seen {
		return TypeRef{Kind: KindObject, Ref: name}, nil
	}
	// Type.String uses the package name, so "main.TabMeta" is the same key in
	// the shipped binary and in the test binary that compiles package main
	// under its import path; PkgPath would split the digest between them.
	name = hint
	if t.Name() != "" {
		name = t.String()
	}
	if _, taken := c.types[name]; taken {
		return TypeRef{}, fmt.Errorf("%s: type name %q is already used by another type", t, name)
	}
	c.named[t] = name
	c.types[name] = ObjectType{}
	fields, err := c.fields(t, name)
	if err != nil {
		return TypeRef{}, fmt.Errorf("%s: %w", name, err)
	}
	c.types[name] = ObjectType{Fields: fields}
	return TypeRef{Kind: KindObject, Ref: name}, nil
}

type fieldCandidate struct {
	field  Field
	depth  int
	tagged bool
}

func (c *typeCollector) fields(t reflect.Type, owner string) ([]Field, error) {
	var found []fieldCandidate
	if err := c.walkFields(t, owner, 0, &found); err != nil {
		return nil, err
	}
	return dominantFields(found), nil
}

// walkFields mirrors encoding/json: embedded structs without a tag name are
// flattened, tagged or non-struct embeds become members named after the type.
func (c *typeCollector) walkFields(t reflect.Type, owner string, depth int, out *[]fieldCandidate) error {
	for i := range t.NumField() {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		if f.Anonymous && name == "" {
			et := f.Type
			if et.Kind() == reflect.Pointer {
				et = et.Elem()
			}
			if et.Kind() == reflect.Struct {
				if err := c.walkFields(et, owner, depth+1, out); err != nil {
					return err
				}
				continue
			}
		}
		if !f.IsExported() {
			continue
		}
		if name == "" {
			name = f.Name
		}
		ref, err := c.ref(f.Type, owner+"."+f.Name)
		if err != nil {
			return fmt.Errorf("field %s: %w", f.Name, err)
		}
		if hasOption(opts, "string") && quotable(f.Type) {
			ref = TypeRef{Kind: KindString}
		}
		optional := hasOption(opts, "omitempty") || hasOption(opts, "omitzero") || f.Type.Kind() == reflect.Pointer
		*out = append(*out, fieldCandidate{
			field:  Field{Name: name, Type: ref, Optional: optional},
			depth:  depth,
			tagged: tag != "",
		})
	}
	return nil
}

// dominantFields applies encoding/json's shadowing: the shallowest member
// wins, a tagged one breaks a tie, an unresolved tie drops the name.
func dominantFields(found []fieldCandidate) []Field {
	byName := map[string][]fieldCandidate{}
	var order []string
	for _, cand := range found {
		if _, ok := byName[cand.field.Name]; !ok {
			order = append(order, cand.field.Name)
		}
		byName[cand.field.Name] = append(byName[cand.field.Name], cand)
	}
	out := make([]Field, 0, len(order))
	for _, name := range order {
		if f, ok := dominant(byName[name]); ok {
			out = append(out, f)
		}
	}
	return out
}

func dominant(cands []fieldCandidate) (Field, bool) {
	minDepth := cands[0].depth
	for _, cand := range cands[1:] {
		minDepth = min(minDepth, cand.depth)
	}
	var shallow, tagged []fieldCandidate
	for _, cand := range cands {
		if cand.depth != minDepth {
			continue
		}
		shallow = append(shallow, cand)
		if cand.tagged {
			tagged = append(tagged, cand)
		}
	}
	switch {
	case len(shallow) == 1:
		return shallow[0].field, true
	case len(tagged) == 1:
		return tagged[0].field, true
	}
	return Field{}, false
}

func hasOption(opts, want string) bool {
	for opts != "" {
		var opt string
		opt, opts, _ = strings.Cut(opts, ",")
		if opt == want {
			return true
		}
	}
	return false
}

func quotable(t reflect.Type) bool {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch k := t.Kind(); {
	case k == reflect.String, k == reflect.Bool, isIntegerKind(k), k == reflect.Float32, k == reflect.Float64:
		return true
	}
	return false
}

func isIntegerKind(k reflect.Kind) bool {
	return k >= reflect.Int && k <= reflect.Uintptr
}

func implements(t, iface reflect.Type) bool {
	return t.Implements(iface) || reflect.PointerTo(t).Implements(iface)
}
