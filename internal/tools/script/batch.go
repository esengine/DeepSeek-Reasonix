package script

import (
	"fmt"

	"go.starlark.net/starlark"

	"reasonix/internal/contract/tool"
)

// callMany is call_many([{"name": ..., "args": {...}}, ...]): one batch the host
// schedules, concurrently only where a model's own parallel calls could run.
func (r *run) callMany(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var list *starlark.List
	if err := starlark.UnpackArgs(fn.Name(), args, kwargs, "calls", &list); err != nil {
		return nil, err
	}
	batcher, ok := r.inv.(tool.BatchInvoker)
	if !ok {
		return nil, fmt.Errorf("%s: this host does not schedule batches; use call or try_call", fn.Name())
	}
	calls := make([]tool.NestedCall, 0, list.Len())
	for i := range list.Len() {
		entry, ok := list.Index(i).(*starlark.Dict)
		if !ok {
			return nil, fmt.Errorf("%s: item %d must be a dict with name and args", fn.Name(), i)
		}
		name, _, _ := entry.Get(starlark.String("name"))
		payload, found, _ := entry.Get(starlark.String("args"))
		if name == nil {
			return nil, fmt.Errorf("%s: item %d has no name", fn.Name(), i)
		}
		if !found {
			payload = starlark.NewDict(0)
		}
		c, err := r.encode(thread, fn, name, payload)
		if err != nil {
			return nil, err
		}
		calls = append(calls, c)
	}
	if err := r.count(fn, calls); err != nil {
		return nil, err
	}
	results := batcher.InvokeMany(r.ctx, calls)
	out := make([]starlark.Value, len(results))
	for i, res := range results {
		v, err := resultStruct(res)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return starlark.NewList(out), nil
}
