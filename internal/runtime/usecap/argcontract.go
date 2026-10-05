package usecap

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// ArgumentContract is how a call compares against its target's own schema.
type ArgumentContract struct {
	Level   string   // the array property whose items were compared; empty is the call itself
	Accepts []string // every property the schema defines
	Unknown []string // supplied fields the schema does not define
	Missing []string // required fields the call omits
	// Mistyped are supplied fields the schema defines and the call filled with
	// the wrong kind of value. Neither unknown nor missing: the name is right
	// and it is there, so only the value can be.
	Mistyped []mistypedField
}

// mistypedField is one field whose value is not a kind its schema admits.
type mistypedField struct {
	Field string
	Want  []string
	Got   string
}

// schemaLevel is one object level of a JSON Schema: what it defines and what
// it insists on.
type schemaLevel struct {
	Properties map[string]json.RawMessage `json:"properties"`
	Required   []string                   `json:"required"`
}

// ReadArgumentContract compares arguments against schema, descending into array
// items once the call's own level is clean: a field named inside `tasks[]` has
// to be answered with an item's parameters, not with the outer object's. ok is
// false when either side is unreadable, which leaves the contract unknown rather
// than violated — a caller may not treat that as a broken call.
func ReadArgumentContract(schema, arguments json.RawMessage) (ArgumentContract, bool) {
	var declared schemaLevel
	if len(schema) == 0 || json.Unmarshal(schema, &declared) != nil || len(declared.Properties) == 0 {
		return ArgumentContract{}, false
	}
	var got map[string]json.RawMessage
	if len(arguments) > 0 && json.Unmarshal(arguments, &got) != nil {
		return ArgumentContract{}, false
	}
	contract := declared.compare(got, "")
	if contract.Broken() {
		return contract, true
	}
	for _, field := range slices.Sorted(maps.Keys(declared.Properties)) {
		if nested, found := itemContract(declared.Properties[field], got[field], field); found {
			return nested, true
		}
	}
	return contract, true
}

// kindMismatch reports whether value is a kind this property does not admit. A
// property that declares no type, and a null, leave the question unanswered:
// the contract may only report what the schema actually said.
func kindMismatch(property, value json.RawMessage) (want []string, got string, bad bool) {
	want = declaredTypes(property)
	got = jsonKind(value)
	if len(want) == 0 || got == "" || got == "null" {
		return nil, "", false
	}
	for _, kind := range want {
		if kind == got || (got == "number" && (kind == "number" || kind == "integer")) {
			return nil, "", false
		}
	}
	return want, got, true
}

// declaredTypes reads a property's "type", which JSON Schema allows to be one
// name or several.
func declaredTypes(property json.RawMessage) []string {
	var one struct {
		Type json.RawMessage `json:"type"`
	}
	if len(property) == 0 || json.Unmarshal(property, &one) != nil || len(one.Type) == 0 {
		return nil
	}
	var single string
	if json.Unmarshal(one.Type, &single) == nil {
		return []string{single}
	}
	var several []string
	if json.Unmarshal(one.Type, &several) == nil {
		return several
	}
	return nil
}

// jsonKind names the kind of an encoded value by its first token, which is what
// a JSON Schema type names too.
func jsonKind(value json.RawMessage) string {
	trimmed := strings.TrimSpace(string(value))
	if trimmed == "" {
		return ""
	}
	switch trimmed[0] {
	case '{':
		return "object"
	case '[':
		return "array"
	case '"':
		return "string"
	case 't', 'f':
		return "boolean"
	case 'n':
		return "null"
	default:
		return "number"
	}
}

// itemContract compares the supplied elements of an array property against that
// property's item schema and returns the first element that breaks it.
func itemContract(property, value json.RawMessage, field string) (ArgumentContract, bool) {
	var wrapper struct {
		Items json.RawMessage `json:"items"`
	}
	if len(property) == 0 || json.Unmarshal(property, &wrapper) != nil || len(wrapper.Items) == 0 {
		return ArgumentContract{}, false
	}
	var items schemaLevel
	if json.Unmarshal(wrapper.Items, &items) != nil || len(items.Properties) == 0 {
		return ArgumentContract{}, false
	}
	var elements []map[string]json.RawMessage
	if len(value) == 0 || json.Unmarshal(value, &elements) != nil {
		return ArgumentContract{}, false
	}
	for _, element := range elements {
		if c := items.compare(element, field); c.Broken() {
			return c, true
		}
	}
	return ArgumentContract{}, false
}

// kindPhrase reads a JSON Schema type list the way a sentence needs it.
func kindPhrase(kinds []string) string {
	articled := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		switch kind {
		case "array", "object", "integer":
			articled = append(articled, "an "+kind)
		default:
			articled = append(articled, "a "+kind)
		}
	}
	return strings.Join(articled, " or ")
}

func (c ArgumentContract) Broken() bool {
	return len(c.Unknown) > 0 || len(c.Missing) > 0 || len(c.Mistyped) > 0
}

// hint names the parameters the target accepts, but only when the call actually
// broke the contract. Staying silent on a well-formed call keeps the contract
// out of failures it cannot explain, such as one the target itself reported.
func (c ArgumentContract) Hint() string {
	if !c.Broken() {
		return ""
	}
	subject, owner := "this capability", "it"
	where := ""
	if c.Level != "" {
		subject = "a `" + c.Level + "` item"
		owner, where = subject, " in "+subject
	}
	var b strings.Builder
	if len(c.Unknown) > 0 {
		fmt.Fprintf(&b, "; %s is not a parameter of %s", strings.Join(quoted(c.Unknown), ", "), subject)
	}
	if len(c.Missing) > 0 {
		fmt.Fprintf(&b, "; %s requires %s", owner, strings.Join(quoted(c.Missing), ", "))
	}
	for _, m := range c.Mistyped {
		fmt.Fprintf(&b, "; %q%s must be %s, not %s", m.Field, where, kindPhrase(m.Want), kindPhrase([]string{m.Got}))
	}
	// A mistyped field is one the target accepts, so listing what it accepts
	// answers a question nobody asked.
	if len(c.Unknown) > 0 || len(c.Missing) > 0 {
		fmt.Fprintf(&b, "; %s accepts %s", owner, strings.Join(quoted(c.Accepts), ", "))
	}
	return b.String()
}

func (s schemaLevel) compare(got map[string]json.RawMessage, level string) ArgumentContract {
	contract := ArgumentContract{Level: level, Accepts: slices.Sorted(maps.Keys(s.Properties))}
	for _, field := range slices.Sorted(maps.Keys(got)) {
		property, defined := s.Properties[field]
		if !defined {
			contract.Unknown = append(contract.Unknown, field)
			continue
		}
		if want, actual, bad := kindMismatch(property, got[field]); bad {
			contract.Mistyped = append(contract.Mistyped, mistypedField{Field: field, Want: want, Got: actual})
		}
	}
	for _, field := range s.Required {
		if _, supplied := got[field]; !supplied {
			contract.Missing = append(contract.Missing, field)
		}
	}
	slices.Sort(contract.Unknown)
	slices.Sort(contract.Missing)
	return contract
}
