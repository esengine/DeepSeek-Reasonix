package main

import (
	"fmt"
	"go/ast"
)

// Each independent scalar multiplies the states a guarded type can be in, and
// nothing records which combinations are legal; grouping them by lifetime costs
// one field and removes the whole product. Only types owning a mutex or atomic
// are counted — a struct without one is not concurrently mutated state, and
// counting config or DTOs would bury this finding under a translation table.
const maxScalarFields = 12

var scalarBasicTypes = map[string]bool{
	"bool": true, "string": true, "byte": true, "rune": true,
	"int": true, "int8": true, "int16": true, "int32": true, "int64": true,
	"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true,
	"uintptr": true, "float32": true, "float64": true,
}

// atomic.Bool and friends are scalars whose concurrency contract is per-field,
// which is exactly the combination problem this counts.
var scalarQualifiedTypes = map[string]bool{
	"atomic.Bool": true, "atomic.Int32": true, "atomic.Int64": true,
	"atomic.Uint32": true, "atomic.Uint64": true, "atomic.Pointer": true,
	"atomic.Value": true, "time.Duration": true, "time.Time": true,
}

// guardsConcurrentState reports whether the type owns a synchronisation
// primitive, which is what separates state several goroutines reach from a
// record that merely has fields.
func guardsConcurrentState(st *ast.StructType) bool {
	if st.Fields == nil {
		return false
	}
	for _, field := range st.Fields.List {
		sel, ok := unwrapPointer(field.Type).(*ast.SelectorExpr)
		if !ok {
			continue
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok {
			continue
		}
		switch pkg.Name + "." + sel.Sel.Name {
		case "sync.Mutex", "sync.RWMutex":
			return true
		}
		if pkg.Name == "atomic" {
			return true
		}
	}
	return false
}

func unwrapPointer(expr ast.Expr) ast.Expr {
	if star, ok := expr.(*ast.StarExpr); ok {
		return star.X
	}
	return expr
}

func scalarFieldCount(st *ast.StructType) int {
	if st.Fields == nil {
		return 0
	}
	total := 0
	for _, field := range st.Fields.List {
		if !isScalarType(field.Type) {
			continue
		}
		// An embedded scalar still occupies one slot in the product.
		total += max(len(field.Names), 1)
	}
	return total
}

func isScalarType(expr ast.Expr) bool {
	switch t := expr.(type) {
	case *ast.Ident:
		return scalarBasicTypes[t.Name]
	case *ast.SelectorExpr:
		pkg, ok := t.X.(*ast.Ident)
		if !ok {
			return false
		}
		return scalarQualifiedTypes[pkg.Name+"."+t.Sel.Name]
	case *ast.IndexExpr: // atomic.Pointer[T]
		return isScalarType(t.X)
	}
	return false
}

func checkStructState(s *sourceFile) []Finding {
	if s.isTest() {
		return nil
	}
	var out []Finding
	ast.Inspect(s.file, func(n ast.Node) bool {
		spec, ok := n.(*ast.TypeSpec)
		if !ok {
			return true
		}
		st, ok := spec.Type.(*ast.StructType)
		if !ok {
			return true
		}
		if !guardsConcurrentState(st) {
			return false
		}
		if n := scalarFieldCount(st); n > maxScalarFields {
			out = append(out, Finding{s.rel, s.line(spec.Pos()), ruleStructState,
				fmt.Sprintf("%s carries %d scalar state fields, over the %d ceiling; group them by lifetime",
					spec.Name.Name, n, maxScalarFields),
				n - maxScalarFields})
		}
		return false
	})
	return out
}
