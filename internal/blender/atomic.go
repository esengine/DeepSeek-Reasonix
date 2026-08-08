// L3GO-style atomic operation API (docs/MODELLING_AGENT_RESEARCH.md §③):
// instead of letting the agent write raw bpy, expose a small registry of
// deterministic atomic ops (add_cube/add_sphere/add_cylinder/boolean/bevel/
// delete_object). Each op is a parameterized bpy snippet — precise, auditable,
// and far fewer tokens than free-form scripts. RunAtomic resolves the op,
// validates args, and reports a compact JSON result.
package blender

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// AtomicOp is one deterministic modeling operation.
type AtomicOp struct {
	Name string // snake_case op name, e.g. "add_cube"
	Desc string // one-line description for the agent's op registry
	Args []ArgSpec
	// Script receives the validated args and returns bpy code. Implementations
	// quote strings with %q and numeric args as-is.
	Script func(args map[string]any) string
}

// ArgSpec describes one argument.
type ArgSpec struct {
	Name string // argument name
	Type string // "float" | "int" | "string" | "bool"
	Def  any    // default value (nil = required)
	Desc string
}

// AtomicOps is the agent-facing op registry (exported for tool schemas).
var AtomicOps = []AtomicOp{
	{
		Name: "add_cube", Desc: "add a cube mesh (optionally at a position)",
		Args: []ArgSpec{{Name: "size", Type: "float", Def: 1.0, Desc: "edge length"},
			{Name: "x", Type: "float", Def: 0.0, Desc: "position x"},
			{Name: "y", Type: "float", Def: 0.0, Desc: "position y"},
			{Name: "z", Type: "float", Def: 0.0, Desc: "position z"}},
		Script: func(a map[string]any) string {
			s, x, y, z := num(a, "size", 1), num(a, "x", 0), num(a, "y", 0), num(a, "z", 0)
			return fmt.Sprintf(`
import bpy
bpy.ops.mesh.primitive_cube_add(size=%g, location=(%g, %g, %g))
`, s, x, y, z)
		},
	},
	{
		Name: "add_uv_sphere", Desc: "add a UV sphere",
		Args: []ArgSpec{{Name: "radius", Type: "float", Def: 1.0, Desc: "radius"},
			{Name: "segments", Type: "int", Def: 32, Desc: "horizontal segments"},
			{Name: "rings", Type: "int", Def: 16, Desc: "vertical rings"},
			{Name: "x", Type: "float", Def: 0.0, Desc: "position x"},
			{Name: "y", Type: "float", Def: 0.0, Desc: "position y"},
			{Name: "z", Type: "float", Def: 0.0, Desc: "position z"}},
		Script: func(a map[string]any) string {
			r, seg, ring := num(a, "radius", 1), intNum(a, "segments", 32), intNum(a, "rings", 16)
			x, y, z := num(a, "x", 0), num(a, "y", 0), num(a, "z", 0)
			return fmt.Sprintf(`
import bpy
bpy.ops.mesh.primitive_uv_sphere_add(radius=%g, segments=%d, ring_count=%d, location=(%g, %g, %g))
`, r, seg, ring, x, y, z)
		},
	},
	{
		Name: "add_cylinder", Desc: "add a cylinder",
		Args: []ArgSpec{{Name: "radius", Type: "float", Def: 1.0, Desc: "radius"},
			{Name: "depth", Type: "float", Def: 2.0, Desc: "height"},
			{Name: "vertices", Type: "int", Def: 64, Desc: "side vertices"},
			{Name: "x", Type: "float", Def: 0.0, Desc: "position x"},
			{Name: "y", Type: "float", Def: 0.0, Desc: "position y"},
			{Name: "z", Type: "float", Def: 0.0, Desc: "position z"}},
		Script: func(a map[string]any) string {
			r, d, v := num(a, "radius", 1), num(a, "depth", 2), intNum(a, "vertices", 64)
			x, y, z := num(a, "x", 0), num(a, "y", 0), num(a, "z", 0)
			return fmt.Sprintf(`
import bpy
bpy.ops.mesh.primitive_cylinder_add(radius=%g, depth=%g, vertices=%d, location=(%g, %g, %g))
`, r, d, v, x, y, z)
		},
	},
	{
		Name: "boolean", Desc: "boolean-modify the active object with a target (union/difference/intersect)",
		Args: []ArgSpec{{Name: "target", Type: "string", Desc: "target object name"},
			{Name: "operation", Type: "string", Def: "DIFFERENCE", Desc: "UNION|DIFFERENCE|INTERSECT"},
			{Name: "apply", Type: "bool", Def: true, Desc: "apply the modifier"}},
		Script: func(a map[string]any) string {
			target := str(a, "target", "")
			op := strings.ToUpper(str(a, "operation", "DIFFERENCE"))
			apply := boolVal(a, "apply", true)
			app := "bpy.ops.object.modifier_apply(modifier=m.name)"
			if !apply {
				app = ""
			}
			return fmt.Sprintf(`
import bpy
assert %q in bpy.data.objects, "unknown target"
bpy.ops.object.modifier_add(type='BOOLEAN')
m = bpy.context.object.modifiers[-1]
m.object = bpy.data.objects[%q]
m.operation = %q
%s
`, target, target, op, app)
		},
	},
	{
		Name: "bevel", Desc: "bevel the active object's edges",
		Args: []ArgSpec{{Name: "width", Type: "float", Def: 0.05, Desc: "bevel width"},
			{Name: "segments", Type: "int", Def: 1, Desc: "bevel segments"},
			{Name: "apply", Type: "bool", Def: true, Desc: "apply the modifier"}},
		Script: func(a map[string]any) string {
			w, seg := num(a, "width", 0.05), intNum(a, "segments", 1)
			apply := boolVal(a, "apply", true)
			app := "bpy.ops.object.modifier_apply(modifier=m.name)"
			if !apply {
				app = ""
			}
			return fmt.Sprintf(`
import bpy
bpy.ops.object.modifier_add(type='BEVEL')
m = bpy.context.object.modifiers[-1]
m.width = %g
m.segments = %d
%s
`, w, seg, app)
		},
	},
	{
		Name: "delete_object", Desc: "delete an object by name (ignored when missing)",
		Args: []ArgSpec{{Name: "name", Type: "string", Desc: "object name"}},
		Script: func(a map[string]any) string {
			name := str(a, "name", "")
			return fmt.Sprintf(`
import bpy
if %q in bpy.data.objects:
    bpy.data.objects.remove(bpy.data.objects[%q], do_unlink=True)
`, name, name)
		},
	},
}

