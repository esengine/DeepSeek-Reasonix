package secrets

import (
	"encoding/json"
	"net/url"
	"regexp"
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

var registered struct {
	sync.RWMutex
	labels map[string]string
}

// RegisterValue records a secret value the host holds for a person under a
// label, so MaskRegistered masks it wherever this process renders text for a
// model. Values shorter than four characters are not registered.
func RegisterValue(label, value string) {
	if len(value) < minValue {
		return
	}
	registered.Lock()
	defer registered.Unlock()
	if registered.labels == nil {
		registered.labels = map[string]string{}
	}
	registered.labels[value] = label
}

// HasRegistered reports whether any value is registered, so a caller with
// nothing to mask can skip copying what it would have masked.
func HasRegistered() bool {
	registered.RLock()
	defer registered.RUnlock()
	return len(registered.labels) > 0
}

// UnregisterValue forgets a value, once the host no longer holds it.
func UnregisterValue(value string) {
	registered.Lock()
	defer registered.Unlock()
	delete(registered.labels, value)
}

// MaskRegistered replaces every registered value in s with [secret:LABEL]: the
// value as written, JSON-escaped, and URL-encoded. Longer values go first, so a
// value that contains another is masked whole.
func MaskRegistered(s string) string {
	registered.RLock()
	defer registered.RUnlock()
	if len(registered.labels) == 0 || s == "" {
		return s
	}
	values := make([]string, 0, len(registered.labels))
	for v := range registered.labels {
		values = append(values, v)
	}
	slices.SortFunc(values, func(a, b string) int { return len(b) - len(a) })
	for _, v := range values {
		mark := "[secret:" + registered.labels[v] + "]"
		for _, form := range forms(v) {
			if !strings.Contains(s, form) {
				continue
			}
			if len(v) < shortValue {
				s = standalone(form).ReplaceAllString(s, "${1}"+mark+"${2}")
				continue
			}
			s = strings.ReplaceAll(s, form, mark)
		}
	}
	return s
}

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

func standalone(form string) *regexp.Regexp {
	return regexp.MustCompile(`(^|[^A-Za-z0-9_])` + regexp.QuoteMeta(form) + `([^A-Za-z0-9_]|$)`)
}
