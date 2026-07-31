package jsonschema_test

import (
	"fmt"
	"log"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// SchemaExt --

type discriminator struct {
	pname  string
	values map[string]*jsonschema.Schema
}

func (d *discriminator) Validate(ctx *jsonschema.ValidatorContext, v any) {
	obj, ok := v.(map[string]any)
	if !ok {
		return
	}
	pvalue, ok := obj[d.pname]
	if !ok {
		return
	}
	value, ok := pvalue.(string)
	if !ok {
		return
	}
	sch := d.values[value]
	if sch == nil {
		return
	}
	if err := ctx.Validate(sch, v, nil); err != nil {
		ctx.AddErr(err)
	} else {
		ctx.EvaluatedProp(d.pname)
	}
}

// Vocab --

func discriminatorVocab() *jsonschema.Vocabulary {
	url := "http://example.com/meta/discriminator"
	schema, err := jsonschema.UnmarshalJSON(strings.NewReader(`{
		"properties" : {
			"discriminator": {
				"type": "object",
				"minProperties": 1,
				"maxProperties": 1,
				"patternProperties": {
					".*": {
						"type": "object",
						"patternProperties": {
							".*": {
								"$ref": "https://json-schema.org/draft/2020-12/schema"
							}
						}
					}
				}
			}
		}
	}`))
	if err != nil {
		log.Fatal(err)
	}

	c := jsonschema.NewCompiler()
	if err := c.AddResource(url, schema); err != nil {
		log.Fatal(err)
	}
	sch, err := c.Compile(url)
	if err != nil {
		log.Fatal(err)
	}

	return &jsonschema.Vocabulary{
		URL:    url,
		Schema: sch,
		Subschemas: []jsonschema.SchemaPath{
			{jsonschema.Prop("discriminator"), jsonschema.AllProp{}, jsonschema.AllProp{}},
		},
		Compile: compileDiscriminator,
	}
}

func compileDiscriminator(ctx *jsonschema.CompilerContext, obj map[string]any) (jsonschema.SchemaExt, error) {
	v, ok := obj["discriminator"]
	if !ok {
		return nil, nil
	}
	d, ok := v.(map[string]any)
	if !ok {
		return nil, nil
	}
	var pname string
	var pvalue any
	for key, value := range d {
		pname = key
		pvalue = value
		break
	}
	values := map[string]*jsonschema.Schema{}
	vm, ok := pvalue.(map[string]any)
	if !ok {
		return nil, nil
	}
	for value := range vm {
		values[value] = ctx.Enqueue([]string{"discriminator", pname, value})
	}
	return &discriminator{pname, values}, nil
}

// Example --

func Example_vocab_discriminator() {
	// if kind is fish, swimmingSpeed is required
	// if kind is dog, runningSpeed is required
	schema, err := jsonschema.UnmarshalJSON(strings.NewReader(`{
		"type": "object",
		"properties": {
			"kind": { "type": "string" }
		},
		"required": ["kind"],
		"discriminator": {
			"kind": {
				"fish": {
					"type": "object",
					"properties": {
						"swimmingSpeed": { "type": "number" }
					},
					"required": ["swimmingSpeed"]
				},
				"dog": {
					"type": "object",
					"properties": {
						"runningSpeed": { "type": "number" }
					},
					"required": ["runningSpeed"]
				}
			}
		}
	}`))
	if err != nil {
		log.Fatal(err)
	}
	inst, err := jsonschema.UnmarshalJSON(strings.NewReader(`{
		"kind": "fish",
		"runningSpeed": 5
	}`))
	if err != nil {
		log.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	c.AssertVocabs()
	c.RegisterVocabulary(discriminatorVocab())
	if err := c.AddResource("schema.json", schema); err != nil {
		log.Fatal(err)
	}
	sch, err := c.Compile("schema.json")
	if err != nil {
		log.Fatal(err)
	}

	err = sch.Validate(inst)
	fmt.Println("valid:", err == nil)
	// Output:
	// valid: false
}
