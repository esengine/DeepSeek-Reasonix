package jsonschema_test

import (
	"fmt"
	"log"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"golang.org/x/text/message"
)

// SchemaExt --

type uniqueKeys struct {
	pname string
}

func (s *uniqueKeys) Validate(ctx *jsonschema.ValidatorContext, v any) {
	arr, ok := v.([]any)
	if !ok {
		return
	}
	var keys []any
	for _, item := range arr {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		key, ok := obj[s.pname]
		if ok {
			keys = append(keys, key)
		}
	}

	i, j, err := ctx.Duplicates(keys)
	if err != nil {
		ctx.AddErr(err)
		return
	}
	if i != -1 {
		ctx.AddError(&UniqueKeys{Key: s.pname, Duplicates: []int{i, j}})
	}
}

// Vocab --

func uniqueKeysVocab() *jsonschema.Vocabulary {
	url := "http://example.com/meta/unique-keys"
	schema, err := jsonschema.UnmarshalJSON(strings.NewReader(`{
		"properties": {
			"uniqueKeys": { "type": "string" }
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
		URL:     url,
		Schema:  sch,
		Compile: compileUniqueKeys,
	}
}

func compileUniqueKeys(ctx *jsonschema.CompilerContext, obj map[string]any) (jsonschema.SchemaExt, error) {
	v, ok := obj["uniqueKeys"]
	if !ok {
		return nil, nil
	}
	s, ok := v.(string)
	if !ok {
		return nil, nil
	}
	return &uniqueKeys{pname: s}, nil
}

// ErrorKind --

type UniqueKeys struct {
	Key        string
	Duplicates []int
}

func (*UniqueKeys) KeywordPath() []string {
	return []string{"uniqueKeys"}
}

func (k *UniqueKeys) LocalizedString(p *message.Printer) string {
	return p.Sprintf("items at %d and %d have same %s", k.Duplicates[0], k.Duplicates[1], k.Key)
}

// Example --

func Example_vocab_uniquekeys() {
	schema, err := jsonschema.UnmarshalJSON(strings.NewReader(`{
		"uniqueKeys": "id"
	}`))
	if err != nil {
		log.Fatal(err)
	}
	inst, err := jsonschema.UnmarshalJSON(strings.NewReader(`[
		{ "id": 1, "name": "alice" },
		{ "id": 2, "name": "bob" },
		{ "id": 1, "name": "scott" }
	]`))
	if err != nil {
		log.Fatal(err)
	}

	c := jsonschema.NewCompiler()
	c.AssertVocabs()
	c.RegisterVocabulary(uniqueKeysVocab())
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
