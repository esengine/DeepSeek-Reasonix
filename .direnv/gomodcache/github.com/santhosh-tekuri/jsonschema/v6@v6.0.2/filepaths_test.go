package jsonschema_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestPath_Absolute(t *testing.T) {
	path, err := filepath.Abs("./testdata/examples/schema.json")
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	if _, err := c.Compile(path); err != nil {
		t.Fatal(err)
	}
}

func TestPath_AbsoluteSpace(t *testing.T) {
	path, err := filepath.Abs("./testdata/examples/sample schema.json")
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	if _, err := c.Compile(path); err != nil {
		t.Fatal(err)
	}
}

func TestPath_RelativeSlash(t *testing.T) {
	path := "./testdata/examples/schema.json"
	c := jsonschema.NewCompiler()
	if _, err := c.Compile(path); err != nil {
		t.Fatal(err)
	}
}

func TestPath_RelativeSlashSpace(t *testing.T) {
	path := "./testdata/examples/sample schema.json"
	c := jsonschema.NewCompiler()
	if _, err := c.Compile(path); err != nil {
		t.Fatal(err)
	}
}

func TestPath_RelativeBackslash(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("runs only on windows")
	}
	path := `.\testdata\examples\schema.json`
	c := jsonschema.NewCompiler()
	if _, err := c.Compile(path); err != nil {
		t.Fatal(err)
	}
}

func TestPath_RelativeBackslashSpace(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("runs only on windows")
	}
	path := `./testdata\examples\sample schema.json`
	c := jsonschema.NewCompiler()
	if _, err := c.Compile(path); err != nil {
		t.Fatal(err)
	}
}
