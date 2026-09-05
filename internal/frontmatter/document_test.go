package frontmatter

import (
	"fmt"
	"maps"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func doc(t *testing.T, fm string) Document {
	t.Helper()
	d, _ := Parse("---\n" + fm + "---\nbody\n")
	return d
}

// 1. A nested field keeps the key it was written under. This is the whole
// point: "who owns this field" is a fact about the document, and the flat view
// is where it used to be destroyed.
func TestNestedFieldKeepsItsParent(t *testing.T) {
	d := doc(t, "delivery:\n  review-report: security\n")
	v, ok := d.Lookup("delivery", "review-report")
	if !ok || v.Kind != KindScalar || v.Scalar != "security" {
		t.Fatalf("delivery.review-report = %+v ok=%v", v, ok)
	}
	if d.Has("review-report") {
		t.Fatal("a nested field must not also appear at the top level")
	}
	if !d.Has("delivery") {
		t.Fatal("the parent namespace must be visible")
	}
}

// 2. A quoted key that merely looks like a path is one top-level key, and is
// not the nested form. Without this the namespace is a spelling convention.
func TestAQuotedCompoundKeyIsNotANestedField(t *testing.T) {
	d := doc(t, "\"delivery.review-report\": security\n")
	if d.Has("delivery", "review-report") {
		t.Fatal("a quoted compound key resolved as a nested path")
	}
	if d.Has("delivery") {
		t.Fatal("a quoted compound key created a parent namespace")
	}
	v, ok := d.Lookup("delivery.review-report")
	if !ok || v.Scalar != "security" {
		t.Fatalf("the compound key is one top-level key: %+v ok=%v", v, ok)
	}
}

// 3. A subtree survives with its shape: the parent is visible and the sequence
// under it is still a sequence, not a comma-joined string.
func TestSubtreeKeepsParentAndSequence(t *testing.T) {
	d := doc(t, "authority:\n  satisfies:\n    - review\n    - security\n")
	if !d.Has("authority") {
		t.Fatal("the authority namespace must be visible to be refusable")
	}
	v, ok := d.Lookup("authority", "satisfies")
	if !ok || v.Kind != KindSequence {
		t.Fatalf("authority.satisfies = %+v ok=%v, want a sequence", v, ok)
	}
	if len(v.Items) != 2 || v.Items[0].Scalar != "review" || v.Items[1].Scalar != "security" {
		t.Fatalf("items = %+v", v.Items)
	}
	// A string that happens to contain a comma is a different document.
	s := doc(t, "authority:\n  satisfies: \"review, security\"\n")
	sv, _ := s.Lookup("authority", "satisfies")
	if sv.Kind != KindScalar {
		t.Fatalf("a quoted scalar became %v; a sequence and a comma string must stay distinct", sv.Kind)
	}
}

// 4. The same leaf name under two parents is two fields, not a collision.
func TestSameLeafUnderDifferentParentsDoesNotCollide(t *testing.T) {
	d := doc(t, "delivery:\n  kind: review\nsomething:\n  kind: unrelated\n")
	a, _ := d.Lookup("delivery", "kind")
	b, _ := d.Lookup("something", "kind")
	if a.Scalar != "review" || b.Scalar != "unrelated" {
		t.Fatalf("delivery.kind=%q something.kind=%q — one parent overwrote the other", a.Scalar, b.Scalar)
	}
}

// 6. Shapes an external file can really write, that a flattened view cannot
// tell from an absent field. Each must be present and distinguishable.
func TestEmptyShapesAreRepresentable(t *testing.T) {
	for _, tc := range []struct {
		name, fm string
		kind     Kind
	}{
		{"empty mapping", "authority: {}\n", KindMapping},
		{"empty sequence", "authority: []\n", KindSequence},
		{"bare key", "authority:\n", KindScalar},
		{"empty string", "authority: \"\"\n", KindScalar},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := doc(t, tc.fm)
			v, ok := d.Lookup("authority")
			if !ok {
				t.Fatal("the author named the namespace and it vanished")
			}
			if v.Kind != tc.kind {
				t.Fatalf("kind = %v, want %v", v.Kind, tc.kind)
			}
			if _, present := d.LegacyFlat()["authority"]; present {
				t.Fatal("the flat view is expected to lose this; that is why new semantics may not read it")
			}
		})
	}
	if doc(t, "name: x\n").Has("authority") {
		t.Fatal("an absent namespace must not be reported present")
	}
}

