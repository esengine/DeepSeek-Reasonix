package provider

import (
	"bytes"
	"encoding/json"
	"net/url"
	"strings"
)

// IsMiMoEndpoint reports whether rawURL points at an official Xiaomi MiMo API
// host, including the regional token-plan subdomains. The bare apex is rejected
// because it is not an API endpoint.
func IsMiMoEndpoint(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host != "xiaomimimo.com" && strings.HasSuffix(host, ".xiaomimimo.com")
}

// NormalizeLegacyTupleItemsForDraft202012 rewrites only the pre-2020-12 tuple
// keywords in a JSON Schema. It is intentionally separate from
// CanonicalizeSchema: provider implementations must opt in only after the target
// endpoint's schema dialect is known, so other vendors keep their original tool
// schema bytes and cache prefixes. A schema that needs no rewrite is returned
// with its input bytes unchanged, byte for byte.
//
// There is deliberately no memoization: MCP schemas are externally supplied
// bytes, and a process-global cache keyed by them would accumulate without
// bound across projects, sessions, and server reconnects in a long-lived
// desktop process. The lexical gate below keeps the common no-op case at zero
// parses, and the rare legacy-tuple schema is cheap to re-convert per request.
func NormalizeLegacyTupleItemsForDraft202012(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	// Legacy tuple syntax cannot exist without an "items" keyword; the common
	// no-op case skips even the parse.
	if !bytes.Contains(raw, []byte(`"items"`)) {
		return raw
	}
	var schema any
	if err := json.Unmarshal(raw, &schema); err != nil {
		return raw
	}
	if _, documentChanged := normalizeDraft202012Schema(schema); !documentChanged {
		return raw
	}
	out, err := json.Marshal(schema)
	if err != nil {
		return raw
	}
	return json.RawMessage(out)
}

// normalizeDraft202012Schema rewrites legacy tuple keywords in place. It
// returns two signals: resourceChanged reports conversions that belong to the
// CALLER's schema resource (the node itself plus descendants up to the next
// $schema boundary), documentChanged reports conversions anywhere in the
// subtree and drives reserialization.
//
// The distinction is what keeps dialect updates inside their own resource: an
// object carrying its own $schema is an independent schema resource, so its
// conversions update its own old-draft declaration and then stop — they must
// not mark the parent as changed, or a 2019-09 parent embedding a converting
// 2020-12 $defs resource would have its untouched declaration "upgraded" and
// the semantics of unrelated parent keywords silently changed.
func normalizeDraft202012Schema(value any) (resourceChanged, documentChanged bool) {
	schema, ok := value.(map[string]any)
	if !ok {
		return false, false
	}
	// Classify the resource's own dialect BEFORE touching anything: for an
	// unknown/custom $schema this code cannot know whether the dialect defines
	// 2020-12 tuple semantics, and a partial rewrite (keywords converted,
	// declaration kept) would be self-contradictory in the other direction.
	// JSON Schema requires processors to switch modes per dialect or leave the
	// resource alone — so the whole custom resource, subtree included, stays
	// untouched. Known legacy drafts and 2020-12 itself (where array-form
	// items is simply malformed input worth repairing) proceed.
	decl, hasDecl := schema["$schema"].(string)
	if hasDecl && !isLegacyJSONSchemaDialect(decl) && !isDraft202012Dialect(decl) {
		return false, false
	}
	changed := false // within this resource, up to nested $schema boundaries
	doc := false
	visit := func(child any) {
		childResource, childDocument := normalizeDraft202012Schema(child)
		changed = changed || childResource
		doc = doc || childDocument
	}

	for _, keyword := range []string{
		"additionalItems", "additionalProperties", "contains", "contentSchema",
		"else", "if", "items", "not", "propertyNames", "then",
		"unevaluatedItems", "unevaluatedProperties",
	} {
		visit(schema[keyword])
	}
	for _, keyword := range []string{"allOf", "anyOf", "oneOf", "prefixItems"} {
		if children, ok := schema[keyword].([]any); ok {
			for _, child := range children {
				visit(child)
			}
		}
	}
	for _, keyword := range []string{
		"$defs", "definitions", "dependentSchemas", "patternProperties", "properties",
	} {
		if children, ok := schema[keyword].(map[string]any); ok {
			for _, child := range children {
				visit(child)
			}
		}
	}
	if dependencies, ok := schema["dependencies"].(map[string]any); ok {
		for _, child := range dependencies {
			visit(child)
		}
	}

	if legacyItems, ok := schema["items"].([]any); ok {
		for _, child := range legacyItems {
			visit(child)
		}
		changed = true
		delete(schema, "items")
		if len(legacyItems) > 0 {
			// Keep an explicit 2020-12 prefix if a malformed mixed-dialect schema
			// contains both forms.
			if _, exists := schema["prefixItems"]; !exists {
				schema["prefixItems"] = legacyItems
			}
		}
		if additional, exists := schema["additionalItems"]; exists {
			delete(schema, "additionalItems")
			if isSchemaObjectOrBool(additional) {
				schema["items"] = additional
			}
		}
	}

	if changed {
		doc = true
		if hasDecl && isLegacyJSONSchemaDialect(decl) {
			schema["$schema"] = "https://json-schema.org/draft/2020-12/schema"
		}
	}
	if hasDecl {
		// Resource boundary: this resource's changes (and its declaration
		// update) are settled here and must not leak into the parent.
		return false, doc
	}
	return changed, doc
}

// isLegacyJSONSchemaDialect reports whether decl names a pre-2020-12 JSON
// Schema dialect. Unknown or custom dialect URIs make the whole resource
// off-limits — this normalizer only understands the official drafts' tuple
// semantics.
func isLegacyJSONSchemaDialect(decl string) bool {
	switch normalizeDialectURI(decl) {
	case "json-schema.org/schema",
		"json-schema.org/draft-03/schema",
		"json-schema.org/draft-04/schema",
		"json-schema.org/draft-06/schema",
		"json-schema.org/draft-07/schema",
		"json-schema.org/draft/2019-09/schema":
		return true
	}
	return false
}

// isDraft202012Dialect reports whether decl names the 2020-12 dialect itself.
func isDraft202012Dialect(decl string) bool {
	return normalizeDialectURI(decl) == "json-schema.org/draft/2020-12/schema"
}

func normalizeDialectURI(decl string) string {
	d := strings.TrimSuffix(strings.TrimSpace(decl), "#")
	d = strings.TrimPrefix(d, "http://")
	return strings.TrimPrefix(d, "https://")
}

func isSchemaObjectOrBool(value any) bool {
	switch value.(type) {
	case map[string]any, bool:
		return true
	default:
		return false
	}
}
