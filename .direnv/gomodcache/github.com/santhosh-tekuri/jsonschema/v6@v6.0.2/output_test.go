package jsonschema_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func testOutputDir(t *testing.T, suite, dir string, draft *jsonschema.Draft) {
	content := filepath.Join(suite, "output-tests", dir, "content")
	if _, err := os.Stat(content); err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatal(err)
	}

	f, err := os.Open(filepath.Join(suite, "output-tests", dir, "output-schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	outputSchema, err := jsonschema.UnmarshalJSON(f)
	if err != nil {
		t.Fatal(err)
	}
	outputSchemaURL := fmt.Sprintf("https://json-schema.org/draft/%s/output/schema", strings.TrimPrefix(dir, "draft"))

	ee, err := os.ReadDir(content)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ee {
		if e.IsDir() {
			continue
		}
		file := filepath.Join(content, e.Name())
		t.Logf("FILE: %s", file)
		var groups []struct {
			Description string
			Schema      any
			Tests       []struct {
				Description string
				Data        any
				Output      struct {
					Basic    any
					Detailed any
				}
			}
		}
		f, err := os.Open(file)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		dec := json.NewDecoder(f)
		dec.UseNumber()
		if err := dec.Decode(&groups); err != nil {
			t.Fatal(err)
		}

		for _, group := range groups {
			t.Logf("    %s", group.Description)
			c := jsonschema.NewCompiler()
			c.DefaultDraft(draft)
			url := "http://output-tests/schema.json"
			basicURL := "http://output-tests/basic.json"
			detailedURL := "http://output-tests/detailed.json"
			if err := c.AddResource(url, group.Schema); err != nil {
				t.Fatal(err)
			}
			if err := c.AddResource(outputSchemaURL, outputSchema); err != nil {
				t.Fatal(err)
			}
			sch, err := c.Compile(url)
			if err != nil {
				t.Fatal(err)
			}
			for _, test := range group.Tests {
				t.Logf("        %s", test.Description)
				verr := sch.Validate(test.Data)
				if verr == nil {
					t.Log("validation success")
					continue
				}
				if test.Output.Basic != nil {
					if err := c.AddResource(basicURL, test.Output.Basic); err != nil {
						t.Fatal(err)
					}
					sch, err := c.Compile(basicURL)
					if err != nil {
						t.Fatal(err)
					}
					b, err := json.Marshal(verr.(*jsonschema.ValidationError).BasicOutput())
					if err != nil {
						t.Fatal(err)
					}
					v, err := jsonschema.UnmarshalJSON(bytes.NewReader(b))
					if err != nil {
						t.Fatal(err)
					}
					if err := sch.Validate(v); err != nil {
						t.Fatal(err)
					}
				}
				if test.Output.Detailed != nil {
					if err := c.AddResource(detailedURL, test.Output.Detailed); err != nil {
						t.Fatal(err)
					}
					sch, err := c.Compile(detailedURL)
					if err != nil {
						t.Fatal(err)
					}
					b, err := json.Marshal(verr.(*jsonschema.ValidationError).DetailedOutput())
					if err != nil {
						t.Fatal(err)
					}
					v, err := jsonschema.UnmarshalJSON(bytes.NewReader(b))
					if err != nil {
						t.Fatal(err)
					}
					if err := sch.Validate(v); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
	}
}

func testOuputSuite(t *testing.T, suite string) {
	testOutputDir(t, suite, "draft2019-09", jsonschema.Draft2019)
	testOutputDir(t, suite, "draft2020-10", jsonschema.Draft2020)
}

func TestOutputSuites(t *testing.T) {
	testOuputSuite(t, "./testdata/JSON-Schema-Test-Suite")
	testOuputSuite(t, "./testdata/Extra-Test-Suite")
}
