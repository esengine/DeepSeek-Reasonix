package shellescape_test

import (
	"testing"

	"github.com/zalando/go-keyring/internal/shellescape"
)

func assertEqual(t *testing.T, s, expected string) {
	if s != expected {
		t.Fatalf("%q (expected: %q)", s, expected)
	}
}

func TestEmptyString(t *testing.T) {
	s := shellescape.Quote("")
	expected := "''"
	if s != expected {
		t.Errorf("Expected escaped string %s, got: %s", expected, s)
	}
}

func TestDoubleQuotedString(t *testing.T) {
	s := shellescape.Quote(`"double quoted"`)
	expected := `'"double quoted"'`
	if s != expected {
		t.Errorf("Expected escaped string %s, got: %s", expected, s)
	}
}

func TestSingleQuotedString(t *testing.T) {
	s := shellescape.Quote(`'single quoted'`)
	expected := `''"'"'single quoted'"'"''`
	if s != expected {
		t.Errorf("Expected escaped string %s, got: %s", expected, s)
	}
}

func TestUnquotedString(t *testing.T) {
	s := shellescape.Quote(`no quotes`)
	expected := `'no quotes'`
	if s != expected {
		t.Errorf("Expected escaped string %s, got: %s", expected, s)
	}
}

func TestSingleInvalid(t *testing.T) {
	s := shellescape.Quote(`;`)
	expected := `';'`
	if s != expected {
		t.Errorf("Expected escaped string %s, got: %s", expected, s)
	}
}

func TestAllInvalid(t *testing.T) {
	s := shellescape.Quote(`;${}`)
	expected := `';${}'`
	if s != expected {
		t.Errorf("Expected escaped string %s, got: %s", expected, s)
	}
}

func TestCleanString(t *testing.T) {
	s := shellescape.Quote("foo.example.com")
	expected := `foo.example.com`
	if s != expected {
		t.Errorf("Expected escaped string %s, got: %s", expected, s)
	}
}
