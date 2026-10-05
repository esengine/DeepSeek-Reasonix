// tomldoc.go — a line-level index of a TOML document, addressed by table path,
// so a save can rewrite the keys it changed and leave every other byte alone.
package config

import (
	"slices"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// tomlPath addresses a table. An element of an array of tables is named by
// its identity: the element's `name` string, or its ordinal when it has none.
type tomlPath []tomlPathSeg

type tomlPathSeg struct {
	key  string
	elem string
}

func (p tomlPath) child(key string) tomlPath {
	return append(append(tomlPath(nil), p...), tomlPathSeg{key: key})
}

func (p tomlPath) element(id string) tomlPath {
	out := append(tomlPath(nil), p...)
	out[len(out)-1].elem = id
	return out
}

func (p tomlPath) String() string {
	var b strings.Builder
	for _, s := range p {
		b.WriteString(s.key)
		b.WriteByte(0x1f)
		if s.elem != "" {
			b.WriteString(s.elem)
			b.WriteByte(0x1e)
		}
	}
	return b.String()
}

func (p tomlPath) header() string {
	keys := make([]string, len(p))
	for i, s := range p {
		keys[i] = tomlKeySegment(s.key)
	}
	return strings.Join(keys, ".")
}

// under reports whether q is p or lies inside it.
func (p tomlPath) under(q tomlPath) bool {
	return strings.HasPrefix(p.String(), q.String())
}

// sameArray reports whether p and q are elements of one array of tables.
func (p tomlPath) sameArray(q tomlPath) bool {
	n := len(p)
	return n > 0 && n == len(q) && p[n-1].elem != "" && q[n-1].elem != "" &&
		p[n-1].key == q[n-1].key && p[:n-1].String() == q[:n-1].String()
}

func tomlKeySegment(key string) string {
	if key != "" && strings.IndexFunc(key, func(r rune) bool {
		return !(r == '_' || r == '-' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9')
	}) < 0 {
		return key
	}
	return strconv.Quote(key)
}

func elementIdentity(name any, ordinal int) string {
	if s, ok := name.(string); ok && s != "" {
		return "name=" + s
	}
	return "#" + strconv.Itoa(ordinal)
}

type tomlDocKey struct {
	key         string
	first, last int // line indexes of the whole value
}

type tomlDocBlock struct {
	path   tomlPath
	header int // line index; -1 for the root table
	end    int // exclusive line index of the next header
	keys   []tomlDocKey
}

type tomlDoc struct {
	lines  []string // each line keeps its terminator
	blocks []tomlDocBlock
	crlf   bool
	broken bool // an edit left text this index cannot model
}

// parseTOMLDoc indexes text. ok is false for a form it does not model — a
// dotted key or a header it cannot split — which leaves the caller to write
// the document whole rather than guess where a key lives.
func parseTOMLDoc(text string) (*tomlDoc, bool) {
	d := &tomlDoc{crlf: strings.Contains(text, "\r\n")}
	for _, span := range tomlLineSpans(text) {
		d.lines = append(d.lines, span.text)
	}
	d.blocks = []tomlDocBlock{{header: -1}}
	type rawHeader struct {
		segs  []string
		array bool
	}
	headers := []rawHeader{{}}
	for i := 0; i < len(d.lines); i++ {
		line := strings.TrimSpace(tomlStripComment(d.lines[i]))
		cur := &d.blocks[len(d.blocks)-1]
		switch {
		case line == "":
		case strings.HasPrefix(line, "["):
			array := strings.HasPrefix(line, "[[")
			inner := strings.TrimSuffix(strings.TrimPrefix(line, "["), "]")
			if array {
				inner = strings.TrimSuffix(strings.TrimPrefix(inner, "["), "]")
			}
			segs, ok := splitTOMLKey(inner)
			if !ok {
				return nil, false
			}
			cur.end = i
			d.blocks = append(d.blocks, tomlDocBlock{header: i})
			headers = append(headers, rawHeader{segs: segs, array: array})
		default:
			eq := tomlAssignmentIndex(d.lines[i])
			if eq < 0 {
				return nil, false
			}
			segs, ok := splitTOMLKey(d.lines[i][:eq])
			if !ok || len(segs) != 1 {
				return nil, false
			}
			last, ok := tomlValueEnd(d.lines, i, eq+1)
			if !ok {
				return nil, false
			}
			cur.keys = append(cur.keys, tomlDocKey{key: segs[0], first: i, last: last})
			i = last
		}
	}
	d.blocks[len(d.blocks)-1].end = len(d.lines)
	current := map[string]tomlPath{}
	ordinals := map[string]int{}
	for bi := 1; bi < len(d.blocks); bi++ {
		h := headers[bi]
		var path tomlPath
		for si, seg := range h.segs {
			path = path.child(seg)
			if si == len(h.segs)-1 && h.array {
				break
			}
			if el, ok := current[path.String()]; ok {
				path = el
			}
		}
		if h.array {
			key := path.String()
			path = path.element(elementIdentity(d.keyValue(bi, "name"), ordinals[key]))
			ordinals[key]++
			current[key] = path
		}
		d.blocks[bi].path = path
	}
	return d, true
}

func tomlStripComment(line string) string {
	state := tomlLexState{}
	if i := scanTOMLLine(line, &state, nil); i >= 0 {
		return line[:i]
	}
	return line
}

// tomlAssignmentIndex finds the `=` that ends a key, outside any quoted key.
func tomlAssignmentIndex(line string) int {
	quote := byte(0)
	for i := 0; i < len(line); i++ {
		switch ch := line[i]; {
		case quote != 0:
			if ch == '\\' && quote == '"' {
				i++
			} else if ch == quote {
				quote = 0
			}
		case ch == '"' || ch == '\'':
			quote = ch
		case ch == '=':
			return i
		case ch == '#':
			return -1
		}
	}
	return -1
}

// splitTOMLKey splits a possibly dotted, possibly quoted key into its parts.
func splitTOMLKey(raw string) ([]string, bool) {
	var probe map[string]any
	if _, err := toml.Decode(strings.TrimSpace(raw)+" = 0", &probe); err != nil {
		return nil, false
	}
	var segs []string
	for len(probe) == 1 {
		var next any
		for k, v := range probe {
			segs = append(segs, k)
			next = v
		}
		m, ok := next.(map[string]any)
		if !ok {
			return segs, len(segs) > 0
		}
		probe = m
	}
	return nil, false
}

// tomlValueEnd returns the last line of a value starting on line first at
// column col, following open brackets, braces and multi-line strings.
func tomlValueEnd(lines []string, first, col int) (int, bool) {
	depth := 0
	state := tomlLexState{}
	for i := first; i < len(lines); i++ {
		text := lines[i]
		if i == first {
			text = text[col:]
		}
		scanTOMLLine(text, &state, func(ch byte) {
			switch ch {
			case '[', '{':
				depth++
			case ']', '}':
				depth--
			}
		})
		if depth <= 0 && !state.inMultilineString() {
			return i, true
		}
	}
	return 0, false
}

func (d *tomlDoc) keyValue(block int, key string) any {
	for _, k := range d.blocks[block].keys {
		if k.key != key {
			continue
		}
		var out map[string]any
		if _, err := toml.Decode("v = "+strings.Join(d.lines[k.first:k.last+1], "")[tomlAssignmentIndex(d.lines[k.first])+1:], &out); err == nil {
			return out["v"]
		}
	}
	return nil
}

func (d *tomlDoc) String() string { return strings.Join(d.lines, "") }

func (d *tomlDoc) block(p tomlPath) int {
	want := p.String()
	for i, b := range d.blocks {
		if b.path.String() == want {
			return i
		}
	}
	return -1
}

func (d *tomlDoc) key(block int, key string) (tomlDocKey, bool) {
	if block < 0 {
		return tomlDocKey{}, false
	}
	for _, k := range d.blocks[block].keys {
		if k.key == key {
			return k, true
		}
	}
	return tomlDocKey{}, false
}

// hasTable reports whether any header sits at or below p.
func (d *tomlDoc) hasTable(p tomlPath) bool {
	for _, b := range d.blocks[1:] {
		if b.path.under(p) {
			return true
		}
	}
	return false
}

// region returns the text of the blocks at or below p, each cut after its
// last key so a trailing comment meant for the next table stays behind.
func (d *tomlDoc) region(match func(tomlPath) bool) string {
	var b strings.Builder
	for i, blk := range d.blocks {
		if i > 0 && match(blk.path) {
			b.WriteString("\n" + d.blockText(i))
		}
	}
	return b.String()
}

// blockText is block i from its header through its last key.
func (d *tomlDoc) blockText(i int) string {
	blk := d.blocks[i]
	end := blk.header
	if n := len(blk.keys); n > 0 {
		end = blk.keys[n-1].last
	}
	text := strings.Join(d.lines[blk.header:end+1], "")
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return text
}

// newline gives text the document's line ending, except inside a multi-line
// string, where the ending is part of the value.
func (d *tomlDoc) newline(text string) string {
	if !d.crlf {
		return text
	}
	var b strings.Builder
	state := tomlLexState{}
	for _, span := range tomlLineSpans(text) {
		line := span.text
		scanTOMLLine(strings.TrimRight(line, "\r\n"), &state, nil)
		if !state.inMultilineString() && strings.HasSuffix(line, "\n") && !strings.HasSuffix(line, "\r\n") {
			line = strings.TrimSuffix(line, "\n") + "\r\n"
		}
		b.WriteString(line)
	}
	return b.String()
}

func (d *tomlDoc) splice(at, drop int, text string) {
	var inserted []string
	if text != "" {
		for _, span := range tomlLineSpans(d.newline(text)) {
			inserted = append(inserted, span.text)
		}
	}
	if at > 0 && !strings.HasSuffix(d.lines[at-1], "\n") {
		d.lines[at-1] += d.newline("\n")
	}
	if n := len(inserted); n > 0 && !strings.HasSuffix(inserted[n-1], "\n") {
		inserted[n-1] += d.newline("\n")
	}
	next := append(append(append([]string(nil), d.lines[:at]...), inserted...), d.lines[at+drop:]...)
	reparsed, ok := parseTOMLDoc(strings.Join(next, ""))
	if !ok {
		d.lines, d.broken = next, true
		return
	}
	*d = *reparsed
}

// setKey writes key = value into the table at p from the rendered lines, keeping
// the document's own trailing comment when both sides fit on one line.
func (d *tomlDoc) setKey(p tomlPath, key, rendered string) {
	bi := d.block(p)
	if bi < 0 && p[len(p)-1].elem != "" {
		d.broken = true
		return
	}
	if bi < 0 {
		d.insertRegion(p, "["+p.header()+"]\n")
		bi = d.block(p)
		if bi < 0 {
			return
		}
	}
	if k, ok := d.key(bi, key); ok {
		if k.first == k.last && !strings.Contains(strings.TrimRight(rendered, "\r\n"), "\n") {
			if comment := tomlInlineComment(d.lines[k.first]); comment != "" {
				rendered = strings.TrimRight(tomlStripComment(rendered), " \t\r\n") + "   " + comment + "\n"
			}
		}
		d.splice(k.first, k.last-k.first+1, rendered)
		return
	}
	at := d.blocks[bi].header + 1
	if n := len(d.blocks[bi].keys); n > 0 {
		at = d.blocks[bi].keys[n-1].last + 1
	} else if bi == 0 {
		at = d.blocks[0].end
	}
	d.splice(at, 0, rendered)
}

// removeKey drops the key, and its table when that leaves the table empty.
func (d *tomlDoc) removeKey(p tomlPath, key string) {
	bi := d.block(p)
	k, ok := d.key(bi, key)
	if !ok {
		return
	}
	d.splice(k.first, k.last-k.first+1, "")
	if bi = d.block(p); bi > 0 && len(d.blocks[bi].keys) == 0 && !d.hasSubTable(p) {
		d.splice(d.blocks[bi].header, d.blocks[bi].end-d.blocks[bi].header, "")
	}
}

func (d *tomlDoc) hasSubTable(p tomlPath) bool {
	for _, b := range d.blocks[1:] {
		if len(b.path) > len(p) && b.path.under(p) {
			return true
		}
	}
	return false
}

// removeTables drops every block the predicate selects, all against one
// index: removing an element header first would hand its sub-tables to the
// element above it.
func (d *tomlDoc) removeTables(match func(tomlPath) bool) {
	var spans [][2]int
	for _, b := range d.blocks[1:] {
		if match(b.path) {
			spans = append(spans, [2]int{b.header, b.end})
		}
	}
	if len(spans) == 0 {
		return
	}
	next := append([]string(nil), d.lines...)
	for _, span := range slices.Backward(spans) {
		next = append(next[:span[0]], next[span[1]:]...)
	}
	reparsed, ok := parseTOMLDoc(strings.Join(next, ""))
	if !ok {
		d.lines, d.broken = next, true
		return
	}
	*d = *reparsed
}

// insertRegion places new blocks for p where TOML attaches them: after the
// other elements of p's array, inside the nearest array element p belongs
// to, or at the end of the document.
func (d *tomlDoc) insertRegion(p tomlPath, text string) {
	at := -1
	if p[len(p)-1].elem != "" {
		at = d.lastBlockEnd(func(b tomlPath) bool { return len(b) >= len(p) && b[:len(p)].sameArray(p) })
	}
	for i := len(p) - 2; at < 0 && i >= 0; i-- {
		if p[i].elem != "" {
			anchor := p[:i+1]
			at = d.lastBlockEnd(func(b tomlPath) bool { return b.under(anchor) })
		}
	}
	if at < 0 {
		at = len(d.lines)
	}
	d.splice(at, 0, "\n"+text)
}

// lastBlockEnd is the line after the last key of the last matching block.
func (d *tomlDoc) lastBlockEnd(match func(tomlPath) bool) int {
	at := -1
	for _, b := range d.blocks[1:] {
		if !match(b.path) {
			continue
		}
		at = b.header + 1
		if n := len(b.keys); n > 0 {
			at = b.keys[n-1].last + 1
		}
	}
	return at
}
