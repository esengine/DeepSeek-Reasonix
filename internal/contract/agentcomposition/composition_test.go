package agentcomposition

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestDefinitionsValidateIdentityAndSelections(t *testing.T) {
	d := Builtins()[0]
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	d.ID = "user:../escape"
	if !errors.Is(d.Validate(), ErrInvalid) {
		t.Fatal("path identity accepted")
	}
	d = Builtins()[1]
	d.Skills = Selection{Mode: Select, Members: []Module{{ID: "one"}, {ID: "one"}}}
	if !errors.Is(d.Validate(), ErrInvalid) {
		t.Fatal("duplicate member accepted")
	}
}

func TestRevisionCanonicalizesParameterObjects(t *testing.T) {
	a := Builtins()[0]
	a.MCP = Selection{Mode: Select, Members: []Module{{ID: "example", Parameters: []byte(`{"b":2,"a":1}`)}}}
	b := a
	b.MCP.Members = []Module{{ID: "example", Parameters: []byte(`{"a":1,"b":2}`)}}
	if Revision(a) != Revision(b) {
		t.Fatal("object order changed revision")
	}
	b.Name = "Changed"
	if Revision(a) == Revision(b) {
		t.Fatal("definition edit did not change revision")
	}
}

func TestMinimalTemplateHasNoAutomationOrDelegation(t *testing.T) {
	definitions := Builtins()
	if len(definitions) != 2 || definitions[0].Name != "日常搭档" || definitions[1].Name != "轻装搭档" {
		t.Fatalf("built-in modes = %+v", definitions)
	}
	for _, d := range Builtins()[1:] {
		for _, s := range []Selection{d.Skills, d.MCP, d.Extensions, d.Hooks, d.Commands} {
			if s.Mode != Select || len(s.Members) != 0 {
				t.Fatalf("%s inherits modules", d.ID)
			}
		}
		if d.Tools.Mode != Select || len(d.Tools.Members) != 2 || d.Tools.Members[0].ID != "bash" || d.Tools.Members[0].Optional || d.Tools.Members[1].ID != "view_image" || !d.Tools.Members[1].Optional {
			t.Fatalf("minimal tools = %+v", d.Tools)
		}
	}
}

func TestOptionalModulesDecodeAndValidate(t *testing.T) {
	for _, group := range []string{"tools", "skills", "mcp", "extensions", "hooks", "commands"} {
		t.Run(group, func(t *testing.T) {
			d := Builtins()[0]
			data, err := json.Marshal(d)
			if err != nil {
				t.Fatal(err)
			}
			var document map[string]json.RawMessage
			if err := json.Unmarshal(data, &document); err != nil {
				t.Fatal(err)
			}
			document[group] = json.RawMessage(`{"mode":"select","members":[{"id":"example","optional":true}]}`)
			data, err = json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := Decode(data)
			if group != "tools" {
				if !errors.Is(err, ErrInvalid) {
					t.Fatalf("optional %s decode error = %v", group, err)
				}
				if !errors.Is(decoded.Validate(), ErrInvalid) {
					t.Fatalf("optional %s passed structural validation", group)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !decoded.Tools.Members[0].Optional {
				t.Fatal("optional tool lost during decode")
			}
		})
	}
}

func TestOptionalToolsBindingRoundTrip(t *testing.T) {
	d := Builtins()[1]
	binding := Binding{
		Resolved: d,
		Version:  Version{Definition: d, Revision: Revision(d)},
	}
	binding.Seal()
	restored := Clone(binding)
	if err := restored.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, definition := range []Definition{restored.Resolved, restored.Version.Definition} {
		if definition.Tools.Members[0].Optional || !definition.Tools.Members[1].Optional {
			t.Fatalf("tool requirements changed during binding roundtrip: %+v", definition.Tools)
		}
	}
	restored.Resolved.Tools.Members[1].Optional = false
	if !errors.Is(restored.Validate(), ErrInvalid) {
		t.Fatal("changing optional requirement did not invalidate binding digest")
	}
}

func TestAbsentOptionalPreservesRevisionAndBindingDigest(t *testing.T) {
	// A legacy declaration has no optional field; false must keep that encoding.
	d := Builtins()[0]
	d.Tools = Selection{Mode: Select, Members: []Module{{ID: "bash"}}}
	data, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var legacy map[string]json.RawMessage
	if err := json.Unmarshal(data, &legacy); err != nil {
		t.Fatal(err)
	}
	legacy["tools"] = json.RawMessage(`{"mode":"select","members":[{"id":"bash"}]}`)
	if Revision(d) != Digest(legacy) {
		t.Fatal("absent optional changed legacy revision")
	}
	decoded, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Tools.Members[0].Optional {
		t.Fatal("explicit tool became optional by default")
	}
	binding := Binding{Resolved: decoded, Version: Version{Definition: decoded, Revision: Revision(decoded)}}
	binding.Seal()
	var legacyBinding map[string]json.RawMessage
	data, err = json.Marshal(binding)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &legacyBinding); err != nil {
		t.Fatal(err)
	}
	legacyData, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	legacyBinding["resolved"] = legacyData
	versionData, err := json.Marshal(map[string]any{"definition": legacy, "revision": Revision(decoded)})
	if err != nil {
		t.Fatal(err)
	}
	legacyBinding["version"] = versionData
	legacyBinding["digest"] = json.RawMessage(`""`)
	if binding.Digest != Digest(legacyBinding) {
		t.Fatal("absent optional changed legacy binding digest")
	}
}
