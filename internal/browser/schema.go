package browser

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const operationIDPattern = `^[A-Za-z0-9_-]{1,100}$`

var operationIDRe = regexp.MustCompile(operationIDPattern)

// property is one schema entry. Keywords live in a map so json.Marshal emits
// them in sorted order, which is exactly provider.CanonicalizeSchema's form.
type property struct {
	name   string
	schema map[string]any
}

func str(name, desc string) property {
	return property{name, map[string]any{"type": "string", "description": desc}}
}

func boolean(name, desc string) property {
	return property{name, map[string]any{"type": "boolean", "description": desc}}
}

func integer(name, desc string) property {
	return property{name, map[string]any{"type": "integer", "description": desc}}
}

func bounded(p property, minimum, maximum int) property {
	p.schema["minimum"] = minimum
	p.schema["maximum"] = maximum
	return p
}

func strList(name, desc string) property {
	return property{name, map[string]any{"type": "array", "description": desc, "items": map[string]any{"type": "string"}, "minItems": 1}}
}

func enum(name, desc string, values ...string) property {
	vals := make([]any, len(values))
	for i, v := range values {
		vals[i] = v
	}
	return property{name, map[string]any{"type": "string", "description": desc, "enum": vals}}
}

func pattern(p property, re string) property {
	p.schema["pattern"] = re
	return p
}

// objectSchema marshals a closed object schema whose bytes already equal the
// canonical form: map keys sort under json.Marshal and required sorts here.
func objectSchema(required []string, props ...property) json.RawMessage {
	properties := make(map[string]any, len(props))
	for _, p := range props {
		properties[p.name] = p.schema
	}
	req := append([]string{}, required...)
	sort.Strings(req)
	b, err := json.Marshal(map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           properties,
		"required":             req,
	})
	if err != nil {
		panic("browser: static schema failed to marshal: " + err.Error())
	}
	return b
}

func tabIDProp() property {
	return str("tabId", "ID of the tab, as returned by browser_tabs or browser_open.")
}

func operationIDProp() property {
	return pattern(str("operationId", "Unique ID for this attempt: letters, digits, '_' or '-', at most 100 characters. Mint a fresh one for every write call and never reuse it. A reused ID is rejected, and an ID whose outcome came back unknown must not be retried."), operationIDPattern)
}

func documentTokenProp() property {
	return str("documentToken", "documentToken from the browser_snapshot this action was planned against. A navigation, page replacement, or user take-over invalidates it; when the call is blocked as stale, take a new snapshot instead of guessing.")
}

func refProp(desc string) property { return str("ref", desc) }

// decode parses args into dst, rejecting fields the schema does not declare
// so additionalProperties:false holds at runtime as well as on paper.
func decode(args json.RawMessage, dst any) error {
	if len(bytes.TrimSpace(args)) == 0 {
		args = json.RawMessage(`{}`)
	}
	dec := json.NewDecoder(bytes.NewReader(args))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("invalid args: %w", err)
	}
	return nil
}

func requireTab(id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("tabId is required; call browser_tabs to find one")
	}
	return nil
}

func requireOperationID(id string) error {
	if !operationIDRe.MatchString(id) {
		return fmt.Errorf("operationId must match %s; mint a fresh one per attempt", operationIDPattern)
	}
	return nil
}

func requireDocumentToken(token string) error {
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("documentToken is required; take a browser_snapshot and pass its documentToken")
	}
	return nil
}

func requireRef(ref string) error {
	if strings.TrimSpace(ref) == "" {
		return fmt.Errorf("ref is required; use an element ref from browser_snapshot")
	}
	return nil
}

func requireStrings(name string, values []string) error {
	if len(values) == 0 {
		return fmt.Errorf("%s must list at least one entry", name)
	}
	for _, v := range values {
		if strings.TrimSpace(v) == "" {
			return fmt.Errorf("%s must not contain empty entries", name)
		}
	}
	return nil
}
