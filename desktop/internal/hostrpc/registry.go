package hostrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"runtime/debug"
	"slices"
)

// Command is one exported method of the bound target as the shell calls it.
type Command struct {
	CommandOwnership
	Name         string    `json:"name"`
	Params       []TypeRef `json:"params"`
	Result       *TypeRef  `json:"result,omitempty"`
	ReturnsError bool      `json:"returnsError,omitempty"`
	Cancellation string    `json:"cancellation"`
}

// Skip names exported methods the registry leaves out of the contract.
type Skip map[string]bool

// UnknownMethodError reports an invoke of a name the registry never accepted.
type UnknownMethodError struct{ Method string }

func (e *UnknownMethodError) Error() string { return "unknown method: " + e.Method }

// InvalidArgsError reports arguments the method cannot take.
type InvalidArgsError struct {
	Method string
	Reason string
}

func (e *InvalidArgsError) Error() string { return e.Method + ": " + e.Reason }

// PanicError carries a panic raised inside a bound method so one faulty call
// cannot take the whole service down.
type PanicError struct {
	Method string
	Value  any
	Stack  []byte
}

func (e *PanicError) Error() string { return fmt.Sprintf("%s: panic: %v", e.Method, e.Value) }

type boundMethod struct {
	fn      reflect.Value
	params  []reflect.Type
	result  bool
	errAt   int
	context bool
}

// Registry is the set of methods the shell may invoke on one target value.
type Registry struct {
	target   reflect.Value
	commands []Command
	methods  map[string]boundMethod
	types    map[string]ObjectType
}

// NewRegistry reflects over the exported methods of target, which must be a
// pointer to a struct. A typed nil pointer yields a registry that can
// describe the contract but not invoke. Any method whose signature the shell
// could not call fails the whole registry so the surface never drifts
// silently; skip excludes methods by name before that check.
func NewRegistry(target any, skip Skip) (*Registry, error) {
	return newRegistry(target, skip, nil)
}

// NewRegistryWithOwners requires source-derived ownership for every bound
// command. Metadata describes the existing owner; it does not grant access.
func NewRegistryWithOwners(target any, skip Skip, owners map[string]CommandOwnership) (*Registry, error) {
	if owners == nil {
		return nil, errors.New("hostrpc: ownership metadata is required")
	}
	return newRegistry(target, skip, owners)
}

func newRegistry(target any, skip Skip, owners map[string]CommandOwnership) (*Registry, error) {
	if target == nil {
		return nil, errors.New("hostrpc: nil target")
	}
	rt := reflect.TypeOf(target)
	if rt.Kind() != reflect.Pointer || rt.Elem().Kind() != reflect.Struct {
		return nil, fmt.Errorf("hostrpc: target must be a pointer to a struct, got %s", rt)
	}
	collector := newTypeCollector()
	r := &Registry{
		target:   reflect.ValueOf(target),
		commands: []Command{},
		methods:  map[string]boundMethod{},
	}
	owner := rt.Elem().Name()
	for i := range rt.NumMethod() {
		m := rt.Method(i)
		if skip[m.Name] {
			continue
		}
		cmd, bound, err := describeMethod(collector, owner, m)
		if err != nil {
			return nil, fmt.Errorf("hostrpc: %s.%s: %w", owner, m.Name, err)
		}
		if owners != nil {
			metadata, ok := owners[m.Name]
			if !ok || metadata.Owner != owner+"."+m.Name || metadata.Domain == "" || len(metadata.Sources) == 0 || metadata.Scope.Resolver != metadata.Owner || len(metadata.Scope.Inputs) != len(cmd.Params) {
				return nil, fmt.Errorf("hostrpc: %s.%s: missing or invalid ownership metadata", owner, m.Name)
			}
			kind := "owner-inputs"
			if len(cmd.Params) == 0 {
				kind = "owner-state"
			}
			if metadata.Scope.Kind != kind {
				return nil, fmt.Errorf("hostrpc: %s.%s: invalid scope kind", owner, m.Name)
			}
			cmd.CommandOwnership = metadata
		}
		r.commands = append(r.commands, cmd)
		r.methods[m.Name] = bound
	}
	r.types = collector.types
	return r, nil
}