func num(a map[string]any, k string, def float64) float64 {
	if v, ok := a[k]; ok {
		if f, ok := v.(float64); ok {
			return f
		}
	}
	return def
}

func intNum(a map[string]any, k string, def int) int {
	if v, ok := a[k]; ok {
		switch t := v.(type) {
		case float64:
			return int(t)
		case int:
			return t
		}
	}
	return def
}

func str(a map[string]any, k string, def string) string {
	if v, ok := a[k]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return def
}

func boolVal(a map[string]any, k string, def bool) bool {
	if v, ok := a[k]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return def
}

// FindAtomicOp returns the op by name.
func FindAtomicOp(name string) (AtomicOp, bool) {
	for _, o := range AtomicOps {
		if o.Name == name {
			return o, true
		}
	}
	return AtomicOp{}, false
}

// RunAtomic executes one atomic op against blendPath ("" = default scene).
// Args are validated against the op's spec; unknown args are ignored, missing
// args fall back to defaults. Returns the run result (compact output).
func RunAtomic(ctx context.Context, blendPath, opName string, args map[string]any, save bool, timeout time.Duration) (*Result, error) {
	op, ok := FindAtomicOp(opName)
	if !ok {
		return nil, fmt.Errorf("blender: unknown atomic op %q (registry: %s)", opName, opNames())
	}
	script := op.Script(args)
	return RunScript(ctx, blendPath, script, save, timeout)
}

func opNames() string {
	names := make([]string, 0, len(AtomicOps))
	for _, o := range AtomicOps {
		names = append(names, o.Name)
	}
	return strings.Join(names, ", ")
}

// AtomicOpSchema renders the registry as a JSON schema (for tool wiring).
func AtomicOpSchema() json.RawMessage {
	type opJSON struct {
		Name string     `json:"name"`
		Desc string     `json:"desc"`
		Args []ArgSpec  `json:"args"`
	}
	ops := make([]opJSON, 0, len(AtomicOps))
	for _, o := range AtomicOps {
		ops = append(ops, opJSON{Name: o.Name, Desc: o.Desc, Args: o.Args})
	}
	b, _ := json.Marshal(ops)
	return b
}
