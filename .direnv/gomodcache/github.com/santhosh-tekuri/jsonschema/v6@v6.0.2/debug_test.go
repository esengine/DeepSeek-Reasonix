package jsonschema_test

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestDebug(t *testing.T) {
	var test struct {
		Remotes map[string]any
		Schema  any
		Data    any
		Valid   bool
	}

	file, err := os.Open("./testdata/debug.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	dec := json.NewDecoder(file)
	dec.UseNumber()
	if err := dec.Decode(&test); err != nil {
		t.Fatal(err)
	}

	url := "http://debug.com/schema.json"
	c := jsonschema.NewCompiler()
	c.AssertFormat()
	c.AssertContent()
	c.UseLoader(debugRemotes(test.Remotes))
	if err := c.AddResource(url, test.Schema); err != nil {
		t.Fatalf("addResource failed: %v", err)
	}
	sch, err := c.Compile(url)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	err = sch.Validate(test.Data)
	if err != nil {
		for _, line := range strings.Split(fmt.Sprintf("%v", err), "\n") {
			t.Logf("        %s", line)
		}
		for _, line := range strings.Split(fmt.Sprintf("%#v", err), "\n") {
			t.Logf("        %s", line)
		}
	}
	if got := err == nil; got != test.Valid {
		t.Errorf("        valid: got %v, want %v", got, test.Valid)
		tsch, _ := json.Marshal(test.Schema)
		t.Log("schema:", string(tsch))
		data, _ := json.Marshal(test.Data)
		t.Log("data:", string(data))
		t.FailNow()
	}
}

type debugRemotes map[string]any

func (r debugRemotes) Load(url string) (any, error) {
	v, ok := r[url]
	if !ok {
		return nil, fmt.Errorf("no remote found for %q", url)
	}
	return v, nil
}
