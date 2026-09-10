package hostrpc

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

type shapeBase struct {
	ID     string `json:"id"`
	Shadow string `json:"shadow"`
	_      int
}

type shapeLevel int

func (l shapeLevel) MarshalText() ([]byte, error) { return []byte("level"), nil }

type shapeNode struct {
	Children []shapeNode `json:"children"`
}

type shapeDTO struct {
	shapeBase
	Shadow   int                 `json:"shadow"`
	When     time.Time           `json:"when"`
	Raw      json.RawMessage     `json:"raw"`
	Blob     []byte              `json:"blob"`
	Digits   [4]byte             `json:"digits"`
	Any      any                 `json:"any"`
	Ptr      *shapeBase          `json:"ptr"`
	Opt      string              `json:"opt,omitempty"`
	Zero     int                 `json:"zero,omitzero"`
	Quoted   int64               `json:"quoted,string"`
	ByName   map[string]int      `json:"byName"`
	ByID     map[int]string      `json:"byId"`
	Level    shapeLevel          `json:"level"`
	Levels   map[shapeLevel]bool `json:"levels"`
	Inline   struct{ X float64 } `json:"inline"`
	Tree     shapeNode           `json:"tree"`
	Untagged bool
	Skipped  string `json:"-"`
	_        string
}

type shapeAmbiguousA struct{ Name string }
type shapeAmbiguousB struct{ Name string }
type shapeAmbiguous struct {
	shapeAmbiguousA
	shapeAmbiguousB
	Keep string `json:"keep"`
}

type shapeBadKey struct {
	M map[float64]string
}

type shapeBadField struct {
	F chan int
}

func describe(t *testing.T, typ reflect.Type) (TypeRef, map[string]ObjectType) {
	t.Helper()
	c := newTypeCollector()
	ref, err := c.ref(typ, "hint")
	if err != nil {
		t.Fatal(err)
	}
	return ref, c.types
}

func fieldByName(t *testing.T, obj ObjectType, name string) Field {
	t.Helper()
	for _, f := range obj.Fields {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("field %q missing from %+v", name, obj.Fields)
	return Field{}
}

func TestTypeRefFollowsEncodingJSONRules(t *testing.T) {
	ref, types := describe(t, reflect.TypeFor[shapeDTO]())
	if ref.Kind != KindObject || ref.Ref != "hostrpc.shapeDTO" {
		t.Fatalf("ref = %+v", ref)
	}
	dto := types["hostrpc.shapeDTO"]
	want := map[string]TypeRef{
		"id":       {Kind: KindString},
		"shadow":   {Kind: KindInteger},
		"when":     {Kind: KindString},
		"raw":      {Kind: KindAny},
		"blob":     {Kind: KindString},
		"digits":   {Kind: KindArray, Elem: &TypeRef{Kind: KindInteger}},
		"any":      {Kind: KindAny},
		"ptr":      {Kind: KindNullable, Elem: &TypeRef{Kind: KindObject, Ref: "hostrpc.shapeBase"}},
		"opt":      {Kind: KindString},
		"zero":     {Kind: KindInteger},
		"quoted":   {Kind: KindString},
		"byName":   {Kind: KindMap, Key: &TypeRef{Kind: KindString}, Elem: &TypeRef{Kind: KindInteger}},
		"byId":     {Kind: KindMap, Key: &TypeRef{Kind: KindInteger}, Elem: &TypeRef{Kind: KindString}},
		"level":    {Kind: KindString},
		"levels":   {Kind: KindMap, Key: &TypeRef{Kind: KindString}, Elem: &TypeRef{Kind: KindBoolean}},
		"inline":   {Kind: KindObject, Ref: "hostrpc.shapeDTO.Inline"},
		"tree":     {Kind: KindObject, Ref: "hostrpc.shapeNode"},
		"Untagged": {Kind: KindBoolean},
	}
	for name, wantRef := range want {
		if got := fieldByName(t, dto, name).Type; !reflect.DeepEqual(got, wantRef) {
			t.Errorf("%s = %+v, want %+v", name, got, wantRef)
		}
	}
	if len(dto.Fields) != len(want) {
		t.Fatalf("got %d fields, want %d: %+v", len(dto.Fields), len(want), dto.Fields)
	}
	for _, name := range []string{"opt", "zero", "ptr"} {
		if !fieldByName(t, dto, name).Optional {
			t.Errorf("%s must be optional", name)
		}
	}
	if fieldByName(t, dto, "id").Optional || fieldByName(t, dto, "when").Optional {
		t.Error("required members must not be optional")
	}
	if dto.Fields[0].Name != "id" || dto.Fields[1].Name != "shadow" {
		t.Fatalf("embedded members must keep declaration order: %+v", dto.Fields[:3])
	}
	if _, ok := types["hostrpc.shapeBase"]; !ok {
		t.Fatal("embedded struct reached through a pointer must be collected")
	}
	node := types["hostrpc.shapeNode"]
	if got := fieldByName(t, node, "children").Type; got.Kind != KindArray || got.Elem.Ref != "hostrpc.shapeNode" {
		t.Fatalf("recursive type = %+v", got)
	}
	if inline := types["hostrpc.shapeDTO.Inline"]; len(inline.Fields) != 1 || inline.Fields[0].Name != "X" {
		t.Fatalf("anonymous struct = %+v", inline)
	}
}

func TestTypeRefDropsAmbiguousEmbeddedMembers(t *testing.T) {
	_, types := describe(t, reflect.TypeFor[shapeAmbiguous]())
	obj := types["hostrpc.shapeAmbiguous"]
	if len(obj.Fields) != 1 || obj.Fields[0].Name != "keep" {
		t.Fatalf("fields = %+v", obj.Fields)
	}
}

func TestTypeRefRejectsUnserialisableShapes(t *testing.T) {
	for _, typ := range []reflect.Type{
		reflect.TypeFor[shapeBadKey](),
		reflect.TypeFor[shapeBadField](),
		reflect.TypeFor[func()](),
		reflect.TypeFor[complex128](),
	} {
		if _, err := newTypeCollector().ref(typ, "hint"); err == nil {
			t.Errorf("%s must be rejected", typ)
		}
	}
}

func TestTypeRefNamesAnonymousStructsFromHint(t *testing.T) {
	ref, types := describe(t, reflect.TypeFor[struct{ A string }]())
	if ref.Ref != "hint" {
		t.Fatalf("ref = %+v", ref)
	}
	if _, ok := types["hint"]; !ok {
		t.Fatalf("types = %v", types)
	}
}
