package regexp2

import (
	"testing"
)

func TestRE2CompatCapture(t *testing.T) {
	r := MustCompile(`re(?P<a>2)`, RE2)
	if m, err := r.FindStringMatch("blahre2blah"); m == nil {
		t.Fatal("Expected match")
	} else if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	} else {
		g := m.GroupByName("a")
		if want, got := "2", g.String(); want != got {
			t.Fatalf("Wanted %v got %v", want, got)
		}
	}
}

func TestRE2CompatCapture_Invalid(t *testing.T) {
	bogus := []string{
		`(?P<name>a`,
		`(?P<name>`,
		`(?P<name`,
		`(?P<x y>a)`,
		`(?P<>a)`,
	}
	for _, inp := range bogus {
		t.Run(inp, func(t *testing.T) {
			r, err := Compile(inp, RE2)
			if err == nil {
				t.Fatal("Expected failure to parse")
			}
			if r != nil {
				t.Fatal("expected regexp to be nil")
			}
		})
	}
}

func TestRE2NamedAscii(t *testing.T) {
	table := []struct {
		nm  string
		pos string
		neg string
	}{
		{nm: "alnum", pos: "1", neg: "!"},
		{nm: "alpha", pos: "g", neg: "0"},
		{nm: "blank", pos: " ", neg: "_"},
		{nm: "ascii", pos: "*", neg: "\x8f"},
		{nm: "cntrl", pos: "\t", neg: "A"},
		{nm: "graph", pos: "!", neg: " "},
		{nm: "lower", pos: "a", neg: "A"},
		{nm: "print", pos: " ", neg: "\r"},
		{nm: "punct", pos: "@", neg: "A"},
		{nm: "space", pos: " ", neg: "A"},
		{nm: "digit", pos: "1", neg: "A"},
		{nm: "upper", pos: "A", neg: "a"},
		{nm: "word", pos: "_", neg: "-"},
		{nm: "xdigit", pos: "A", neg: "G"},
	}

	for _, row := range table {
		t.Run(row.nm, func(t *testing.T) {
			r := MustCompile(`[[:`+row.nm+`:]]`, RE2)
			if m, _ := r.MatchString(row.pos); !m {
				t.Fatal("Expected match")
			}
			if m, _ := r.MatchString(row.neg); m {
				t.Fatal("Expected no match")
			}
		})
		t.Run(row.nm+" negate", func(t *testing.T) {
			r := MustCompile(`[[:^`+row.nm+`:]]`, RE2)
			if m, _ := r.MatchString(row.neg); !m {
				t.Fatal("Expected match")
			}
			if m, _ := r.MatchString(row.pos); m {
				t.Fatal("Expected no match")
			}
		})
	}

}
func TestRE2NamedAscii_Concat(t *testing.T) {
	r := MustCompile(`[[:digit:]a]`, RE2)
	if m, _ := r.MatchString("b"); m {
		t.Fatal("Expected no match")
	}
	if m, _ := r.MatchString("a"); !m {
		t.Fatal("Expected match")
	}
	if m, _ := r.MatchString("["); m {
		t.Fatal("Expected no match")
	}
	if m, _ := r.MatchString("5"); !m {
		t.Fatal("Expected match")
	}
}

func TestRE2Dollar_Singleline(t *testing.T) {
	// PCRE allows for \n after the $ and RE2 doesn't
	r := MustCompile(`^ac$\n`, RE2)
	if m, _ := r.MatchString("ac"); m {
		t.Fatal("Expected no match")
	}
	if m, _ := r.MatchString("ac\n"); m {
		t.Fatal("Expected no match")
	}
}

func TestRE2Dollar_Multiline(t *testing.T) {
	r := MustCompile(`^ac$\n`, RE2|Multiline)
	if m, _ := r.MatchString("ac"); m {
		t.Fatal("Expected no match")
	}
	if m, err := r.MatchString("ac\n"); err != nil {
		t.Fatal(err)
	} else if !m {
		t.Fatal("Expected match")
	}
}

func TestRE2ExtendedZero(t *testing.T) {
	notZero := "߀" // \u07c0
	r := MustCompile(`^\d$`, RE2)
	if m, _ := r.MatchString(notZero); m {
		t.Fatal("Expected no match")
	}

	r = MustCompile(`^\D$`, RE2)
	if m, _ := r.MatchString(notZero); !m {
		t.Fatal("Expected match")
	}
}

func TestRegularExtendedZero(t *testing.T) {
	notZero := "߀" // \u07c0

	r := MustCompile(`^\d$`, 0)
	if m, _ := r.MatchString(notZero); !m {
		t.Fatal("Expected match")
	}

	r = MustCompile(`^\D$`, 0)
	if m, _ := r.MatchString(notZero); m {
		t.Fatal("Expected no match")
	}
}

func TestRE2Word(t *testing.T) {
	r := MustCompile(`\w`, RE2)
	if m, _ := r.MatchString("å"); m {
		t.Fatal("Expected no match")
	}

	r = MustCompile(`\W`, RE2)
	if m, _ := r.MatchString("å"); !m {
		t.Fatal("Expected match")
	}

}

func TestRegularWord(t *testing.T) {
	r := MustCompile(`\w`, 0)
	if m, _ := r.MatchString("å"); !m {
		t.Fatal("Expected match")
	}
	r = MustCompile(`\W`, 0)
	if m, _ := r.MatchString("å"); m {
		t.Fatal("Expected no match")
	}
}

func TestRE2Space(t *testing.T) {
	r := MustCompile(`\s`, RE2)
	if m, _ := r.MatchString("\x0b"); m {
		t.Fatal("Expected no match")
	}
	r = MustCompile(`\S`, RE2)
	if m, _ := r.MatchString("\x0b"); !m {
		t.Fatal("Expected match")
	}
}

func TestRegularSpace(t *testing.T) {
	r := MustCompile(`\s`, 0)
	if m, _ := r.MatchString("\x0b"); !m {
		t.Fatal("Expected match")
	}
	r = MustCompile(`\S`, 0)
	if m, _ := r.MatchString("\x0b"); m {
		t.Fatal("Expected no match")
	}
}

func TestEscapeLiteralDefaults(t *testing.T) {
	_, err := Compile(`a\_test`, 0)
	if err == nil {
		t.Fatal("Expected compile fail")
	}

	r := MustCompile(`a\_test`, RE2)
	if m, _ := r.MatchString("a_test"); !m {
		t.Fatal("Expected match")
	}
	if m, _ := r.MatchString("a\\_test"); m {
		t.Fatal("Expected no match")
	}
}

/*
func TestRE2EndZ_Singleline(t *testing.T) {
	// PCRE allows for \n after the $ and RE2 doesn't
	r := MustCompile(`^ac$\Z`, RE2|Debug)
	if m, _ := r.MatchString("ac"); m {
		t.Fatal("Expected no match")
	}
	if m, _ := r.MatchString("ac\n"); m {
		t.Fatal("Expected no match")
	}
}*/
