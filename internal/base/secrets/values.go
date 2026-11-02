package secrets

import (
	"encoding/json"
	"net/url"
	"slices"
	"strings"
	"sync"
)

// shortValue is the length under which a value is masked only where it stands
// alone: a four-digit PIN is also part of order numbers and line counts.
const shortValue = 8

// minValue is the shortest value worth registering; anything shorter would
// mask ordinary text and protect nothing.
const minValue = 4

// Labeled is a value the host holds for a person, and the name a model reads
// in its place.
type Labeled struct{ Label, Value string }

var registered struct {
	sync.Mutex
	held     map[Labeled]struct{}
	sources  map[string]func() []Labeled
	snapshot *Mask
}

// WatchValues adds a source read on every Masker call, so masking follows the
// store that owns the values rather than whoever remembered to announce a
// change. A second source under the same key replaces the first. Nothing
// forgets a value a source once answered: a value handed out may sit in any
// transcript, and forgetting it would unmask every copy at once.
func WatchValues(key string, src func() []Labeled) {
	registered.Lock()
	defer registered.Unlock()
	if registered.sources == nil {
		registered.sources = map[string]func() []Labeled{}
	}
	registered.sources[key] = src
}

func hold(l Labeled) {
	if len(l.Value) < minValue || l.Label == "" {
		return
	}
	if registered.held == nil {
		registered.held = map[Labeled]struct{}{}
	}
	if _, exists := registered.held[l]; exists {
		return
	}
	registered.held[l] = struct{}{}
	registered.snapshot = nil
}

// Mask is a snapshot of the registered values. A nil Mask masks nothing.
type Mask struct {
	values []spelled
	labels map[string]bool
}

// spelled is one value with every spelling it is masked in, and its mark.
type spelled struct {
	mark   string
	labels []string
	forms  []string
	short  bool
}

// Masker reads every source, holds what they answer, and snapshots the lot;
// nil when nothing is registered.
func Masker() *Mask {
	registered.Lock()
	srcs := make([]func() []Labeled, 0, len(registered.sources))
	for _, src := range registered.sources {
		srcs = append(srcs, src)
	}
	registered.Unlock()
	var fresh []Labeled
	for _, src := range srcs {
		fresh = append(fresh, src()...)
	}
	registered.Lock()
	defer registered.Unlock()
	for _, l := range fresh {
		hold(l)
	}
	if len(registered.held) == 0 {
		return nil
	}
	if registered.snapshot != nil {
		return registered.snapshot
	}
	byValue := map[string][]string{}
	labels := map[string]bool{}
	for l := range registered.held {
		labels[l.Label] = true
		if !slices.Contains(byValue[l.Value], l.Label) {
			byValue[l.Value] = append(byValue[l.Value], l.Label)
		}
	}
	values := make([]string, 0, len(byValue))
	for v := range byValue {
		values = append(values, v)
	}
	slices.SortFunc(values, func(a, b string) int {
		if d := len(b) - len(a); d != 0 {
			return d
		}
		return strings.Compare(a, b)
	})
	m := &Mask{labels: labels}
	for _, v := range values {
		names := slices.Sorted(slices.Values(byValue[v]))
		m.values = append(m.values, spelled{mark: Mark(strings.Join(names, ",")), labels: names, forms: forms(v), short: len(v) < shortValue})
	}
	registered.snapshot = m
	return m
}

// String replaces every registered value in s with [secret:LABEL]: the value
// as written, JSON-escaped, and URL-encoded. A value held under several labels
// reads [secret:A,B], so whoever holds either knows it. Longer values go
// first, so a value that contains another is masked whole.
func (m *Mask) String(s string) string {
	if m == nil || s == "" {
		return s
	}
	for _, v := range m.values {
		for _, form := range v.forms {
			s = replace(s, form, v.mark, v.short)
		}
	}
	return s
}

// Marks returns the labels whose mark appears in s: text a model wrote back
// from a masked copy, which would put the mark where the value was.
func (m *Mask) Marks(s string) []string {
	if m == nil || !strings.Contains(s, markOpen) {
		return nil
	}
	var out []string
	for label := range m.labels {
		if strings.Contains(s, Mark(label)) {
			out = append(out, label)
		}
	}
	for _, v := range m.values {
		if len(v.labels) > 1 && strings.Contains(s, v.mark) {
			out = append(out, v.labels...)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

const markOpen = "[secret:"

// Mark is what a model reads in place of a value registered under label.
func Mark(label string) string { return markOpen + label + "]" }

// forms are the spellings a value takes on its way to a model.
func forms(v string) []string {
	quoted, _ := json.Marshal(v)
	out := []string{v}
	for _, f := range []string{string(quoted[1 : len(quoted)-1]), url.QueryEscape(v), url.PathEscape(v)} {
		if !slices.Contains(out, f) {
			out = append(out, f)
		}
	}
	return out
}

// replace swaps every occurrence of form for mark; a standalone form is
// replaced only between non-word bytes, judged on s as it was, so a separator
// between two copies counts for both.
func replace(s, form, mark string, standalone bool) string {
	if !strings.Contains(s, form) {
		return s
	}
	if !standalone {
		return strings.ReplaceAll(s, form, mark)
	}
	var b strings.Builder
	at := 0
	for {
		i := strings.Index(s[at:], form)
		if i < 0 {
			break
		}
		i += at
		end := i + len(form)
		if (i > 0 && word(s[i-1])) || (end < len(s) && word(s[end])) {
			b.WriteString(s[at : i+1])
			at = i + 1
			continue
		}
		b.WriteString(s[at:i])
		b.WriteString(mark)
		at = end
	}
	b.WriteString(s[at:])
	return b.String()
}

func word(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}
