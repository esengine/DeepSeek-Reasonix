// document.go — the canonical parse: what the author actually wrote, with the
// shape they wrote it in. New semantics read from here. The flat map Split
// returns is a compatibility view derived from this and never the other way
// round, because flattening drops the one fact a namespace is made of — which
// key a field was written under.
package frontmatter

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// Kind classifies a frontmatter value's shape. A key written with no value at
// all is a scalar whose text is empty: the key is still present in Fields,
// which is what "the author reached for this namespace" is read from.
type Kind int

const (
	KindScalar Kind = iota
	KindSequence
	KindMapping
)

// Value is one frontmatter value. Only the member matching Kind is populated;
// an empty sequence and an empty mapping are distinct from each other and from
// an absent field, which is a distinction a flattened view cannot make.
type Value struct {
	Kind   Kind
	Scalar string
	Items  []Value
	Fields []Field
}

// Field is one key and its value at one level, in document order.
type Field struct {
	Key   string
	Value Value
}

// Document is a parsed frontmatter block.
type Document struct {
	Fields []Field
}

// Parse separates an optional leading ---fenced block from the body and returns
// the block as written. Fence handling matches Split exactly: an unopened or
// unclosed fence makes the whole input the body.
func Parse(s string) (Document, string) {
	raw, body, ok := splitRaw(s)
	if !ok || strings.TrimSpace(raw) == "" {
		return Document{}, body
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
		return Document{}, body
	}
	root := mappingRoot(&doc)
	if root == nil {
		return Document{}, body
	}
	return Document{Fields: fieldsOf(root)}, body
}

func fieldsOf(mapping *yaml.Node) []Field {
	out := make([]Field, 0, len(mapping.Content)/2)
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		key := normalizeKey(mapping.Content[i].Value)
		if key == "" {
			continue
		}
		out = append(out, Field{Key: key, Value: valueOf(mapping.Content[i+1])})
	}
	return out
}

func valueOf(node *yaml.Node) Value {
	switch {
	case node == nil:
		return Value{}
	case node.Kind == yaml.MappingNode:
		return Value{Kind: KindMapping, Fields: fieldsOf(node)}
	case node.Kind == yaml.SequenceNode:
		items := make([]Value, 0, len(node.Content))
		for _, item := range node.Content {
			items = append(items, valueOf(item))
		}
		return Value{Kind: KindSequence, Items: items}
	default:
		return Value{Kind: KindScalar, Scalar: strings.TrimSpace(node.Value)}
	}
}

// Lookup resolves a path of keys. A key repeated at one level resolves to the
// last one written, which is the precedence the flat view has always had.
func (d Document) Lookup(path ...string) (Value, bool) {
	if len(path) == 0 {
		return Value{}, false
	}
	fields, found := d.Fields, Value{}
	for depth, key := range path {
		key = normalizeKey(key)
		ok := false
		for _, f := range fields {
			if f.Key == key {
				found, ok = f.Value, true
			}
		}
		if !ok {
			return Value{}, false
		}
		if depth == len(path)-1 {
			return found, true
		}
		if found.Kind != KindMapping {
			return Value{}, false
		}
		fields = found.Fields
	}
	return found, true
}

// Has reports whether the author wrote this path at all, whatever they put
// under it. An empty mapping, an empty sequence and a bare key all count: each
// is the author reaching for the namespace.
func (d Document) Has(path ...string) bool {
	_, ok := d.Lookup(path...)
	return ok
}

// LegacyFlat is the compatibility view, for historic vocabularies only: every
// nested mapping collapsed onto its own leaf key, sequences joined, last write
// winning. A new semantic consumer reads Lookup/Has instead — the collapse
// drops which key a field was written under, and repolint holds the callers of
// this to a closed list for that reason.
func (d Document) LegacyFlat() map[string]string {
	out := map[string]string{}
	for _, f := range d.Fields {
		flatten(out, f.Key, f.Value)
	}
	return out
}

func flatten(out map[string]string, key string, v Value) {
	switch v.Kind {
	case KindMapping:
		for _, f := range v.Fields {
			flatten(out, f.Key, f.Value)
		}
	case KindSequence:
		items := make([]string, 0, len(v.Items))
		for _, item := range v.Items {
			if item.Scalar != "" {
				items = append(items, item.Scalar)
			}
		}
		if len(items) == 0 {
			return
		}
		joined := strings.Join(items, ", ")
		if key == "argument-hint" {
			joined = "[" + joined + "]"
		}
		out[key] = joined
	default:
		if v.Scalar != "" {
			out[key] = v.Scalar
		}
	}
}
