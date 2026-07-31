package jsonschema_test

import (
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestMetaschemaResource(t *testing.T) {
	mainSchema, err := jsonschema.UnmarshalJSON(strings.NewReader(`{
        "$schema": "http://tmp.com/meta.json",
        "type": "number"
    }`))
	if err != nil {
		t.Fatal(err)
	}

	metaSchema, err := jsonschema.UnmarshalJSON(strings.NewReader(`{
        "$schema": "https://json-schema.org/draft/2020-12/schema",
        "$vocabulary": {
            "https://json-schema.org/draft/2020-12/vocab/applicator": true,
            "https://json-schema.org/draft/2020-12/vocab/core": true
        },
        "allOf": [
            { "$ref": "https://json-schema.org/draft/2020-12/meta/applicator" },
            { "$ref": "https://json-schema.org/draft/2020-12/meta/core" }
        ]
    }`))
	if err != nil {
		t.Fatal(err)
	}

	c := jsonschema.NewCompiler()
	if err := c.AddResource("schema.json", mainSchema); err != nil {
		t.Fatal(err)
	}
	if err := c.AddResource("http://tmp.com/meta.json", metaSchema); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Compile("schema.json"); err != nil {
		t.Fatal(err)
	}
}

func TestCompileAnchor(t *testing.T) {
	schema, err := jsonschema.UnmarshalJSON(strings.NewReader(`{
        "$schema": "https://json-schema.org/draft/2020-12/schema",
        "$defs": {
            "x": {
                "$anchor": "a1",
                "type": "number"
            }
        }
    }`))
	if err != nil {
		t.Fatal(err)
	}

	c := jsonschema.NewCompiler()
	if err := c.AddResource("schema.json", schema); err != nil {
		t.Fatal(err)
	}
	sch1, err := c.Compile("schema.json#a1")
	if err != nil {
		t.Fatal(err)
	}
	sch2, err := c.Compile("schema.json#/$defs/x")
	if err != nil {
		t.Fatal(err)
	}
	if sch1 != sch2 {
		t.Fatal("schemas did not match")
	}
}

func TestCompileNonStd(t *testing.T) {
	schema, err := jsonschema.UnmarshalJSON(strings.NewReader(`{
        "components": {
            "schemas": {
                "foo" : {
                    "$schema": "https://json-schema.org/draft/2020-12/schema",
                    "$defs": {
                        "x": {
                            "$anchor": "a",
                            "type": "number"
                        },
                        "y": {
                            "$id": "http://temp.com/y",
                            "type": "string"
                        }
                    },
                    "oneOf": [
                        { "$ref": "#a" },
                        { "$ref": "http://temp.com/y" }
                    ]
                }
            }
        }
    }`))
	if err != nil {
		t.Fatal(err)
	}

	c := jsonschema.NewCompiler()
	if err := c.AddResource("schema.json", schema); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Compile("schema.json#/components/schemas/foo"); err != nil {
		t.Fatal(err)
	}
}

func TestCustomVocabValidation(t *testing.T) {
	schema, err := jsonschema.UnmarshalJSON(strings.NewReader(`{"uniqueKeys": 1}`))
	if err != nil {
		t.Fatal(err)
	}

	c := jsonschema.NewCompiler()
	c.AssertVocabs()
	c.RegisterVocabulary(uniqueKeysVocab())
	if err := c.AddResource("schema.json", schema); err != nil {
		t.Fatal(err)
	}
	_, err = c.Compile("schema.json")
	if err == nil {
		t.Fatal("excepted compilation failure")
	}
}

func TestCustomVocabMetaschema(t *testing.T) {
	metaschema, err := jsonschema.UnmarshalJSON(strings.NewReader(`{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$vocabulary": {
			"http://example.com/meta/unique-keys": true,
			"https://json-schema.org/draft/2020-12/vocab/validation": true,
			"https://json-schema.org/draft/2020-12/vocab/core": true
		},
		"$dynamicAnchor": "meta",
		"allOf": [
			{ "$ref": "http://example.com/meta/unique-keys" },
			{ "$ref": "https://json-schema.org/draft/2020-12/meta/validation" },
			{ "$ref": "https://json-schema.org/draft/2020-12/meta/core" }
		]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	schema, err := jsonschema.UnmarshalJSON(strings.NewReader(`{
		"$schema": "http://temp.com/metaschema",
		"uniqueKeys": 1
	}`))
	if err != nil {
		t.Fatal(err)
	}

	c := jsonschema.NewCompiler()
	c.RegisterVocabulary(uniqueKeysVocab())
	if err := c.AddResource("http://temp.com/metaschema", metaschema); err != nil {
		t.Fatal(err)
	}
	if err := c.AddResource("invalid_schema.json", schema); err != nil {
		t.Fatal(err)
	}
	_, err = c.Compile("invalid_schema.json")
	if err == nil {
		t.Fatal("exception compilation failure")
	}
	if _, ok := err.(*jsonschema.SchemaValidationError); !ok {
		t.Log("want SchemaValidationError")
		t.Fatalf(" got %v", err)
	}

	schema, err = jsonschema.UnmarshalJSON(strings.NewReader(`{
		"$schema": "http://temp.com/metaschema",
		"uniqueKeys": "id"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	inst, err := jsonschema.UnmarshalJSON(strings.NewReader(`[
		{ "id": 1, "name": "alice" },
		{ "id": 2, "name": "bob" },
		{ "id": 1, "name": "scott" }
	]`))
	if err != nil {
		t.Fatal(err)
	}

	if err := c.AddResource("valid_schema.json", schema); err != nil {
		t.Fatal(err)
	}
	sch, err := c.Compile("valid_schema.json")
	if err != nil {
		t.Fatal(err)
	}

	err = sch.Validate(inst)
	if err == nil {
		t.Fatal("instance should be invalid")
	}
}

func TestCustomVocabSubschemaResource(t *testing.T) {
	schema, err := jsonschema.UnmarshalJSON(strings.NewReader(`{
		"discriminator": {
			"p1" : {
				"v1": {
					"$id": "http://temp.com/inner.json",
					"type": "string"
				}
			}
		},
		"$ref": "http://temp.com/inner.json"
	}`))
	if err != nil {
		t.Fatal(err)
	}

	c := jsonschema.NewCompiler()
	c.AssertVocabs()
	c.RegisterVocabulary(discriminatorVocab())
	if err := c.AddResource("schema.json", schema); err != nil {
		t.Fatal(err)
	}
	_, err = c.Compile("schema.json")
	if err != nil {
		t.Fatal(err)
	}
}

func TestNonJSONResource(t *testing.T) {
	c := jsonschema.NewCompiler()
	err := c.AddResource("schema.json", strings.NewReader("123"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Compile("schema.json")
	if err == nil {
		t.Fatal("compile must fail")
	}
}