func describeMethod(c *typeCollector, owner string, m reflect.Method) (Command, boundMethod, error) {
	ft := m.Type
	if ft.IsVariadic() {
		return Command{}, boundMethod{}, errors.New("variadic parameters are not supported")
	}
	cmd := Command{Name: m.Name, Params: []TypeRef{}, Cancellation: "before-dispatch"}
	bound := boundMethod{fn: m.Func, errAt: -1}
	first := 1
	if ft.NumIn() > 1 && ft.In(1) == reflect.TypeFor[context.Context]() {
		bound.context, first, cmd.Cancellation = true, 2, "cooperative-context"
	}
	for i := first; i < ft.NumIn(); i++ {
		pt := ft.In(i)
		if pt.Kind() == reflect.Interface && pt.NumMethod() > 0 {
			return Command{}, boundMethod{}, fmt.Errorf("parameter %d: %s cannot be decoded from JSON", i-first, pt)
		}
		ref, err := c.ref(pt, fmt.Sprintf("%s.%s.arg%d", owner, m.Name, i-first))
		if err != nil {
			return Command{}, boundMethod{}, fmt.Errorf("parameter %d: %w", i-first, err)
		}
		cmd.Params = append(cmd.Params, ref)
		bound.params = append(bound.params, pt)
	}
	switch ft.NumOut() {
	case 0:
	case 1:
		if ft.Out(0) == errorType {
			cmd.ReturnsError, bound.errAt = true, 0
			break
		}
		if err := describeResult(c, owner, m.Name, ft.Out(0), &cmd, &bound); err != nil {
			return Command{}, boundMethod{}, err
		}
	case 2:
		if ft.Out(1) != errorType || ft.Out(0) == errorType {
			return Command{}, boundMethod{}, fmt.Errorf("results must be (T, error), got (%s, %s)", ft.Out(0), ft.Out(1))
		}
		if err := describeResult(c, owner, m.Name, ft.Out(0), &cmd, &bound); err != nil {
			return Command{}, boundMethod{}, err
		}
		cmd.ReturnsError, bound.errAt = true, 1
	default:
		return Command{}, boundMethod{}, fmt.Errorf("%d results; want (), (T), (error) or (T, error)", ft.NumOut())
	}
	return cmd, bound, nil
}

func describeResult(c *typeCollector, owner, method string, t reflect.Type, cmd *Command, bound *boundMethod) error {
	ref, err := c.ref(t, owner+"."+method+".result")
	if err != nil {
		return fmt.Errorf("result: %w", err)
	}
	cmd.Result = &ref
	bound.result = true
	return nil
}

// Commands lists the accepted methods sorted by name; never nil.
func (r *Registry) Commands() []Command { return slices.Clone(r.commands) }

// Invoke decodes args into the method's parameters, calls it, and returns
// its result (nil for void) or its error. Missing trailing arguments decode
// as zero values, matching the retired shell's tolerance; extra ones are rejected.
func (r *Registry) Invoke(ctx context.Context, name string, args []json.RawMessage) (result any, err error) {
	m, ok := r.methods[name]
	if !ok {
		return nil, &UnknownMethodError{Method: name}
	}
	if r.target.IsNil() {
		return nil, errors.New("hostrpc: registry has no target instance")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(args) > len(m.params) {
		return nil, &InvalidArgsError{Method: name, Reason: fmt.Sprintf("expected at most %d arguments, got %d", len(m.params), len(args))}
	}
	in := make([]reflect.Value, 0, len(m.params)+1)
	in = append(in, r.target)
	if m.context {
		in = append(in, reflect.ValueOf(ctx))
	}
	for i, pt := range m.params {
		v := reflect.New(pt)
		if i < len(args) && len(bytes.TrimSpace(args[i])) > 0 {
			if err := json.Unmarshal(args[i], v.Interface()); err != nil {
				return nil, &InvalidArgsError{Method: name, Reason: fmt.Sprintf("argument %d: %v", i, err)}
			}
		}
		in = append(in, v.Elem())
	}
	defer func() {
		if rec := recover(); rec != nil {
			result, err = nil, &PanicError{Method: name, Value: rec, Stack: debug.Stack()}
		}
	}()
	// Decoders may take time or trigger cancellation. This is the final
	// cancellation boundary for synchronous methods; never discard their
	// result after dispatch, since a write may already have committed.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := m.fn.Call(in)
	if m.errAt >= 0 {
		if callErr, _ := out[m.errAt].Interface().(error); callErr != nil {
			return nil, callErr
		}
	}
	if m.result {
		return out[0].Interface(), nil
	}
	return nil, nil
}
