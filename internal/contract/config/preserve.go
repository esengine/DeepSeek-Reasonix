// preserve.go — saving the user config by rewriting only the keys a save
// changed. The file is shared with the 1.x line and with hand edits, so a key
// this build does not decode, or a value it reads differently, is not its to drop.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	fileencoding "reasonix/internal/base/fileutil/encoding"
)

// userConfigBody is what a save writes to the user config at resolved: the
// existing text with only the changed keys rewritten, or, when that cannot be
// shown to load as c, the whole render. The prior bytes are kept beside it
// whenever an entry Config does not decode is not carried; a body that does
// not load is never returned.
func (c *Config) userConfigBody(logicalPath, resolved string) (string, bool, error) {
	full := RenderTOMLForScope(c, RenderScopeUser)
	fullLoaded, fullErr := loadUserConfigBytes(logicalPath, full)
	raw, err := fileencoding.ReadFileUTF8(resolved)
	if err != nil || strings.TrimSpace(string(raw)) == "" {
		return full, errors.Is(err, os.ErrNotExist), renderLoadError(resolved, fullErr)
	}
	text := string(fileencoding.DecodeToUTF8(raw))
	base, err := loadUserConfigBytes(logicalPath, text)
	if err != nil {
		return full, false, renderLoadError(resolved, fullErr)
	}
	patched, ok, lossy := patchUserConfig(text, base, c, full)
	for _, strip := range retiredUserConfigKeys {
		patched, _ = strip(patched)
	}
	if ok {
		if loaded, err := loadUserConfigBytes(logicalPath, patched); err == nil && (sameUserRender(loaded, fullLoaded) ||
			fullErr != nil && RenderTOMLForScope(loaded, RenderScopeUser) == full) {
			if lossy {
				keepPriorUserConfig(logicalPath, resolved, raw, "config: saved in place without entries it could not carry")
			}
			return patched, false, nil
		}
	}
	if fullErr != nil {
		return "", false, renderLoadError(resolved, fullErr)
	}
	keepPriorUserConfig(logicalPath, resolved, raw, "config: saved by full rewrite")
	return full, false, nil
}

func keepPriorUserConfig(logicalPath, resolved string, raw []byte, msg string) {
	backup := resolved + ".rewrite-" + time.Now().Format("20060102-150405")
	if err := os.WriteFile(backup, raw, configFilePerm(logicalPath)); err != nil {
		slog.Warn("config: back up the prior user config", "path", resolved, "err", err)
	}
	slog.Warn(msg, "path", resolved, "backup", backup)
}

// writeUserConfig hands write the body a save puts in the user config, and
// writes nothing when there is no body that loads.
func (c *Config) writeUserConfig(logicalPath, resolved string, write func(body string) error) error {
	body, newFile, err := c.userConfigBody(logicalPath, resolved)
	if err != nil {
		return err
	}
	if err := write(body); err != nil {
		return err
	}
	if newFile {
		if err := markDesktopPostureReleased(logicalPath); err != nil {
			slog.Warn("config: record desktop posture release", "path", logicalPath, "err", err)
		}
	}
	return nil
}

func renderLoadError(path string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("config %s: rendered settings do not load, file left unchanged: %w", path, err)
}

// retiredUserConfigKeys are the keys this build declares retired; a save
// removes them rather than carrying a setting nothing honours.
var retiredUserConfigKeys = []func(string) (string, bool){
	stripLegacyAgentStepLimitLines,
	stripLegacyRedactToolOutputLines,
	stripLegacyMemoryCompilerLines,
	stripLegacyMultiThresholdCompactionLines,
	func(raw string) (string, bool) {
		return stripTOMLKeyLines(raw, "agent", "auto_plan", "auto_plan_classifier")
	},
}

func loadUserConfigBytes(path, text string) (*Config, error) {
	cfg := Default()
	if _, err := mergeFileSnapshotWithRead(cfg, path, func(string) ([]byte, error) { return []byte(text), nil }); err != nil {
		return nil, err
	}
	normalizeConfigForEdit(cfg)
	return cfg, nil
}

