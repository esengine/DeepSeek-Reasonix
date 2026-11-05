package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"reasonix/internal/contract/config"
)

// Class says who may change a setting. It has no zero value: an entry that
// does not name one fails registration.
type Class int

const (
	// Preference may be proposed by the model and applied on the user's click.
	Preference Class = iota + 1
	// Elevated may be proposed too, but no automatic answer stands in for the click.
	Elevated
	// Guarded is never written on the model's say-so; the model can only point at it.
	Guarded
)

// Effect is when a plain write of the user file reaches the host: at once, at
// the next turn, when a session is next opened, or after a restart.
type Effect int

const (
	Immediate Effect = iota + 1
	NextTurn
	OnOpen
	Restart
)

// Entry declares one user-file key, addressed by its dotted toml path.
type Entry struct {
	ID     string
	Class  Class
	Effect Effect
	// Secret keys are never read back, to the model or to a card.
	Secret bool

	// Via is the config's own setter, which validates and normalises.
	Via func(cfg *config.Config, value any) error
	// Min and Max bound a number when Bounded is set.
	Min, Max float64
	Bounded  bool
	// Free says any value of the key's shape is legal.
	Free bool
}

// Shape is the kind of value a key holds, read from the Config type.
type Shape int

const (
	ShapeBool Shape = iota + 1
	ShapeInt
	ShapeFloat
	ShapeString
	ShapeStringList
	// ShapeStructured values (maps, tables, lists of tables) are edited in their own screen.
	ShapeStructured
)

const (
	CodeUnknown    = "setting.unknown"
	CodeGuarded    = "setting.guarded"
	CodeSecret     = "setting.secret"
	CodeShape      = "setting.shape_unsupported"
	CodeValue      = "setting.invalid_value"
	CodeRange      = "setting.out_of_range"
	codeUndeclared = "setting.undeclared"
)

// Error carries the identity a caller classifies on.
type Error struct {
	Code string
	ID   string
	msg  string
}

func (e *Error) Error() string { return e.Code + ": " + e.ID + ": " + e.msg }

// Message is the sentence without the code, for a caller that carries the code itself.
func (e *Error) Message() string { return e.msg }

// CodeOf is the code of a settings refusal, or "" for any other error.
func CodeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

type field struct {
	index []int
	shape Shape
}

var (
	entries = map[string]Entry{}
	fields  = map[string]field{}
)

func init() {
	if err := register(table, reflect.TypeFor[config.Config]()); err != nil {
		panic(err)
	}
}

func register(rows []Entry, root reflect.Type) error {
	fs := map[string]field{}
	walk(root, "", nil, fs)
	es := map[string]Entry{}
	for _, e := range rows {
		if e.Class < Preference || e.Class > Guarded {
			return fmt.Errorf("setting %q has no class", e.ID)
		}
		if e.Effect < Immediate || e.Effect > Restart {
			return fmt.Errorf("setting %q has no effect timing", e.ID)
		}
		if _, dup := es[e.ID]; dup {
			return fmt.Errorf("setting %q is declared twice", e.ID)
		}
		if _, ok := fs[e.ID]; !ok {
			return fmt.Errorf("setting %q is not a key of the user config", e.ID)
		}
		c := constraints[e.ID]
		e.Via, e.Min, e.Max, e.Bounded, e.Free = c.Via, c.Min, c.Max, c.Bounded, c.Free
		es[e.ID] = e
	}
	for id := range fs {
		if _, ok := es[id]; !ok {
			return &Error{Code: codeUndeclared, ID: id, msg: "a config key with no declared class"}
		}
	}
	entries, fields = es, fs
	return nil
}

func walk(t reflect.Type, prefix string, index []int, out map[string]field) {
	for i := range t.NumField() {
		f := t.Field(i)
		name := strings.Split(f.Tag.Get("toml"), ",")[0]
		if f.Anonymous && f.Type.Kind() == reflect.Struct && f.Tag.Get("toml") == "" {
			walk(f.Type, prefix, append(append([]int(nil), index...), i), out)
			continue
		}
		if !f.IsExported() || name == "" || name == "-" {
			continue
		}
		id := name
		if prefix != "" {
			id = prefix + "." + name
		}
		at := append(append([]int(nil), index...), i)
		if f.Type.Kind() == reflect.Struct {
			walk(f.Type, id, at, out)
			continue
		}
		out[id] = field{index: at, shape: shapeOf(f.Type)}
	}
}

