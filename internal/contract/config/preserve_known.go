// preserve_known.go — which keys a table's Config type decodes, and carrying
// the ones it does not through a save that rewrites the table around them.
package config

import (
	"reflect"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// tomlKnownKeys is the set of keys the table at path decodes into. all is true
// where the type takes any key (a map or an opaque value); ok is false where
// path names no table of Config.
type tomlKnownKeys struct {
	keys map[string]bool
	all  bool
}

func (k tomlKnownKeys) has(key string) bool {
	if k.all || k.keys[key] {
		return true
	}
	for known := range k.keys {
		if strings.EqualFold(known, key) {
			return true
		}
	}
	return false
}

func knownKeysAt(path tomlPath) (tomlKnownKeys, bool) {
	t := reflect.TypeFor[Config]()
	for _, seg := range path {
		t = tableType(t)
		if t.Kind() != reflect.Struct {
			return tomlKnownKeys{all: true}, true
		}
		f, ok := tomlField(t, seg.key)
		if !ok {
			return tomlKnownKeys{}, false
		}
		t = f.Type
		if seg.elem != "" || t.Kind() == reflect.Slice {
			t = tableType(t)
			if t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
				t = t.Elem()
			}
		}
	}
	t = tableType(t)
	if t.Kind() != reflect.Struct {
		return tomlKnownKeys{all: true}, true
	}
	keys := map[string]bool{}
	collectTOMLKeys(t, keys)
	return tomlKnownKeys{keys: keys}, true
}

func tableType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

func tomlFieldName(f reflect.StructField) (string, bool) {
	if !f.IsExported() {
		return "", false
	}
	name, _, _ := strings.Cut(f.Tag.Get("toml"), ",")
	switch name {
	case "-":
		return "", false
	case "":
		return f.Name, true
	}
	return name, true
}

func tomlField(t reflect.Type, key string) (reflect.StructField, bool) {
	var fold reflect.StructField
	found := false
	for f := range t.Fields() {
		if f.Anonymous && tableType(f.Type).Kind() == reflect.Struct && f.Tag.Get("toml") == "" {
			if inner, ok := tomlField(tableType(f.Type), key); ok {
				return inner, true
			}
			continue
		}
		name, ok := tomlFieldName(f)
		if !ok {
			continue
		}
		if name == key {
			return f, true
		}
		if !found && strings.EqualFold(name, key) {
			fold, found = f, true
		}
	}
	return fold, found
}

func collectTOMLKeys(t reflect.Type, into map[string]bool) {
	for f := range t.Fields() {
		if f.Anonymous && tableType(f.Type).Kind() == reflect.Struct && f.Tag.Get("toml") == "" {
			collectTOMLKeys(tableType(f.Type), into)
			continue
		}
		if name, ok := tomlFieldName(f); ok {
			into[name] = true
		}
	}
}

// dropTables removes the blocks match selects, marking the patch lossy when
// one holds an entry Config does not decode. A block inside an array element
// the result no longer has goes with that element.
func (p *configPatch) dropTables(match func(tomlPath) bool) {
	for _, b := range p.doc.blocks[1:] {
		if match(b.path) && p.holdsUnknown(b) && !p.elementGone(b.path) {
			p.lossy = true
			break
		}
	}
	p.doc.removeTables(match)
}

func (p *configPatch) elementGone(path tomlPath) bool {
	for i, seg := range path {
		if seg.elem != "" && p.rendered.block(path[:i+1]) < 0 {
			return true
		}
	}
	return false
}

func (p *configPatch) holdsUnknown(b tomlDocBlock) bool {
	known, ok := knownKeysAt(b.path)
	if !ok {
		return true
	}
	for _, k := range b.keys {
		if !known.has(k.key) {
			return true
		}
	}
	return false
}

// inlineUnknown returns the entries of the inline table at path.key that
// Config does not decode and that a table can hold as plain key lines; any
// other such entry marks the patch lossy.
func (p *configPatch) inlineUnknown(path tomlPath, key string) map[string]any {
	bi := p.doc.block(path)
	if _, ok := p.doc.key(bi, key); !ok {
		return nil
	}
	switch v := p.doc.keyValue(bi, key).(type) {
	case map[string]any:
		known, ok := knownKeysAt(path.child(key))
		out := map[string]any{}
		for k, x := range v {
			if ok && known.has(k) {
				continue
			}
			if _, line := tomlKeyLine(k, x); line {
				out[k] = x
			} else {
				p.lossy = true
			}
		}
		return out
	case []map[string]any:
		known, ok := knownKeysAt(path.child(key).element("x"))
		for _, el := range v {
			for k := range el {
				if !ok || !known.has(k) {
					p.lossy = true
				}
			}
		}
	}
	return nil
}