func sameUserRender(a, b *Config) bool {
	if a == nil || b == nil {
		return false
	}
	ma, okA := decodeTOMLTree(RenderTOMLForScope(a, RenderScopeUser))
	mb, okB := decodeTOMLTree(RenderTOMLForScope(b, RenderScopeUser))
	return okA && okB && sameValue(ma, mb)
}

// decodeRenderTree decodes a render, setting aside each line the decoder
// refuses, such as a float the renderer spells as NaN. skipped lists them so
// a caller can require both sides of a save to have set aside the same ones.
func decodeRenderTree(text string) (tree map[string]any, skipped []string, ok bool) {
	lines := strings.SplitAfter(text, "\n")
	for range 8 {
		_, err := toml.Decode(strings.Join(lines, ""), &tree)
		if err == nil {
			return tree, skipped, true
		}
		var pe toml.ParseError
		if !errors.As(err, &pe) || pe.Position.Line < 1 || pe.Position.Line > len(lines) {
			return nil, nil, false
		}
		skipped = append(skipped, lines[pe.Position.Line-1])
		lines[pe.Position.Line-1] = "\n"
		tree = nil
	}
	return nil, nil, false
}

func decodeTOMLTree(text string) (map[string]any, bool) {
	var out map[string]any
	_, err := toml.Decode(text, &out)
	return out, err == nil
}

// patchUserConfig applies to text what separates next from base: the keys
// their renders disagree on, and the keys next changed that it never renders.
// lossy reports that an entry Config does not decode was dropped on the way.
func patchUserConfig(text string, base, next *Config, nextRender string) (patched string, ok, lossy bool) {
	baseRender := RenderTOMLForScope(base, RenderScopeUser)
	doc, parsed := parseTOMLDoc(text)
	if !parsed {
		return "", false, false
	}
	rendered, parsed := parseTOMLDoc(nextRender)
	if !parsed {
		return "", false, false
	}
	before, skippedB, okB := decodeRenderTree(baseRender)
	after, skippedA, okA := decodeRenderTree(nextRender)
	if !okB || !okA || !slices.Equal(skippedB, skippedA) {
		return "", false, false
	}
	was, _ := encodeConfigTree(base)
	now, _ := encodeConfigTree(next)
	p := &configPatch{doc: doc, rendered: rendered}
	p.table(nil, configSide{before, was}, configSide{after, now})
	if was != nil && now != nil {
		p.dropUnrendered(nil, was, now, after)
	}
	if doc.broken || p.broken {
		return "", false, false
	}
	return doc.String(), true, p.lossy
}

type configPatch struct {
	doc, rendered *tomlDoc
	broken, lossy bool
}

// configSide is one side of a save at one path: its value as rendered, and as
// the struct encodes it, which still tells apart values the render writes alike.
type configSide struct{ shown, raw any }

func (s configSide) at(key string) configSide {
	shown, _ := s.shown.(map[string]any)
	raw, _ := s.raw.(map[string]any)
	return configSide{shown[key], raw[key]}
}

func (p *configPatch) table(path tomlPath, before, after configSide) {
	shownB, _ := before.shown.(map[string]any)
	shownA, _ := after.shown.(map[string]any)
	for _, key := range sortedUnion(shownB, shownA) {
		was, inBefore := shownB[key]
		now, inAfter := shownA[key]
		switch {
		case !inAfter:
			if inBefore {
				p.remove(path, key)
			}
		case p.renderedAsLine(path, key):
			if inBefore && sameValue(was, now) && sameValue(before.at(key).raw, after.at(key).raw) {
				continue
			}
			p.dropTables(func(b tomlPath) bool { return b.under(path.child(key)) })
			p.setFromRender(path, key)
		default:
			p.nested(path, key, before.at(key), after.at(key))
		}
	}
}