func shapeOf(t reflect.Type) Shape {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Bool:
		return ShapeBool
	case reflect.Int, reflect.Int64:
		return ShapeInt
	case reflect.Float64:
		return ShapeFloat
	case reflect.String:
		return ShapeString
	case reflect.Slice:
		if t.Elem().Kind() == reflect.String {
			return ShapeStringList
		}
	}
	return ShapeStructured
}

// Lookup returns the declaration of a key.
func Lookup(id string) (Entry, bool) {
	e, ok := entries[id]
	return e, ok
}

// ShapeOf returns the kind of value a declared key holds.
func ShapeOf(id string) Shape { return fields[id].shape }

// All lists every declared key in id order.
func All() []Entry {
	out := make([]Entry, 0, len(entries))
	for _, e := range entries {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func locate(cfg *config.Config, id string) (reflect.Value, Entry, error) {
	e, ok := entries[id]
	if !ok {
		return reflect.Value{}, Entry{}, &Error{Code: CodeUnknown, ID: id, msg: "no such setting"}
	}
	return reflect.ValueOf(cfg).Elem().FieldByIndex(fields[id].index), e, nil
}

// Read returns the current value of a key as it would be written in JSON.
func Read(cfg *config.Config, id string) (any, error) {
	v, e, err := locate(cfg, id)
	if err != nil {
		return nil, err
	}
	if e.Secret {
		return nil, &Error{Code: CodeSecret, ID: id, msg: "the value is never shown"}
	}
	if ShapeOf(id) == ShapeStructured {
		return nil, &Error{Code: CodeShape, ID: id, msg: "edited in its own screen"}
	}
	return plain(v), nil
}

// plain is the value a key holds, with an optional key's pointer looked through.
func plain(v reflect.Value) any {
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	return v.Interface()
}

// Write sets a key on cfg from a JSON value and returns what it held before. It
// refuses a Guarded key, so the one gate that keeps the model out of its own
// boundary is here and not in each caller. Neither class nor value is judged
// further: the click that consents belongs to the controller.
func Write(cfg *config.Config, id string, raw json.RawMessage) (from, to any, err error) {
	v, e, err := locate(cfg, id)
	if err != nil {
		return nil, nil, err
	}
	if e.Class == Guarded {
		return nil, nil, &Error{Code: CodeGuarded, ID: id, msg: "a boundary setting is changed by the user, not proposed"}
	}
	if ShapeOf(id) == ShapeStructured {
		return nil, nil, &Error{Code: CodeShape, ID: id, msg: "edited in its own screen"}
	}
	next := reflect.New(v.Type())
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, next.Interface()) != nil {
		return nil, nil, &Error{Code: CodeValue, ID: id, msg: "the value does not fit " + v.Type().String()}
	}
	want := plain(next.Elem())
	if err := e.check(id, want); err != nil {
		return nil, nil, err
	}
	from = plain(v)
	if e.Via != nil {
		if err := e.Via(cfg, want); err != nil {
			return nil, nil, &Error{Code: CodeValue, ID: id, msg: err.Error()}
		}
		return from, plain(v), nil
	}
	v.Set(next.Elem())
	return from, plain(v), nil
}

func (e Entry) check(id string, value any) error {
	if !e.Bounded {
		return nil
	}
	var n float64
	switch x := value.(type) {
	case int:
		n = float64(x)
	case int64:
		n = float64(x)
	case float64:
		n = x
	default:
		return nil
	}
	if n < e.Min || n > e.Max {
		return &Error{Code: CodeRange, ID: id, msg: fmt.Sprintf("%v is outside %g to %g", n, e.Min, e.Max)}
	}
	return nil
}

// Changed lists, in id order, the plain keys whose value on cfg differs from def.
// A secret or structured key is never named: its value cannot be shown here.
func Changed(cfg, def *config.Config) []string {
	var out []string
	for _, e := range All() {
		a, errA := Read(cfg, e.ID)
		b, errB := Read(def, e.ID)
		if errA != nil || errB != nil || reflect.DeepEqual(a, b) {
			continue
		}
		out = append(out, e.ID)
	}
	return out
}
