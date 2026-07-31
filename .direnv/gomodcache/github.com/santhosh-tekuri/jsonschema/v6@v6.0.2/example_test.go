package jsonschema_test

import (
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func Example_fromFiles() {
	schemaFile := "./testdata/examples/schema.json"
	instanceFile := "./testdata/examples/instance.json"

	c := jsonschema.NewCompiler()
	sch, err := c.Compile(schemaFile)
	if err != nil {
		log.Fatal(err)
	}

	f, err := os.Open(instanceFile)
	if err != nil {
		log.Fatal(err)
	}
	inst, err := jsonschema.UnmarshalJSON(f)
	if err != nil {
		log.Fatal(err)
	}

	err = sch.Validate(inst)
	fmt.Println("valid:", err == nil)
	// Output:
	// valid: true
}

func Example_fromStrings() {
	catSchema, err := jsonschema.UnmarshalJSON(strings.NewReader(`{
        "type": "object",
        "properties": {
            "speak": { "const": "meow" }
        },
        "required": ["speak"]
    }`))
	if err != nil {
		log.Fatal(err)
	}
	// note that dog.json is loaded from file ./testdata/examples/dog.json
	petSchema, err := jsonschema.UnmarshalJSON(strings.NewReader(`{
        "oneOf": [
            { "$ref": "dog.json" },
            { "$ref": "cat.json" }
        ]
    }`))
	if err != nil {
		log.Fatal(err)
	}
	inst, err := jsonschema.UnmarshalJSON(strings.NewReader(`{"speak": "bow"}`))
	if err != nil {
		log.Fatal(err)
	}

	c := jsonschema.NewCompiler()
	if err := c.AddResource("./testdata/examples/cat.json", catSchema); err != nil {
		log.Fatal(err)
	}
	if err := c.AddResource("./testdata/examples/pet.json", petSchema); err != nil {
		log.Fatal(err)
	}
	sch, err := c.Compile("./testdata/examples/pet.json")
	if err != nil {
		log.Fatal(err)
	}
	err = sch.Validate(inst)
	fmt.Println("valid:", err == nil)
	// Output:
	// valid: true
}

func Example_customFormat() {
	validatePalindrome := func(v any) error {
		s, ok := v.(string)
		if !ok {
			return nil
		}
		var runes []rune
		for _, r := range s {
			runes = append(runes, r)
		}
		for i, j := 0, len(runes)-1; i <= j; i, j = i+1, j-1 {
			if runes[i] != runes[j] {
				return fmt.Errorf("no match for rune at %d", i)
			}
		}
		return nil
	}

	schema, err := jsonschema.UnmarshalJSON(strings.NewReader(`{"type": "string", "format": "palindrome"}`))
	if err != nil {
		log.Fatal(err)
	}
	inst := "hello world"

	c := jsonschema.NewCompiler()
	c.RegisterFormat(&jsonschema.Format{
		Name:     "palindrome",
		Validate: validatePalindrome,
	})
	c.AssertFormat()
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

// Example_customContentEncoding shows how to define
// "hex" contentEncoding.
func Example_customContentEndocing() {
	schema, err := jsonschema.UnmarshalJSON(strings.NewReader(`{"type": "string", "contentEncoding": "hex"}`))
	if err != nil {
		log.Fatal(err)
	}
	inst := "abcxyz"

	c := jsonschema.NewCompiler()
	c.RegisterContentEncoding(&jsonschema.Decoder{
		Name:   "hex",
		Decode: hex.DecodeString,
	})
	c.AssertContent()
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

// Example_customContentMediaType shows how to define
// "application/xml" contentMediaType.
func Example_customContentMediaType() {
	schema, err := jsonschema.UnmarshalJSON(strings.NewReader(`{"type": "string", "contentMediaType": "application/xml"}`))
	if err != nil {
		log.Fatal(err)
	}
	inst := "<abc></def>"

	c := jsonschema.NewCompiler()
	c.RegisterContentMediaType(&jsonschema.MediaType{
		Name: "application/xml",
		Validate: func(b []byte) error {
			return xml.Unmarshal(b, new(any))
		},
		UnmarshalJSON: nil, // xml is not json-compatible format
	})
	c.AssertContent()
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