func (p *configPatch) nested(path tomlPath, key string, was, now configSide) {
	var carry map[string]any
	if _, line := p.doc.key(p.doc.block(path), key); line {
		carry = p.inlineUnknown(path, key)
		p.doc.removeKey(path, key)
		was = configSide{}
	}
	switch v := now.shown.(type) {
	case map[string]any:
		prev, _ := was.shown.(map[string]any)
		if !p.doc.hasTable(path.child(key)) && !sameValue(prev, v) {
			p.insertFromRender(path.child(key))
		} else {
			p.table(path.child(key), was, now)
		}
		p.carryInline(path.child(key), carry)
	case []map[string]any:
		p.array(path.child(key), was, now)
	default:
		p.broken = true
	}
}

// array edits the elements the save changed in place. The whole array is
// written from the render instead when an element has no identity, when one
// the save touches is not a block of its own in the text (the load merged or
// renamed it), or when the save reorders the elements the text holds.
func (p *configPatch) array(path tomlPath, before, after configSide) {
	shownB, _ := before.shown.([]map[string]any)
	shownA, _ := after.shown.([]map[string]any)
	rawB, _ := before.raw.([]map[string]any)
	rawA, _ := after.raw.([]map[string]any)
	if sameValue(shownB, shownA) && sameValue(rawB, rawA) {
		return
	}
	rawWas, _ := elementsByIdentity(rawB)
	rawNow, _ := elementsByIdentity(rawA)
	was, okB := elementsByIdentity(shownB)
	now, okA := elementsByIdentity(shownA)
	changed := func(id string) bool {
		next, ok := now[id]
		return !ok || !sameValue(was[id], next) || !sameValue(rawWas[id], rawNow[id])
	}
	ids, named := p.docElementIDs(path)
	if !okB || !okA || len(ids) == 0 || !named || !docHoldsChanged(ids, was, changed) || !keepsOrder(ids, was, shownA) {
		p.replaceArray(path, was, now)
		return
	}
	gone := map[string]bool{}
	for id := range was {
		if _, ok := now[id]; !ok {
			gone[path.element(id).String()] = true
		}
	}
	if len(gone) > 0 {
		p.doc.removeTables(func(b tomlPath) bool { return len(b) >= len(path) && gone[b[:len(path)].String()] })
	}
	for i, next := range shownA {
		id := elementIdentity(next["name"], 0)
		if _, existed := was[id]; existed && !changed(id) {
			continue
		}
		el := path.element(id)
		if p.doc.block(el) < 0 {
			p.insertElement(el, p.nextBlockIn(path, shownA[i+1:]))
			continue
		}
		p.table(el, configSide{was[id], rawWas[id]}, configSide{next, rawNow[id]})
	}
}

// nextBlockIn is the header block of the first of rest the text holds, or -1.
func (p *configPatch) nextBlockIn(path tomlPath, rest []map[string]any) int {
	for _, el := range rest {
		if bi := p.doc.block(path.element(elementIdentity(el["name"], 0))); bi >= 0 {
			return bi
		}
	}
	return -1
}

// insertElement writes the rendered element el just before the block at
// index before, after the keys of the block ahead of it, so comments above
// that header stay with it; with no such block it goes after the array.
func (p *configPatch) insertElement(el tomlPath, before int) {
	if before < 1 {
		p.insertFromRender(el)
		return
	}
	region := p.rendered.region(func(b tomlPath) bool { return b.under(el) })
	if region == "" {
		return
	}
	prev := p.doc.blocks[before-1]
	at := prev.header + 1
	if n := len(prev.keys); n > 0 {
		at = prev.keys[n-1].last + 1
	}
	p.doc.splice(at, 0, region)
}

func docHoldsChanged(ids []string, was map[string]map[string]any, changed func(string) bool) bool {
	inDoc := make(map[string]bool, len(ids))
	for _, id := range ids {
		inDoc[id] = true
	}
	for id := range was {
		if changed(id) && !inDoc[id] {
			return false
		}
	}
	return true
}

// keepsOrder reports whether next lists the elements it keeps from the text
// in the order the text holds them.
func keepsOrder(ids []string, was map[string]map[string]any, next []map[string]any) bool {
	at := make(map[string]int, len(ids))
	for i, id := range ids {
		at[id] = i
	}
	last := -1
	for _, el := range next {
		id := elementIdentity(el["name"], 0)
		i, inDoc := at[id]
		if _, kept := was[id]; !kept || !inDoc {
			continue
		}
		if i < last {
			return false
		}
		last = i
	}
	return true
}

