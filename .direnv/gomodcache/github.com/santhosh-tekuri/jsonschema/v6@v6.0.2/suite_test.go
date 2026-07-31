package jsonschema_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

var skip = []string{
	"ecmascript-regex.json",
	"zeroTerminatedFloats.json",
	"idn-email.json", "idn-hostname.json",
}

func testFile(t *testing.T, suite, fpath string, draft *jsonschema.Draft) {
	optional := strings.Contains(fpath, "/optional/")
	fpath = path.Join(suite, "tests", fpath)
	t.Log("FILE:", fpath)
	file, err := os.Open(fpath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	url := "http://testsuites.com/schema.json"
	var groups []struct {
		Description string
		Schema      any
		Tests       []struct {
			Description string
			Data        any
			Valid       bool
		}
	}
	dec := json.NewDecoder(file)
	dec.UseNumber()
	if err := dec.Decode(&groups); err != nil {
		t.Fatal(err)
	}

	for _, group := range groups {
		t.Log(group.Description)

		c := jsonschema.NewCompiler()
		c.DefaultDraft(draft)
		if optional {
			c.AssertFormat()
			c.AssertContent()
		}
		loader := jsonschema.SchemeURLLoader{
			"file": jsonschema.FileLoader{},
			"http": suiteRemotes(suite),
		}
		c.UseLoader(loader)

		if err := c.AddResource(url, group.Schema); err != nil {
			t.Fatalf("add resource failed: %v", err)
		}
		sch, err := c.Compile(url)
		if err != nil {
			t.Fatalf("schema compilation failed: %v", err)
		}
		for _, test := range group.Tests {
			t.Logf("    %s", test.Description)
			err := sch.Validate(test.Data)
			if err != nil {
				for _, line := range strings.Split(fmt.Sprintf("%v", err), "\n") {
					t.Logf("        %s", line)
				}
				for _, line := range strings.Split(fmt.Sprintf("%#v", err), "\n") {
					t.Logf("        %s", line)
				}
				if verr, ok := err.(*jsonschema.ValidationError); ok {
					detailed, err := json.MarshalIndent(verr.DetailedOutput(), "", "    ")
					if err != nil {
						t.Fatal(err)
					}
					t.Logf("detailed: %s", string(detailed))
					basic, err := json.MarshalIndent(verr.BasicOutput(), "", "    ")
					if err != nil {
						t.Fatal(err)
					}
					t.Logf("basic: %s", string(basic))
				}
			}
			if got := err == nil; got != test.Valid {
				t.Errorf("        valid: got %v, want %v", got, test.Valid)
				gsch, _ := json.Marshal(group.Schema)
				t.Log("schema:", string(gsch))
				data, _ := json.Marshal(test.Data)
				t.Log("data:", string(data))
				t.FailNow()
			}
		}
	}
}

func testDir(t *testing.T, suite, dpath string, draft *jsonschema.Draft) {
	dir := path.Join(suite, "tests", dpath)
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatal(err)
	}
	ee, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ee {
		if e.IsDir() {
			testDir(t, suite, path.Join(dpath, e.Name()), draft)
			continue
		}
		if path.Ext(e.Name()) != ".json" {
			continue
		}
		if slices.Contains(skip, e.Name()) {
			continue
		}
		testFile(t, suite, path.Join(dpath, e.Name()), draft)
	}
}

func testSuite(t *testing.T, suite string) {
	if _, err := os.Stat(suite); err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatal(err)
	}
	testDir(t, suite, "draft4", jsonschema.Draft4)
	testDir(t, suite, "draft6", jsonschema.Draft6)
	testDir(t, suite, "draft7", jsonschema.Draft7)
	testDir(t, suite, "draft2019-09", jsonschema.Draft2019)
	testDir(t, suite, "draft2020-12", jsonschema.Draft2020)
}

func TestSuites(t *testing.T) {
	testSuite(t, "./testdata/JSON-Schema-Test-Suite")
	testSuite(t, "./testdata/Extra-Test-Suite")
}

// --

type suiteRemotes string

func (rl suiteRemotes) Load(url string) (any, error) {
	if rem, ok := strings.CutPrefix(url, "http://localhost:1234/"); ok {
		f, err := os.Open(path.Join(string(rl), "remotes", rem))
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return jsonschema.UnmarshalJSON(f)
	}
	return nil, errors.New("no internet")
}