// tomlKeyLine encodes one entry as a single `key = value` line.
func tomlKeyLine(key string, value any) (string, bool) {
	var b strings.Builder
	if err := toml.NewEncoder(&b).Encode(map[string]any{key: value}); err != nil {
		return "", false
	}
	line := b.String()
	return line, strings.Count(strings.TrimRight(line, "\n"), "\n") == 0 && !strings.HasPrefix(line, "[")
}

func (p *configPatch) carryInline(path tomlPath, carry map[string]any) {
	keys := make([]string, 0, len(carry))
	for k := range carry {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		line, _ := tomlKeyLine(k, carry[k])
		p.doc.setKey(path, k, line)
	}
}

// elementExtra is what a text element holds that Config does not decode: key
// lines and sub-tables, verbatim.
type elementExtra struct {
	keys, tables string
	stuck        bool // held somewhere a rewrite cannot carry it from
}

// replaceArray writes the array at path whole from the render, carrying each
// text element's entries Config does not decode into the rendered element of
// the same name. An element the save deleted takes its entries with it; any
// other entry left without a home marks the patch lossy.
func (p *configPatch) replaceArray(path tomlPath, was, now map[string]map[string]any) {
	extras, dup := p.elementExtras(path)
	var b strings.Builder
	blocks := p.rendered.blocks
	for i := 1; i < len(blocks); {
		if len(blocks[i].path) != len(path) || !blocks[i].path.sameArray(path.element("x")) {
			i++
			continue
		}
		id := blocks[i].path[len(path)-1].elem
		ex, carried := extras[id]
		carried = carried && !dup[id] && !strings.HasPrefix(id, "#")
		b.WriteString("\n" + p.rendered.blockText(i))
		if carried {
			b.WriteString(ex.keys)
			if ex.stuck {
				p.lossy = true
			}
			delete(extras, id)
		}
		for i++; i < len(blocks) && len(blocks[i].path) > len(path) && blocks[i].path.under(path.element(id)); i++ {
			b.WriteString("\n" + p.rendered.blockText(i))
		}
		if carried {
			b.WriteString(ex.tables)
		}
	}
	for id, ex := range extras {
		_, deleted := was[id]
		if _, kept := now[id]; kept || !deleted || dup[id] || strings.HasPrefix(id, "#") {
			if ex.keys != "" || ex.tables != "" || ex.stuck {
				p.lossy = true
			}
		}
	}
	p.doc.removeTables(func(b tomlPath) bool { return b.under(path) })
	if b.Len() > 0 {
		p.doc.insertRegion(path, strings.TrimPrefix(b.String(), "\n"))
	}
}

func (p *configPatch) elementExtras(path tomlPath) (map[string]*elementExtra, map[string]bool) {
	probe := path.element("x")
	known, ok := knownKeysAt(probe)
	extras, seen, dup := map[string]*elementExtra{}, map[string]bool{}, map[string]bool{}
	for i, b := range p.doc.blocks {
		if i == 0 || len(b.path) < len(path) || !b.path[:len(path)].sameArray(probe) {
			continue
		}
		id := b.path[len(path)-1].elem
		ex := extras[id]
		if ex == nil {
			ex = &elementExtra{}
			extras[id] = ex
		}
		switch {
		case len(b.path) == len(path):
			dup[id] = dup[id] || seen[id]
			seen[id] = true
			for _, k := range b.keys {
				if !ok || !known.has(k.key) {
					ex.keys += strings.Join(p.doc.lines[k.first:k.last+1], "")
				}
			}
			if ex.keys != "" && !strings.HasSuffix(ex.keys, "\n") {
				ex.keys += "\n"
			}
		case !ok || !known.has(b.path[len(path)].key):
			ex.tables += "\n" + p.doc.blockText(i)
		default:
			ex.stuck = ex.stuck || p.holdsUnknown(b)
		}
	}
	return extras, dup
}