// elementsByIdentity keys elements by name; ok is false when any element has
// no name or two share one, which leaves the array to be written as a whole.
func elementsByIdentity(list []map[string]any) (map[string]map[string]any, bool) {
	out := make(map[string]map[string]any, len(list))
	for _, el := range list {
		name, _ := el["name"].(string)
		if name == "" {
			return nil, false
		}
		id := elementIdentity(name, 0)
		if _, dup := out[id]; dup {
			return nil, false
		}
		out[id] = el
	}
	return out, true
}

// docElementIDs lists the identities of the array's elements in text order;
// named is false when one has no name or two share one.
func (p *configPatch) docElementIDs(path tomlPath) (ids []string, named bool) {
	seen := map[string]bool{}
	named = true
	probe := path.element("x")
	for _, b := range p.doc.blocks[1:] {
		if !b.path.sameArray(probe) {
			continue
		}
		id := b.path[len(path)-1].elem
		if strings.HasPrefix(id, "#") || seen[id] {
			named = false
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids, named
}

// dropUnrendered removes a key the edit changed but the renderer never
// writes: the build owns it and has decided it no longer belongs on disk.
func (p *configPatch) dropUnrendered(path tomlPath, was, now, rendered map[string]any) {
	for _, key := range sortedUnion(was, now) {
		if _, shown := rendered[key]; shown {
			if wasT, ok := was[key].(map[string]any); ok {
				nowT, _ := now[key].(map[string]any)
				shownT, _ := rendered[key].(map[string]any)
				p.dropUnrendered(path.child(key), wasT, nowT, shownT)
			}
			continue
		}
		if !sameValue(was[key], now[key]) {
			p.remove(path, key)
		}
	}
}

func encodeConfigTree(c *Config) (map[string]any, bool) {
	var b strings.Builder
	if err := toml.NewEncoder(&b).Encode(c); err != nil {
		return nil, false
	}
	return decodeTOMLTree(b.String())
}

func (p *configPatch) remove(path tomlPath, key string) {
	if len(p.inlineUnknown(path, key)) > 0 {
		p.lossy = true
	}
	p.doc.removeKey(path, key)
	p.dropTables(func(b tomlPath) bool { return b.under(path.child(key)) })
}

func (p *configPatch) renderedAsLine(path tomlPath, key string) bool {
	_, ok := p.rendered.key(p.rendered.block(path), key)
	return ok
}

func (p *configPatch) setFromRender(path tomlPath, key string) {
	k, ok := p.rendered.key(p.rendered.block(path), key)
	if !ok {
		p.broken = true
		return
	}
	p.doc.setKey(path, key, strings.Join(p.rendered.lines[k.first:k.last+1], ""))
}

func (p *configPatch) insertFromRender(path tomlPath) {
	region := p.rendered.region(func(b tomlPath) bool { return b.under(path) })
	if region == "" {
		return
	}
	p.doc.insertRegion(path, strings.TrimPrefix(region, "\n"))
}

// sameValue is reflect.DeepEqual over decoded TOML, except that NaN equals
// NaN: a float the file holds as nan is the same value on both sides.
func sameValue(a, b any) bool {
	switch x := a.(type) {
	case float64:
		y, ok := b.(float64)
		return ok && (x == y || x != x && y != y)
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, v := range x {
			if w, ok := y[k]; !ok || !sameValue(v, w) {
				return false
			}
		}
		return true
	case []map[string]any:
		y, ok := b.([]map[string]any)
		return ok && slices.EqualFunc(x, y, func(m, n map[string]any) bool { return sameValue(m, n) })
	case []any:
		y, ok := b.([]any)
		return ok && slices.EqualFunc(x, y, sameValue)
	}
	return reflect.DeepEqual(a, b)
}

func sortedUnion(a, b map[string]any) []string {
	keys := make([]string, 0, len(a)+len(b))
	for k := range a {
		keys = append(keys, k)
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}
