package hostrpc

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type fixtureItem struct {
	ID    string       `json:"id"`
	Note  string       `json:"note,omitempty"`
	Next  *fixtureItem `json:"next"`
	Count int          `json:"-"`
}

type fixtureTarget struct {
	calls []string
	fail  bool
	empty bool
}

func (f *fixtureTarget) Void()            { f.calls = append(f.calls, "Void") }
func (f *fixtureTarget) Platform() string { return "test-os" }
func (f *fixtureTarget) Fail() error      { return errors.New("boom") }
func (f *fixtureTarget) Ping(id string, n int) (fixtureItem, error) {
	f.calls = append(f.calls, "Ping:"+id)
	if f.fail {
		return fixtureItem{}, errors.New("ping failed: " + id)
	}
	return fixtureItem{ID: id, Count: n}, nil
}
func (f *fixtureTarget) Items() []fixtureItem {
	if f.empty {
		return []fixtureItem{}
	}
	return nil
}
func (f *fixtureTarget) Explode()                            { panic("kaboom") }
func (f *fixtureTarget) Hidden(map[string]any)               {}
func (f *fixtureTarget) Update(item fixtureItem) fixtureItem { return item }

type badResults struct{}

func (badResults) Three() (int, int, error) { return 0, 0, nil }

type badOrder struct{}

func (badOrder) Pair() (int, string) { return 0, "" }

type badParam struct{}

func (badParam) Ctx(string, context.Context) {}

type badVariadic struct{}

func (badVariadic) Many(...string) {}

type badFunc struct{}

func (badFunc) Callback(func()) {}

type notStruct int

func (notStruct) M() {}

func mustRegistry(t *testing.T, target any, skip Skip) *Registry {
	t.Helper()
	r, err := NewRegistry(target, skip)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRegistryDescribesAcceptedSignatures(t *testing.T) {
	r := mustRegistry(t, &fixtureTarget{}, Skip{"Hidden": true})
	got := map[string]Command{}
	for _, cmd := range r.Commands() {
		got[cmd.Name] = cmd
	}
	if _, ok := got["Hidden"]; ok {
		t.Fatal("skipped method appeared in the contract")
	}
	if cmd := got["Void"]; cmd.Result != nil || cmd.ReturnsError || len(cmd.Params) != 0 {
		t.Fatalf("Void = %+v", cmd)
	}
	if cmd := got["Platform"]; cmd.Result == nil || cmd.Result.Kind != KindString || cmd.ReturnsError {
		t.Fatalf("Platform = %+v", cmd)
	}
	if cmd := got["Fail"]; cmd.Result != nil || !cmd.ReturnsError {
		t.Fatalf("Fail = %+v", cmd)
	}
	ping := got["Ping"]
	if !ping.ReturnsError || ping.Result == nil || ping.Result.Kind != KindObject || ping.Result.Ref != "hostrpc.fixtureItem" {
		t.Fatalf("Ping = %+v", ping)
	}
	if len(ping.Params) != 2 || ping.Params[0].Kind != KindString || ping.Params[1].Kind != KindInteger {
		t.Fatalf("Ping params = %+v", ping.Params)
	}
	if items := got["Items"]; items.Result.Kind != KindArray || items.Result.Elem.Ref != "hostrpc.fixtureItem" {
		t.Fatalf("Items = %+v", items)
	}
	names := make([]string, 0, len(r.Commands()))
	for _, cmd := range r.Commands() {
		names = append(names, cmd.Name)
	}
	if strings.Join(names, ",") != "Explode,Fail,Items,Ping,Platform,Update,Void" {
		t.Fatalf("commands not sorted by name: %v", names)
	}
}

func TestRegistryRejectsUnsupportedSignatures(t *testing.T) {
	cases := []struct {
		target any
		want   string
	}{
		{&badResults{}, "badResults.Three: 3 results"},
		{&badOrder{}, "badOrder.Pair: results must be (T, error), got (int, string)"},
		{&badParam{}, "badParam.Ctx: parameter 1: context.Context cannot be decoded"},
		{&badVariadic{}, "badVariadic.Many: variadic"},
		{&badFunc{}, "badFunc.Callback: parameter 0: func() is not JSON-serialisable"},
		{new(notStruct), "must be a pointer to a struct"},
		{fixtureTarget{}, "must be a pointer to a struct"},
		{nil, "nil target"},
	}
	for _, tc := range cases {
		_, err := NewRegistry(tc.target, nil)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("NewRegistry(%T) error = %v, want substring %q", tc.target, err, tc.want)
		}
	}
}

