package jsonschema_test

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestInvalidSchemas(t *testing.T) {
	f, err := os.Open("./testdata/invalid_schemas.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var tests []struct {
		Description string
		Remotes     map[string]any
		Schema      any
		Errors      []string
	}

	dec := json.NewDecoder(f)
	dec.UseNumber()
	if err := dec.Decode(&tests); err != nil {
		t.Fatal(err)
	}

	for _, test := range tests {
		t.Log(test.Description)
		url := "http://invalid-schemas.com/schema.json"
		c := jsonschema.NewCompiler()
		loader := jsonschema.SchemeURLLoader{
			"file": jsonschema.FileLoader{},
			"http": invalidRemotes(test.Remotes),
		}
		c.UseLoader(loader)
		if err := c.AddResource(url, test.Schema); err != nil {
			t.Fatalf("addResource failed: %v", err)
		}
		_, err := c.Compile(url)
		if err == nil {
			if len(test.Errors) > 0 {
				t.Fatal("want compilation to fail")
			}
		} else {
			if len(test.Errors) == 0 {
				t.Log(err)
				t.Fatal("want compilation to succeed")
			}
			got := fmt.Sprintf("%#v", err)
			for _, want := range test.Errors {
				if !strings.Contains(got, want) {
					t.Errorf(" got %s", got)
					t.Fatalf("want %s", want)
				}
			}
		}
		t.Log()
	}
}

type invalidRemotes map[string]any

func (l invalidRemotes) Load(url string) (any, error) {
	if v, ok := l[url]; ok {
		return v, nil
	}
	return nil, fmt.Errorf("remote %q not found", url)
}