// 5. The compatibility view is unchanged, held against the algorithm it
// replaced rather than against a fresh reading of it. The legacy walk is kept
// here, in the test that exists to catch a difference.
func TestFlatViewMatchesTheLegacyWalkByteForByte(t *testing.T) {
	corpus := []string{
		"", "no fence at all\n", "---\nunclosed\n",
		"---\nname: x\ndescription: y\n---\nbody\n",
		"---\nallowed-tools:\n  - read_file\n  - grep\n---\nb\n",
		"---\nallowed-tools: read_file, grep\n---\nb\n",
		"---\nargument-hint:\n  - a\n  - b\n---\nb\n",
		"---\nmetadata:\n  type: user\n  scope: global\n---\nb\n",
		"---\na:\n  b:\n    c: deep\n---\nb\n",
		"---\nname: first\nname: second\n---\nb\n",
		"---\nempty:\nblank: \"\"\nzero: 0\nflag: true\n---\nb\n",
		"---\nlist:\n  - {k: v}\n  - plain\n---\nb\n",
		"---\nmap: {}\nseq: []\n---\nb\n",
		"---\n\"delivery.review-report\": security\n---\nb\n",
		"---\ndelivery:\n  review-report: security\n---\nb\n",
		"---\n  SPACED  :   Value  \n---\nb\n",
		"---\n- not a mapping\n---\nb\n",
		"---\n: novalue\n---\nb\n",
		"---\r\nname: crlf\r\n---\r\nbody\r\n",
		"---\n: broken: [yaml\n---\nb\n",
	}
	for i, in := range corpus {
		got, gotBody := SplitLegacy(in)
		want, wantBody := legacySplit(in)
		if !maps.Equal(got, want) {
			t.Errorf("corpus[%d] flat view drifted\n in:   %q\n got:  %v\n want: %v", i, in, got, want)
		}
		if gotBody != wantBody {
			t.Errorf("corpus[%d] body drifted\n got:  %q\n want: %q", i, gotBody, wantBody)
		}
	}
}

// legacySplit is the flattening parser Split used before the canonical document
// existed, kept verbatim so a drift in the compatibility view fails here.
func legacySplit(s string) (map[string]string, string) {
	fm := map[string]string{}
	raw, body, ok := splitRaw(s)
	if !ok {
		return fm, body
	}
	if strings.TrimSpace(raw) == "" {
		return fm, body
	}
	var d yaml.Node
	if err := yaml.Unmarshal([]byte(raw), &d); err != nil {
		return fm, body
	}
	root := mappingRoot(&d)
	if root == nil {
		return fm, body
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		key := normalizeKey(root.Content[i].Value)
		if key == "" {
			continue
		}
		legacyAdd(fm, key, root.Content[i+1])
	}
	return fm, body
}

func legacyAdd(out map[string]string, key string, value *yaml.Node) {
	switch {
	case value == nil:
		return
	case value.Kind == yaml.MappingNode:
		for i := 0; i+1 < len(value.Content); i += 2 {
			nested := normalizeKey(value.Content[i].Value)
			if nested == "" {
				continue
			}
			legacyAdd(out, nested, value.Content[i+1])
		}
	case value.Kind == yaml.SequenceNode:
		items := make([]string, 0, len(value.Content))
		for _, item := range value.Content {
			if s := legacyScalar(item); s != "" {
				items = append(items, s)
			}
		}
		if len(items) > 0 {
			joined := strings.Join(items, ", ")
			if key == "argument-hint" {
				joined = "[" + joined + "]"
			}
			out[key] = joined
		}
	default:
		if s := legacyScalar(value); s != "" {
			out[key] = s
		}
	}
}

func legacyScalar(node *yaml.Node) string {
	if node == nil {
		return ""
	}
	if node.Kind != yaml.ScalarNode {
		return strings.TrimSpace(fmt.Sprint(node.Value))
	}
	return strings.TrimSpace(node.Value)
}