func raw(values ...string) []json.RawMessage {
	out := make([]json.RawMessage, 0, len(values))
	for _, v := range values {
		out = append(out, json.RawMessage(v))
	}
	return out
}

func TestRegistryInvokeMarshalsArgumentsAndResults(t *testing.T) {
	target := &fixtureTarget{}
	r := mustRegistry(t, target, nil)
	ctx := context.Background()

	result, err := r.Invoke(ctx, "Void", nil)
	if err != nil || result != nil {
		t.Fatalf("Void = %v, %v", result, err)
	}
	result, err = r.Invoke(ctx, "Ping", raw(`"alpha"`, `7`))
	if err != nil {
		t.Fatal(err)
	}
	if item, ok := result.(fixtureItem); !ok || item.ID != "alpha" || item.Count != 7 {
		t.Fatalf("Ping result = %#v", result)
	}
	if _, err := r.Invoke(ctx, "Ping", raw(`"missing"`)); err != nil {
		t.Fatalf("missing trailing argument must decode as zero: %v", err)
	}
	if _, err := r.Invoke(ctx, "Ping", raw(`"a"`, `1`, `2`)); err == nil {
		t.Fatal("extra argument must be rejected")
	}
	var invalid *InvalidArgsError
	if _, err := r.Invoke(ctx, "Ping", raw(`42`)); !errors.As(err, &invalid) {
		t.Fatalf("type mismatch error = %v", err)
	}
	updated, err := r.Invoke(ctx, "Update", raw(`{"id":"x","next":{"id":"y","next":null}}`))
	if err != nil || updated.(fixtureItem).Next == nil || updated.(fixtureItem).Next.ID != "y" {
		t.Fatalf("Update = %#v, %v", updated, err)
	}
	if strings.Join(target.calls, " ") != "Void Ping:alpha Ping:missing" {
		t.Fatalf("calls = %v", target.calls)
	}
}

func TestRegistryInvokeReportsErrorsAndUnknownMethods(t *testing.T) {
	target := &fixtureTarget{fail: true}
	r := mustRegistry(t, target, Skip{"Hidden": true})
	ctx := context.Background()

	if _, err := r.Invoke(ctx, "Fail", nil); err == nil || err.Error() != "boom" {
		t.Fatalf("Fail error = %v", err)
	}
	if _, err := r.Invoke(ctx, "Ping", raw(`"z"`)); err == nil || err.Error() != "ping failed: z" {
		t.Fatalf("Ping error = %v", err)
	}
	var unknown *UnknownMethodError
	if _, err := r.Invoke(ctx, "Nope", nil); !errors.As(err, &unknown) || unknown.Method != "Nope" {
		t.Fatalf("unknown method error = %v", err)
	}
	if _, err := r.Invoke(ctx, "Hidden", raw(`{}`)); !errors.As(err, &unknown) {
		t.Fatalf("skipped method must be unknown at invoke: %v", err)
	}
	var panicked *PanicError
	if _, err := r.Invoke(ctx, "Explode", nil); !errors.As(err, &panicked) || panicked.Method != "Explode" {
		t.Fatalf("panic error = %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := r.Invoke(cancelled, "Void", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context error = %v", err)
	}
}

func TestRegistryInvokeKeepsNilAndEmptySlicesDistinct(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		empty bool
		want  string
	}{{false, "null"}, {true, "[]"}} {
		r := mustRegistry(t, &fixtureTarget{empty: tc.empty}, nil)
		result, err := r.Invoke(ctx, "Items", nil)
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(result)
		if string(encoded) != tc.want {
			t.Fatalf("Items(empty=%v) encodes as %s, want %s", tc.empty, encoded, tc.want)
		}
	}
}

func TestRegistryWithoutInstanceDescribesButCannotInvoke(t *testing.T) {
	r := mustRegistry(t, (*fixtureTarget)(nil), nil)
	if len(r.Commands()) == 0 {
		t.Fatal("typed nil target must still describe the contract")
	}
	if _, err := r.Invoke(context.Background(), "Void", nil); err == nil {
		t.Fatal("invoke on a nil target must fail")
	}
}
