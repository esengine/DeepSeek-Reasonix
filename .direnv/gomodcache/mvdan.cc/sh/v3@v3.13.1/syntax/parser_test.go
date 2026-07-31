// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-quicktest/qt"
	"github.com/google/go-cmp/cmp"
	"mvdan.cc/sh/v3/internal"
)

func TestParseFiles(t *testing.T) {
	t.Parallel()
	for lang := range langResolvedVariants.bits() {
		t.Run(lang.String(), func(t *testing.T) {
			p := NewParser(Variant(lang))
			for i, c := range append(fileTests, fileTestsNoPrint...) {
				want := c.byLangIndex[lang.index()]
				switch want := want.(type) {
				case nil:
					continue
				case *File:
					for j, in := range c.inputs {
						t.Run(fmt.Sprintf("OK/%03d-%d", i, j), singleParse(p, in, want))
					}
				case string:
					want = strings.Replace(want, "LANG", p.lang.String(), 1)
					for j, in := range c.inputs {
						t.Run(fmt.Sprintf("Err/%03d-%d", i, j), func(t *testing.T) {
							t.Logf("input: %s", in)
							_, err := p.Parse(newStrictReader(in), "")
							if err == nil {
								t.Fatalf("Expected error: %v", want)
							}
							if got := err.Error(); got != want {
								t.Fatalf("Error mismatch\nwant: %s\ngot:  %s",
									want, got)
							}
						})
					}
				}
			}
		})
	}
}

func TestParseErr(t *testing.T) {
	t.Parallel()
	for lang := range langResolvedVariants.bits() {
		t.Run(lang.String(), func(t *testing.T) {
			p := NewParser(Variant(lang), KeepComments(true))
			for _, c := range errorCases {
				want := c.byLangIndex[lang.index()]
				if want == "" {
					continue
				}
				t.Run("", func(t *testing.T) { // number them #001, #002, ...
					want = strings.Replace(want, "LANG", p.lang.String(), 1)
					t.Logf("input: %s", c.in)
					_, err := p.Parse(newStrictReader(c.in), "")
					if err == nil {
						t.Fatalf("Expected error: %v", want)
					}
					if got := err.Error(); got != want {
						t.Fatalf("Error mismatch\nwant: %s\ngot:  %s",
							want, got)
					}
				})
			}
		})
	}
}

func TestParseConfirm(t *testing.T) {
	if testing.Short() {
		t.Skip("calling external shells is slow")
	}
	for lang := range langResolvedVariants.bits() {
		t.Run(lang.String(), func(t *testing.T) {
			external, ok := externalShells[lang]
			if !ok {
				t.Skip("no external shell to check against")
			}
			if external.require != nil {
				external.require(t)
			}
			for i, c := range append(fileTests, fileTestsNoPrint...) {
				want := c.byLangIndex[lang.index()]
				switch want.(type) {
				case nil:
					continue
				case *File:
					for j, in := range c.inputs {
						wantErr := lang.in(c.flipConfirmSet)
						t.Run(fmt.Sprintf("OK/%03d-%d", i, j), confirmParse(in, external.cmd, wantErr))
					}
				case string:
					for j, in := range c.inputs {
						wantErr := !lang.in(c.flipConfirmSet)
						t.Run(fmt.Sprintf("Err/%03d-%d", i, j), confirmParse(in, external.cmd, wantErr))
					}
				}
			}
			if lang == LangZsh {
				return // TODO: we don't confirm errors with zsh yet
			}
			for i, c := range errorCases {
				want := c.byLangIndex[lang.index()]
				if want == "" {
					continue
				}
				wantErr := !lang.in(c.flipConfirmSet)
				t.Run(fmt.Sprintf("ErrOld/%03d", i), confirmParse(c.in, external.cmd, wantErr))
			}
		})
	}
}

func TestParseBashKeepComments(t *testing.T) {
	t.Parallel()
	p := NewParser(KeepComments(true))
	for i, c := range fileTestsKeepComments {
		want, _ := c.byLangIndex[LangBash.index()].(*File)
		if want == nil {
			continue
		}
		for j, in := range c.inputs {
			t.Run(fmt.Sprintf("%03d-%d", i, j), singleParse(p, in, want))
		}
	}
}

func TestParsePosOverflow(t *testing.T) {
	t.Parallel()

	// Consider using a custom reader to save memory.
	tests := []struct {
		name, in, want string
	}{
		{
			"LineOverflowIsValid",
			strings.Repeat("\n", lineMax) + "foo; bar",
			"<nil>",
		},
		{
			"LineOverflowPosString",
			strings.Repeat("\n", lineMax) + ")",
			"?:1: `)` can only be used to close a subshell",
		},
		{
			"LineOverflowExtraPosString",
			strings.Repeat("\n", lineMax+5) + ")",
			"?:1: `)` can only be used to close a subshell",
		},
		{
			"ColOverflowPosString",
			strings.Repeat(" ", colMax) + ")",
			"1:?: `)` can only be used to close a subshell",
		},
		{
			"ColOverflowExtraPosString",
			strings.Repeat(" ", colMax) + ")",
			"1:?: `)` can only be used to close a subshell",
		},
		{
			"ColOverflowSkippedPosString",
			strings.Repeat(" ", colMax+5) + "\n)",
			"2:1: `)` can only be used to close a subshell",
		},
		{
			"LargestLineNumber",
			strings.Repeat("\n", lineMax-1) + ")",
			"262143:1: `)` can only be used to close a subshell",
		},
		{
			"LargestColNumber",
			strings.Repeat(" ", colMax-1) + ")",
			"1:16383: `)` can only be used to close a subshell",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			p := NewParser()
			_, err := p.Parse(strings.NewReader(test.in), "")
			got := fmt.Sprint(err)
			if got != test.want {
				t.Fatalf("want error %q, got %q", test.want, got)
			}
		})
	}
}

func TestMain(m *testing.M) {
	internal.TestMainSetup()
	m.Run()
}

var (
	onceHasBash53 = sync.OnceValue(func() bool {
		return cmdContains("version 5.3", "bash", "--version")
	})

	onceHasDash059 = sync.OnceValue(func() bool {
		// dash provides no way to check its version, so we have to
		// check if it's new enough as to not have the bug that breaks
		// our integration tests.
		// This also means our check does not require a specific version.
		//
		// We get odd failures on Windows on CI, and it's hard to debug
		// or even understand what version of dash it's using; skip on those.
		return cmdContains("Bad subst", "dash", "-c", "echo ${#<}") &&
			runtime.GOOS != "windows"
	})

	onceHasMksh59 = sync.OnceValue(func() bool {
		return cmdContains(" R59 ", "mksh", "-c", "echo $KSH_VERSION")
	})

	onceHasZsh59 = sync.OnceValue(func() bool {
		return cmdContains("zsh 5.9", "zsh", "--version")
	})
)

type externalShell struct {
	cmd     string
	require func(testing.TB)
}

// requireShells can be set to make sure that no external shell tests
// are being skipped due to a misalignment in installed versions.
var requireShells = os.Getenv("REQUIRE_SHELLS") == "1"

func skipExternal(tb testing.TB, message string) {
	if requireShells {
		tb.Fatal(message)
	} else {
		tb.Skip(message)
	}
}

// Note that externalShells is a map, and not an array,
// because [LangVariant.index] is not a constant expression.
// This seems fine; this table is only for the sake of testing.
var externalShells = map[LangVariant]externalShell{
	LangBash: {"bash", func(tb testing.TB) {
		if !onceHasBash53() {
			skipExternal(tb, "bash 5.2 required to run")
		}
	}},
	LangPOSIX: {"dash", func(tb testing.TB) {
		if !onceHasDash059() {
			skipExternal(tb, "dash 0.5.9+ required to run")
		}
	}},
	LangMirBSDKorn: {"mksh", func(tb testing.TB) {
		if !onceHasMksh59() {
			skipExternal(tb, "mksh 59 required to run")
		}
	}},
	LangZsh: {"zsh", func(tb testing.TB) {
		if !onceHasZsh59() {
			skipExternal(tb, "zsh 5.9 required to run")
		}
	}},
}

func cmdContains(substr, cmd string, args ...string) bool {
	out, err := exec.Command(cmd, args...).CombinedOutput()
	got := string(out)
	if err != nil {
		got += "\n" + err.Error()
	}
	return strings.Contains(got, substr)
}

var extGlobRe = regexp.MustCompile(`[@?*+!]\(`)

func confirmParse(in, cmd string, wantErr bool) func(*testing.T) {
	return func(t *testing.T) {
		t.Helper()
		t.Parallel()
		t.Logf("input: %s", in)
		var opts []string
		if strings.Contains(in, "\\\r\n") {
			t.Skip("shells do not generally support CRLF line endings")
		}
		if cmd == "bash" && extGlobRe.MatchString(in) {
			// otherwise bash refuses to parse these
			// properly. Also avoid -n since that too makes
			// bash bail.
			in = "shopt -s extglob\n" + in
		} else if !wantErr {
			// -n makes bash accept invalid inputs like
			// "let" or "`{`", so only use it in
			// non-erroring tests. Should be safe to not use
			// -n anyway since these are supposed to just fail.
			// also, -n will break if we are using extglob
			// as extglob is not actually applied.
			opts = append(opts, "-n")
		}

		// All the bits of shell we test should either finish or fail very quickly,
		// given that they are very small. If we make a mistake with an endless loop,
		// or we somehow trigger a bug that makes a shell hang, kill it.
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, cmd, opts...)
		killCommandOnTestExit(cmd)
		cmd.Dir = t.TempDir() // to be safe
		cmd.Stdin = strings.NewReader(in)
		var stderrBuf strings.Builder
		cmd.Stderr = &stderrBuf
		err := cmd.Run()

		if cmd.ProcessState.ExitCode() == -1 {
			t.Fatalf("shell terminated by signal: %v", err)
		}

		// bash sometimes likes to error on an input via stderr
		// while forgetting to set the exit code to non-zero. Fun.
		// Note that we do not treat warnings as errors.
		stderrLines := strings.Split(stderrBuf.String(), "\n")
		for i, line := range stderrLines {
			stderrLines[i] = strings.TrimSpace(line)
		}
		stderrLines = slices.DeleteFunc(stderrLines, func(line string) bool {
			return line == "" || strings.Contains(line, "warning:")
		})
		if stderr := strings.Join(stderrLines, "\n"); stderr != "" {
			if err == nil {
				err = fmt.Errorf("non-fatal error: %s", stderr)
			} else {
				err = fmt.Errorf("%v: %s", err, stderr)
			}
		}

		if wantErr && err == nil {
			t.Fatalf("Expected error in %q", strings.Join(cmd.Args, " "))
		} else if !wantErr && err != nil {
			t.Fatalf("Unexpected error in %q: %v", strings.Join(cmd.Args, " "), err)
		}
	}
}

var cmpOpt = cmp.FilterValues(func(p1, p2 Pos) bool { return true }, cmp.Ignore())

func singleParse(p *Parser, in string, want *File) func(t *testing.T) {
	return func(t *testing.T) {
		t.Helper()
		t.Logf("input: %s", in)
		got, err := p.Parse(newStrictReader(in), "")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		Walk(got, sanityChecker{tb: t, src: in}.visit)
		qt.Assert(t, qt.CmpEquals(got, want, cmpOpt))
	}
}

type errorCase struct {
	in string

	byLangIndex [langResolvedVariantsCount]string

	// The real shells where testing the input succeeds rather than failing as expected.
	flipConfirmSet LangVariant
}

func errCase(in string, opts ...func(*errorCase)) errorCase {
	c := errorCase{in: in}
	for _, o := range opts {
		o(&c)
	}
	return c
}

func langErr(want string, langSets ...LangVariant) func(*errorCase) {
	return func(c *errorCase) {
		// The parameter is a slice to allow omitting the argument.
		switch len(langSets) {
		case 0:
			for i := range c.byLangIndex {
				c.byLangIndex[i] = want
			}
			return
		case 1:
			// continue below
		default:
			panic("use a LangVariant bitset")
		}
		for lang := range langSets[0].bits() {
			c.byLangIndex[lang.index()] = want
		}
	}
}

func flipConfirm(langSet LangVariant) func(*errorCase) {
	return func(c *errorCase) { c.flipConfirmSet = langSet }
}

var flipConfirmAll = flipConfirm(langResolvedVariants)

// The real shells which allow unclosed heredocs.
// TODO: allow ending a heredoc at EOF in these language variant modes.
var flipConfirmUnclosedHeredoc = flipConfirm(LangBash | LangPOSIX | LangBats | LangZsh)

func init() {
	seenInputs := make(map[string]bool)
	for i, c := range errorCases {
		if seenInputs[c.in] {
			panic(fmt.Sprintf("duplicate at %d: %q", i, c.in))
		}
		seenInputs[c.in] = true
	}
}

var errorCases = []errorCase{
	errCase(
		"echo \x80",
		langErr("1:6: invalid UTF-8 encoding"),
		flipConfirmAll, // common shells use bytes
	),
	errCase(
		"\necho \x80",
		langErr("2:6: invalid UTF-8 encoding"),
		flipConfirmAll, // common shells use bytes
	),
	errCase(
		"echo foo\x80bar",
		langErr("1:9: invalid UTF-8 encoding"),
		flipConfirmAll, // common shells use bytes
	),
	errCase(
		"echo foo\xc3",
		langErr("1:9: invalid UTF-8 encoding"),
		flipConfirmAll, // common shells use bytes
	),
	errCase(
		"#foo\xc3",
		langErr("1:5: invalid UTF-8 encoding"),
		flipConfirmAll, // common shells use bytes
	),
	errCase(
		"echo a\x80",
		langErr("1:7: invalid UTF-8 encoding"),
		flipConfirmAll, // common shells use bytes
	),
	errCase(
		"<<$\xc8\n$\xc8",
		langErr("1:4: invalid UTF-8 encoding"),
		flipConfirmAll, // common shells use bytes
	),
	errCase(
		"echo $((foo\x80bar",
		langErr("1:12: invalid UTF-8 encoding"),
	),
	errCase(
		"z=($\\\n#\\\n\\\n$#\x91\\\n",
		langErr("4:3: invalid UTF-8 encoding", LangBash),
	),
	errCase(
		`${ `,
		langErr("1:1: reached EOF without matching `${` with `}`", LangMirBSDKorn),
	),
	errCase(
		`${ foo;`,
		langErr("1:1: reached EOF without matching `${` with `}`", LangMirBSDKorn),
	),
	errCase(
		`${ foo }`,
		langErr("1:1: reached EOF without matching `${` with `}`", LangMirBSDKorn),
	),
	errCase(
		`${|`,
		langErr("1:1: reached EOF without matching `${` with `}`", LangMirBSDKorn),
	),
	errCase(
		`${|foo;`,
		langErr("1:1: reached EOF without matching `${` with `}`", LangMirBSDKorn),
	),
	errCase(
		`${|foo }`,
		langErr("1:1: reached EOF without matching `${` with `}`", LangMirBSDKorn),
	),
	errCase(
		"((foo\x80bar",
		langErr("1:6: invalid UTF-8 encoding"),
	),
	errCase(
		";\x80",
		langErr("1:2: invalid UTF-8 encoding"),
	),
	errCase(
		"${a\x80",
		langErr("1:4: invalid UTF-8 encoding"),
	),
	errCase(
		"${a#\x80",
		langErr("1:5: invalid UTF-8 encoding"),
	),
	errCase(
		"${a-'\x80",
		langErr("1:6: invalid UTF-8 encoding"),
	),
	errCase(
		"echo $((a |\x80",
		langErr("1:12: invalid UTF-8 encoding"),
	),
	errCase(
		"!",
		langErr("1:1: `!` cannot form a statement alone"),
	),
	errCase(
		"! !",
		langErr("1:1: cannot negate a command multiple times"),
		flipConfirm(LangBash), // bash allows lone `!`, unlike dash, mksh, and us.
	),
	errCase(
		"! ! foo",
		langErr("1:1: cannot negate a command multiple times"),
		flipConfirm(LangBash|LangMirBSDKorn), // bash allows lone `!`, unlike dash, mksh, and us.
	),
	errCase(
		"}",
		langErr("1:1: `}` can only be used to close a block"),
	),
	errCase(
		"foo | }",
		langErr("1:7: `}` can only be used to close a block"),
	),
	errCase(
		"foo }",
		langErr("1:5: `}` can only be used to close a block", LangZsh),
	),
	errCase(
		"then",
		langErr("1:1: `then` can only be used in an `if`"),
	),
	errCase(
		"elif",
		langErr("1:1: `elif` can only be used in an `if`"),
	),
	errCase(
		"fi",
		langErr("1:1: `fi` can only be used to end an `if`"),
	),
	errCase(
		"do",
		langErr("1:1: `do` can only be used in a loop"),
	),
	errCase(
		"done",
		langErr("1:1: `done` can only be used to end a loop"),
	),
	errCase(
		"esac",
		langErr("1:1: `esac` can only be used to end a `case`"),
	),
	errCase(
		"a=b { foo; }",
		langErr("1:12: `}` can only be used to close a block"),
	),
	errCase(
		"a=b foo() { bar; }",
		langErr("1:8: a command can only contain words and redirects; encountered `(`"),
	),
	errCase(
		"a=b if foo; then bar; fi",
		langErr("1:13: `then` can only be used in an `if`"),
	),
	errCase(
		">f { foo; }",
		langErr("1:1: redirects before compound commands are a zsh feature; tried parsing as LANG"),
		langErr("", LangZsh),
	),
	errCase(
		">f foo() { bar; }",
		langErr("1:1: redirects before compound commands are a zsh feature; tried parsing as LANG"),
		langErr("", LangZsh),
	),
	errCase(
		">f if foo; then bar; fi",
		langErr("1:1: redirects before compound commands are a zsh feature; tried parsing as LANG"),
		langErr("", LangZsh),
	),
	errCase(
		"if done; then b; fi",
		langErr("1:4: `done` can only be used to end a loop"),
	),
	errCase(
		"'",
		langErr("1:1: reached EOF without closing quote `'`"),
	),
	errCase(
		`"`,
		langErr("1:1: reached EOF without closing quote `\"`"),
	),
	errCase(
		`'\''`,
		langErr("1:4: reached EOF without closing quote `'`"),
	),
	errCase(
		";",
		langErr("1:1: `;` can only immediately follow a statement"),
	),
	errCase(
		"{ ; }",
		langErr("1:1: `{` must be followed by a statement list"),
		langErr("", LangZsh|LangMirBSDKorn),
	),
	errCase(
		`"foo"(){ :; }`,
		langErr("1:1: invalid func name"),
		flipConfirm(LangMirBSDKorn), // TODO: support non-literal func names
	),
	errCase(
		`foo$bar(){ :; }`,
		langErr("1:1: invalid func name"),
	),
	errCase(
		"{",
		langErr("1:1: `{` must be followed by a statement list"),
		langErr("1:1: reached EOF without matching `{` with `}`", LangZsh|LangMirBSDKorn),
	),
	errCase(
		"{ foo;",
		langErr("1:1: reached EOF without matching `{` with `}`"),
	),
	errCase(
		"{ foo; #}",
		langErr("1:1: reached EOF without matching `{` with `}`"),
	),
	errCase(
		"(x",
		langErr("1:1: reached EOF without matching `(` with `)`"),
	),
	errCase(
		")",
		langErr("1:1: `)` can only be used to close a subshell"),
	),
	errCase(
		"`",
		langErr("1:1: reached EOF without closing quote \"`\""),
	),
	errCase(
		";;",
		langErr("1:1: `;;` can only be used in a case clause"),
	),
	errCase(
		"( foo;",
		langErr("1:1: reached EOF without matching `(` with `)`"),
	),
	errCase(
		"&",
		langErr("1:1: `&` can only immediately follow a statement"),
	),
	errCase(
		"|",
		langErr("1:1: `|` can only immediately follow a statement"),
	),
	errCase(
		"&&",
		langErr("1:1: `&&` can only immediately follow a statement"),
	),
	errCase(
		"||",
		langErr("1:1: `||` can only immediately follow a statement"),
	),
	errCase(
		"foo; || bar",
		langErr("1:6: `||` can only immediately follow a statement"),
	),
	errCase(
		"echo & || bar",
		langErr("1:8: `||` can only immediately follow a statement"),
	),
	errCase(
		"echo & ; bar",
		langErr("1:8: `;` can only immediately follow a statement"),
	),
	errCase(
		"foo;;",
		langErr("1:4: `;;` can only be used in a case clause"),
	),
	errCase(
		"foo(",
		langErr("1:1: `foo(` must be followed by `)`", LangPOSIX|LangBash|LangMirBSDKorn|LangBats),
		langErr("1:4: reached EOF without matching `(` with `)`", LangZsh),
	),
	errCase(
		"foo(bar",
		langErr("1:1: `foo(` must be followed by `)`", LangPOSIX|LangBash|LangMirBSDKorn|LangBats),
		langErr("1:4: reached EOF without matching `(` with `)`", LangZsh),
	),
	errCase(
		"à(",
		langErr("1:1: `foo(` must be followed by `)`", LangPOSIX|LangBash|LangMirBSDKorn|LangBats),
		langErr("1:3: reached EOF without matching `(` with `)`", LangZsh),
	),
	errCase(
		"foo'",
		langErr("1:4: reached EOF without closing quote `'`"),
	),
	errCase(
		`foo"`,
		langErr("1:4: reached EOF without closing quote `\"`"),
	),
	errCase(
		`"foo`,
		langErr("1:1: reached EOF without closing quote `\"`"),
	),
	errCase(
		`"foobar\`,
		langErr("1:1: reached EOF without closing quote `\"`"),
	),
	errCase(
		`"foo\a`,
		langErr("1:1: reached EOF without closing quote `\"`"),
	),
	errCase(
		"foo()",
		langErr("1:1: `foo()` must be followed by a statement"),
		flipConfirm(LangMirBSDKorn), // TODO: some variants allow a missing body
	),
	errCase(
		"foo() {",
		langErr("1:7: `{` must be followed by a statement list"),
		langErr("1:7: reached EOF without matching `{` with `}`", LangZsh|LangMirBSDKorn),
	),
	errCase(
		"foo() { bar;",
		langErr("1:7: reached EOF without matching `{` with `}`"),
	),
	errCase(
		"foo-bar() { x; }",
		langErr("1:1: invalid func name", LangPOSIX),
	),
	errCase(
		"foò() { x; }",
		langErr("1:1: invalid func name", LangPOSIX),
	),
	errCase(
		"echo foo(",
		langErr("1:9: a command can only contain words and redirects; encountered `(`", LangPOSIX|LangBash|LangMirBSDKorn|LangBats),
		langErr("1:9: reached EOF without matching `(` with `)`", LangZsh),
	),
	errCase(
		"echo &&",
		langErr("1:6: `&&` must be followed by a statement"),
	),
	errCase(
		"echo |",
		langErr("1:6: `|` must be followed by a statement"),
	),
	errCase(
		"echo ||",
		langErr("1:6: `||` must be followed by a statement"),
	),
	errCase(
		"echo | #bar",
		langErr("1:6: `|` must be followed by a statement"),
	),
	errCase(
		"echo && #bar",
		langErr("1:6: `&&` must be followed by a statement"),
	),
	errCase(
		"`echo &&`",
		langErr("1:7: `&&` must be followed by a statement"),
	),
	errCase(
		"`echo |`",
		langErr("1:7: `|` must be followed by a statement"),
	),
	errCase(
		"echo | ! true",
		langErr("1:8: `!` can only be used in full statements"),
	),
	errCase(
		"echo >",
		langErr("1:6: `>` must be followed by a word"),
	),
	errCase(
		"echo >>",
		langErr("1:6: `>>` must be followed by a word"),
	),
	errCase(
		"echo <",
		langErr("1:6: `<` must be followed by a word"),
	),
	errCase(
		"echo 2>",
		langErr("1:7: `>` must be followed by a word"),
	),
	errCase(
		"echo <\nbar",
		langErr("1:6: `<` must be followed by a word"),
	),
	errCase(
		"echo | < #bar",
		langErr("1:8: `<` must be followed by a word"),
	),
	errCase(
		"echo && > #",
		langErr("1:9: `>` must be followed by a word"),
	),
	errCase(
		"<<",
		langErr("1:1: `<<` must be followed by a word"),
	),
	errCase(
		"<<EOF",
		langErr("1:1: unclosed here-document `EOF`"),
		flipConfirmUnclosedHeredoc,
	),
	errCase(
		"<<EOF\n\\",
		langErr("1:1: unclosed here-document `EOF`"),
		flipConfirmUnclosedHeredoc,
	),
	errCase(
		"<<EOF\n\\\n",
		langErr("1:1: unclosed here-document `EOF`"),
		flipConfirmUnclosedHeredoc,
	),
	errCase(
		"<<EOF\n\\\nEOF",
		langErr("1:1: unclosed here-document `EOF`"),
		flipConfirmAll, // why does mksh allow this?
	),
	errCase(
		"<<EOF\nfoo\\\nEOF",
		langErr("1:1: unclosed here-document `EOF`"),
		flipConfirmUnclosedHeredoc,
	),
	errCase(
		"<<'EOF'\n\\\n",
		langErr("1:1: unclosed here-document `EOF`"),
		flipConfirmUnclosedHeredoc,
	),
	errCase(
		"<<EOF <`\n#\n`\n``",
		langErr("1:1: unclosed here-document `EOF`"),
	),
	errCase(
		"<<'EOF'",
		langErr("1:1: unclosed here-document `EOF`"),
		flipConfirmUnclosedHeredoc,
	),
	errCase(
		"<<\\EOF",
		langErr("1:1: unclosed here-document `EOF`"),
		flipConfirmUnclosedHeredoc,
	),
	errCase(
		"<<\\\\EOF",
		langErr("1:1: unclosed here-document `\\EOF`"),
		flipConfirmUnclosedHeredoc,
	),
	errCase(
		"<<-EOF",
		langErr("1:1: unclosed here-document `EOF`"),
		flipConfirmUnclosedHeredoc,
	),
	errCase(
		"<<-EOF\n\t",
		langErr("1:1: unclosed here-document `EOF`"),
		flipConfirmUnclosedHeredoc,
	),
	errCase(
		"<<-'EOF'\n\t",
		langErr("1:1: unclosed here-document `EOF`"),
		flipConfirmUnclosedHeredoc,
	),
	errCase(
		"<<\nEOF\nbar\nEOF",
		langErr("1:1: `<<` must be followed by a word"),
	),
	errCase(
		"$(<<EOF\nNOTEOF)",
		langErr("1:3: unclosed here-document `EOF`", LangBash|LangMirBSDKorn),
		// Note that this fails on external shells as they treat ")" as part of the heredoc.
	),
	errCase(
		"`<<EOF\nNOTEOF`",
		langErr("1:2: unclosed here-document `EOF`", LangBash|LangMirBSDKorn),
		flipConfirmAll,
		// Note that this works on external shells as they treat "`" as outside the heredoc.
	),
	errCase(
		"if",
		langErr("1:1: `if` must be followed by a statement list"),
		langErr("1:1: `if <cond>` must be followed by `then`", LangZsh|LangMirBSDKorn),
	),
	errCase(
		"if true;",
		langErr("1:1: `if <cond>` must be followed by `then`"),
	),
	errCase(
		"if true then",
		langErr("1:1: `if <cond>` must be followed by `then`"),
	),
	errCase(
		"if true; then bar;",
		langErr("1:1: `if` statement must end with `fi`"),
	),
	errCase(
		"if true; then bar; fi#etc",
		langErr("1:1: `if` statement must end with `fi`"),
	),
	errCase(
		"if a; then b; elif c;",
		langErr("1:15: `elif <cond>` must be followed by `then`"),
	),
	errCase(
		"'foo' '",
		langErr("1:7: reached EOF without closing quote `'`"),
	),
	errCase(
		"'foo\n' '",
		langErr("2:3: reached EOF without closing quote `'`"),
	),
	errCase(
		"while",
		langErr("1:1: `while` must be followed by a statement list"),
		langErr("1:1: `while <cond>` must be followed by `do`", LangZsh|LangMirBSDKorn),
	),
	errCase(
		"while true;",
		langErr("1:1: `while <cond>` must be followed by `do`"),
	),
	errCase(
		"while true; do bar",
		langErr("1:1: `while` statement must end with `done`"),
	),
	errCase(
		"while true; do bar;",
		langErr("1:1: `while` statement must end with `done`"),
	),
	errCase(
		"until",
		langErr("1:1: `until` must be followed by a statement list"),
		langErr("1:1: `until <cond>` must be followed by `do`", LangZsh|LangMirBSDKorn),
	),
	errCase(
		"until true;",
		langErr("1:1: `until <cond>` must be followed by `do`"),
	),
	errCase(
		"until true; do bar",
		langErr("1:1: `until` statement must end with `done`"),
	),
	errCase(
		"until true; do bar;",
		langErr("1:1: `until` statement must end with `done`"),
	),
	errCase(
		"for",
		langErr("1:1: `for` must be followed by a literal"),
	),
	errCase(
		"for i",
		langErr("1:1: `for foo` must be followed by `in`, `do`, `;`, or a newline"),
	),
	errCase(
		"for i in;",
		langErr("1:1: `for foo [in words]` must be followed by `do`"),
	),
	errCase(
		"for i in 1 2 3;",
		langErr("1:1: `for foo [in words]` must be followed by `do`"),
	),
	errCase(
		"for i in 1 2 &",
		langErr("1:1: `for foo [in words]` must be followed by `do`"),
	),
	errCase(
		"for i in 1 2 (",
		langErr("1:14: word list can only contain words"),
		langErr("1:14: reached EOF without matching `(` with `)`", LangZsh),
	),
	errCase(
		"for i in 1 2 3; do echo $i;",
		langErr("1:1: `for` statement must end with `done`"),
	),
	errCase(
		"for i in 1 2 3; echo $i;",
		langErr("1:1: `for foo [in words]` must be followed by `do`"),
	),
	errCase(
		"for 'i' in 1 2 3; do echo $i; done",
		langErr("1:1: `for` must be followed by a literal"),
	),
	errCase(
		"for in 1 2 3; do echo $i; done",
		langErr("1:1: `for foo` must be followed by `in`, `do`, `;`, or a newline"),
	),
	errCase(
		"select",
		langErr("1:1: `select` must be followed by a literal", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"select i",
		langErr("1:1: `select foo` must be followed by `in`, `do`, `;`, or a newline", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"select i in;",
		langErr("1:1: `select foo [in words]` must be followed by `do`", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"select i in 1 2 3;",
		langErr("1:1: `select foo [in words]` must be followed by `do`", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"select i in 1 2 3; do echo $i;",
		langErr("1:1: `select` statement must end with `done`", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"select i in 1 2 3; echo $i;",
		langErr("1:1: `select foo [in words]` must be followed by `do`", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"select 'i' in 1 2 3; do echo $i; done",
		langErr("1:1: `select` must be followed by a literal", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"select in 1 2 3; do echo $i; done",
		langErr("1:1: `select foo` must be followed by `in`, `do`, `;`, or a newline", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"echo foo &\n;",
		langErr("2:1: `;` can only immediately follow a statement"),
	),
	errCase(
		"echo $(foo",
		langErr("1:6: reached EOF without matching `$(` with `)`"),
	),
	errCase(
		"echo $((foo",
		langErr("1:6: reached EOF without matching `$((` with `))`"),
	),
	errCase(
		`echo $((\`,
		langErr("1:6: reached EOF without matching `$((` with `))`"),
	),
	errCase(
		`echo $((o\`,
		langErr("1:6: reached EOF without matching `$((` with `))`"),
	),
	errCase(
		`echo $((foo\a`,
		langErr("1:6: reached EOF without matching `$((` with `))`"),
	),
	errCase(
		`echo $(($(a"`,
		langErr("1:12: reached EOF without closing quote `\"`"),
	),
	errCase(
		"echo $((`echo 0`",
		langErr("1:6: reached EOF without matching `$((` with `))`"),
	),
	errCase(
		`echo $((& $(`,
		langErr("1:9: `&` must follow an expression"),
	),
	errCase(
		`echo $((a'`,
		langErr("1:10: reached EOF without closing quote `'`"),
	),
	errCase(
		`echo $((a b"`,
		langErr("1:11: not a valid arithmetic operator: `b`"),
	),
	errCase(
		"echo $(())",
		langErr("1:6: `$((` must be followed by an expression"),
		flipConfirmAll, // TODO: empty arithmetic expressions seem to be OK?
	),
	errCase(
		"echo $((()))",
		langErr("1:9: `(` must be followed by an expression"),
	),
	errCase(
		"echo $(((3))",
		langErr("1:6: reached `)` without matching `$((` with `))`"),
	),
	errCase(
		"echo $((+))",
		langErr("1:9: `+` must be followed by an expression"),
	),
	errCase(
		"echo $((a b c))",
		langErr("1:11: not a valid arithmetic operator: `b`"),
	),
	errCase(
		"echo $((a ; c))",
		langErr("1:11: not a valid arithmetic operator: `;`"),
	),
	errCase(
		"echo $((foo) )",
		langErr("1:6: reached `)` without matching `$((` with `))`", LangBash|LangMirBSDKorn|LangZsh),
		flipConfirmAll, // note that we don't backtrack
	),
	errCase(
		"echo $((a *))",
		langErr("1:11: `*` must be followed by an expression"),
	),
	errCase(
		"echo $((++))",
		langErr("1:9: `++` must be followed by a literal"),
	),
	errCase(
		"echo $((a ? b))",
		langErr("1:11: ternary operator missing `:` after `?`"),
	),
	errCase(
		"echo $((a : b))",
		langErr("1:11: ternary operator missing `?` before `:`"),
	),
	errCase(
		"echo $((/",
		langErr("1:9: `/` must follow an expression"),
	),
	errCase(
		"echo $((:",
		langErr("1:9: ternary operator missing `?` before `:`"),
	),
	errCase(
		"echo $(((a)+=b))",
		langErr("1:12: `+=` must follow a name"),
		flipConfirm(LangMirBSDKorn),
	),
	errCase(
		"echo $((1=2))",
		langErr("1:10: `=` must follow a name"),
	),
	errCase(
		"echo $(($0=2))",
		langErr("1:11: `=` must follow a name"),
		flipConfirmAll,
	),
	errCase(
		"echo $(($(a)=2))",
		langErr("1:13: `=` must follow a name"),
		flipConfirmAll,
	),
	// errCase(
	// 	"echo $((1'2`))",
	// 	// TODO: Take a look at this again, since this no longer fails
	// 	// after fixing https://github.com/mvdan/sh/issues/587.
	// 	// Note that Bash seems to treat code inside $(()) as if it were
	// 	// within double quotes, yet still requires single quotes to be
	// 	// matched.
	// 	//  `1:10: not a valid arithmetic operator: ``,
	// ),
	errCase(
		"<<EOF\n$(()a",
		langErr("2:1: `$((` must be followed by an expression"),
	),
	errCase(
		"<<EOF\n`))",
		langErr("2:2: `)` can only be used to close a subshell"),
	),
	errCase(
		"echo ${foo",
		langErr("1:6: reached EOF without matching `${` with `}`"),
	),
	errCase(
		"echo ${à}",
		langErr("1:8: invalid parameter name"),
		langErr("1:8: not a valid parameter expansion operator: `à`", LangZsh), // not great, but seems like an edge case
	),
	errCase(
		"echo ${1a}",
		langErr("1:8: invalid parameter name"),
	),
	errCase(
		"echo ${foo-bar",
		langErr("1:6: reached EOF without matching `${` with `}`"),
	),
	errCase(
		"#foo\n{ bar;",
		langErr("2:1: reached EOF without matching `{` with `}`"),
	),
	errCase(
		`echo "foo${bar"`,
		langErr("1:15: not a valid parameter expansion operator: `\"`"),
	),
	errCase(
		"echo ${%",
		langErr("1:6: `${%foo}` is a mksh feature; tried parsing as LANG"),
		langErr("1:9: invalid parameter name", LangMirBSDKorn),
	),
	errCase(
		"echo ${+",
		langErr("1:6: `${+foo}` is a zsh feature; tried parsing as LANG"),
		langErr("1:6: reached EOF without matching `${` with `}`", LangZsh),
	),
	errCase(
		"echo ${#${",
		langErr("1:9: nested parameter expansions are a zsh feature; tried parsing as LANG"),
		langErr("1:9: reached EOF without matching `${` with `}`", LangZsh),
	),
	errCase(
		"echo ${#$(",
		langErr("1:9: nested parameter expansions are a zsh feature; tried parsing as LANG"),
		langErr("1:9: reached EOF without matching `$(` with `)`", LangZsh),
	),
	errCase(
		"echo ${(",
		langErr("1:6: parameter expansion flags are a zsh feature; tried parsing as LANG"),
		langErr("1:8: reached EOF without matching `(` with `)`", LangZsh),
	),
	errCase(
		"echo $$(foo)",
		langErr("1:8: a command can only contain words and redirects; encountered `(`", LangPOSIX|LangBash|LangMirBSDKorn|LangBats),
	),
	errCase(
		"echo ${##",
		langErr("1:6: reached EOF without matching `${` with `}`"),
	),
	errCase(
		"echo ${#<}",
		langErr("1:9: not a valid parameter expansion operator: `<`"),
	),
	errCase(
		"echo ${%<}",
		langErr("1:8: invalid parameter name", LangMirBSDKorn),
	),
	errCase(
		"echo ${!<}",
		langErr("1:9: not a valid parameter expansion operator: `<`", LangBash|LangMirBSDKorn),
	),
	errCase(
		"echo ${@foo}",
		langErr("1:9: `@` cannot be followed by a word"),
	),
	errCase(
		"echo ${$needbraces}",
		langErr("1:9: `$` cannot be followed by a word"),
	),
	errCase(
		"echo ${?foo}",
		langErr("1:9: `?` cannot be followed by a word"),
	),
	errCase(
		"echo ${-foo}",
		langErr("1:9: `-` cannot be followed by a word"),
	),
	errCase(
		`echo ${"bad"}`,
		langErr("1:6: invalid nested parameter expansion", LangZsh),
	),
	errCase(
		`echo ${"$needbraces"}`,
		langErr("1:10: `$` cannot be followed by a word", LangZsh),
	),
	errCase(
		`echo ${"${foo}}`,
		langErr("1:8: reached `}` without closing quote `\"`", LangZsh),
	),
	errCase(
		`echo ${"${foo}bad"}`,
		langErr("1:6: invalid nested parameter expansion", LangZsh),
	),
	errCase(
		"echo ${${nested}foo}",
		langErr("1:17: nested parameter expansion cannot be followed by a word", LangZsh),
	),
	errCase(
		"echo ${@[@]} ${@[*]}",
		langErr("1:9: cannot index a special parameter name", LangBash|LangMirBSDKorn),
	),
	errCase(
		"echo ${${nested}[@]",
		langErr("1:6: reached EOF without matching `${` with `}`", LangZsh),
	),
	errCase(
		"echo ${*[@]} ${*[*]}",
		langErr("1:9: cannot index a special parameter name", LangBash|LangMirBSDKorn),
	),
	errCase(
		"echo ${#[x]}",
		langErr("1:9: cannot index a special parameter name", LangBash|LangMirBSDKorn),
	),
	errCase(
		"echo ${$[0]}",
		langErr("1:9: cannot index a special parameter name", LangBash|LangMirBSDKorn),
	),
	errCase(
		"echo ${?[@]}",
		langErr("1:9: cannot index a special parameter name", LangBash|LangMirBSDKorn),
	),
	errCase(
		"echo ${2[@]}",
		langErr("1:9: cannot index a special parameter name", LangBash|LangMirBSDKorn),
	),
	errCase(
		"echo ${foo*}",
		langErr("1:11: not a valid parameter expansion operator: `*`", LangBash|LangMirBSDKorn),
	),
	errCase(
		"echo ${foo;}",
		langErr("1:11: not a valid parameter expansion operator: `;`", LangBash|LangMirBSDKorn),
	),
	errCase(
		"echo ${foo!}",
		langErr("1:11: not a valid parameter expansion operator: `!`", LangBash|LangMirBSDKorn),
	),
	errCase(
		"echo ${#foo:-bar}",
		langErr("1:12: cannot combine multiple parameter expansion operators", LangBash|LangMirBSDKorn),
	),
	errCase(
		"echo ${%foo:1:3}",
		langErr("1:12: cannot combine multiple parameter expansion operators", LangMirBSDKorn),
	),
	errCase(
		"echo ${#foo%x}",
		langErr("1:12: cannot combine multiple parameter expansion operators", LangMirBSDKorn),
	),
	errCase(
		"echo foo\n;",
		langErr("2:1: `;` can only immediately follow a statement"),
	),
	errCase(
		"<<$ <<0\n$(<<$<<",
		langErr("2:6: `<<` must be followed by a word", LangBash|LangMirBSDKorn),
	),
	errCase(
		"(foo) bar",
		langErr("1:7: statements must be separated by &, ; or a newline"),
	),
	errCase(
		"{ foo; } bar",
		langErr("1:10: statements must be separated by &, ; or a newline"),
	),
	errCase(
		"if foo; then bar; fi bar",
		langErr("1:22: statements must be separated by &, ; or a newline"),
	),
	errCase(
		"case",
		langErr("1:1: `case` must be followed by a word"),
	),
	errCase(
		"case i",
		langErr("1:1: `case x` must be followed by `in`"),
	),
	errCase(
		"case i in 3) foo;",
		langErr("1:1: `case` statement must end with `esac`"),
	),
	errCase(
		"case i in 3) foo; 4) bar; esac",
		langErr("1:20: a command can only contain words and redirects; encountered `)`"),
	),
	errCase(
		"case i in 3&) foo;",
		langErr("1:12: case patterns must be separated with `|`"),
	),
	errCase(
		"case $i in &) foo;",
		langErr("1:12: case patterns must consist of words"),
	),
	errCase(
		"case i {",
		langErr("1:1: `case i {` is a mksh feature; tried parsing as LANG"),
		langErr("1:1: `case` statement must end with `}`", LangMirBSDKorn),
	),
	errCase(
		"case i { x) y ;;",
		langErr("1:1: `case` statement must end with `}`", LangMirBSDKorn),
	),
	errCase(
		"\"`\"",
		langErr("1:3: reached EOF without closing quote `\"`"),
	),
	errCase(
		"`\"`",
		langErr("1:2: reached \"`\" without closing quote `\"`"),
	),
	errCase(
		"`\\```",
		langErr("1:3: reached EOF without closing quote \"`\""),
	),
	errCase(
		"`{\nfoo`",
		langErr("1:2: reached \"`\" without matching `{` with `}`"),
	),
	errCase(
		"echo \"`)`\"",
		langErr("1:8: `)` can only be used to close a subshell"),
		flipConfirm(LangPOSIX), // dash bug?
	),
	errCase(
		"<<$bar\n$bar",
		langErr("1:3: expansions not allowed in heredoc words"),
		flipConfirmAll, // we are stricter
	),
	errCase(
		"<<${bar}\n${bar}",
		langErr("1:3: expansions not allowed in heredoc words"),
		flipConfirmAll, // we are stricter
	),
	errCase(
		"<<$(bar)\n$(bar)",
		langErr("1:3: expansions not allowed in heredoc words", LangBash),
		flipConfirmAll, // we are stricter
	),

	errCase(
		"<<$-\n$-",
		langErr("1:3: expansions not allowed in heredoc words"),
		flipConfirmAll, // we are stricter
	),
	errCase(
		"<<`bar`\n`bar`",
		langErr("1:3: expansions not allowed in heredoc words"),
		flipConfirmAll, // we are stricter
	),
	errCase(
		"<<\"$bar\"\n$bar",
		langErr("1:4: expansions not allowed in heredoc words"),
		flipConfirmAll, // we are stricter
	),
	errCase(
		"<<a <<0\n$(<<$<<",
		langErr("2:6: `<<` must be followed by a word"),
	),
	errCase(
		`""()`,
		langErr("1:1: invalid func name"),
		flipConfirm(LangMirBSDKorn), // TODO: support non-literal func names, even empty ones?
	),
	errCase(
		"]] )",
		langErr("1:1: `]]` can only be used to close a test"),
		langErr("1:4: a command can only contain words and redirects; encountered `)`", LangPOSIX),
	),
	errCase(
		"((foo",
		langErr("1:1: reached EOF without matching `((` with `))`", LangBash|LangMirBSDKorn|LangZsh),
		langErr("1:2: reached EOF without matching `(` with `)`", LangPOSIX),
	),
	errCase(
		"(())",
		langErr("1:1: `((` must be followed by an expression", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"echo ((foo",
		langErr("1:6: `((` can only be used to open an arithmetic cmd", LangBash|LangMirBSDKorn|LangZsh),
		langErr("1:1: `foo(` must be followed by `)`", LangPOSIX),
	),
	errCase(
		"echo |&",
		langErr("1:6: `|&` must be followed by a statement", LangBash|LangZsh),
		langErr("1:6: `|` must be followed by a statement", LangPOSIX),
	),
	errCase(
		"|& a",
		langErr("1:1: `|&` is not a valid start for a statement", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"foo |& bar",
		langErr("1:5: `|` must be followed by a statement", LangPOSIX),
	),
	errCase(
		"let",
		langErr("1:1: `let` must be followed by an expression", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"let a+ b",
		langErr("1:6: `+` must be followed by an expression", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"let + a",
		langErr("1:5: `+` must be followed by an expression", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"let a ++",
		langErr("1:7: `++` must be followed by a literal", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"let (a)++",
		langErr("1:8: `++` must follow a name", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"let 1++",
		langErr("1:6: `++` must follow a name", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"let $0++",
		langErr("1:7: `++` must follow a name", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"let --(a)",
		langErr("1:5: `--` must be followed by a literal", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"let --$a",
		langErr("1:5: `--` must be followed by a literal", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"let a+\n",
		langErr("1:6: `+` must be followed by an expression", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"let ))",
		langErr("1:1: `let` must be followed by an expression", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"`let !`",
		langErr("1:6: `!` must be followed by an expression", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"let a:b",
		langErr("1:6: ternary operator missing `?` before `:`", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"let a+b=c",
		langErr("1:8: `=` must follow a name", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"`let` { foo; }",
		langErr("1:2: `let` must be followed by an expression", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"$(let)",
		langErr("1:3: `let` must be followed by an expression", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"[[",
		langErr("1:1: `[[` must be followed by an expression", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"[[ ]]",
		langErr("1:1: `[[` must be followed by an expression", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"[[ a",
		langErr("1:1: reached EOF without matching `[[` with `]]`", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"[[ a ||",
		langErr("1:6: `||` must be followed by an expression", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"[[ a && &&",
		langErr("1:6: `&&` must be followed by an expression", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"[[ a && ]]",
		langErr("1:6: `&&` must be followed by an expression", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"[[ a ==",
		langErr("1:6: `==` must be followed by a word", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"[[ a =~",
		langErr("1:6: `=~` must be followed by a word", LangBash|LangZsh),
		langErr("1:6: regex tests are a bash/zsh feature; tried parsing as LANG", LangMirBSDKorn),
	),
	errCase(
		"[[ -f a",
		langErr("1:1: reached EOF without matching `[[` with `]]`", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"[[ -n\na ]]",
		langErr("1:4: `-n` must be followed by a word", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"[[ a -ef\nb ]]",
		langErr("1:6: `-ef` must be followed by a word", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"[[ a ==\nb ]]",
		langErr("1:6: `==` must be followed by a word", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"[[ a -nt b",
		langErr("1:1: reached EOF without matching `[[` with `]]`", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"[[ a =~ b",
		langErr("1:1: reached EOF without matching `[[` with `]]`", LangBash|LangZsh),
	),
	errCase(
		"[[ a b c ]]",
		langErr("1:6: not a valid test operator: `b`", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"[[ a b$x c ]]",
		langErr("1:6: test operator words must consist of a single literal", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"[[ a & b ]]",
		langErr("1:6: not a valid test operator: `&`", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"[[ true && () ]]",
		langErr("1:12: `(` must be followed by an expression", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"[[ true && (&& ]]",
		langErr("1:12: `(` must be followed by an expression", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"[[ a == ! b ]]",
		langErr("1:11: not a valid test operator: `b`", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"[[ (! ) ]]",
		langErr("1:5: `!` must be followed by an expression", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"[[ ! && ]]",
		langErr("1:4: `!` must be followed by an expression", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"[[ (-e ) ]]",
		langErr("1:5: `-e` must be followed by a word", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"[[ (a) == b ]]",
		langErr("1:8: expected `&&`, `||` or `]]` after complex expr", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"[[ a =~ ; ]]",
		langErr("1:6: `=~` must be followed by a word", LangBash|LangZsh),
	),
	errCase(
		"[[ a =~ )",
		langErr("1:6: `=~` must be followed by a word", LangBash|LangZsh),
	),
	errCase(
		"[[ a =~ ())",
		langErr("1:1: reached `)` without matching `[[` with `]]`", LangBash|LangZsh),
	),
	errCase(
		"[[ >",
		langErr("1:1: `[[` must be followed by an expression", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"local (",
		langErr("1:7: `local` must be followed by names or assignments", LangBash),
		langErr("1:7: reached EOF without matching `(` with `)`", LangZsh),
	),
	errCase(
		"declare 0=${o}",
		langErr("1:9: invalid var name", LangBash|LangZsh),
	),
	errCase(
		"declare ab=${o})",
		langErr("1:16: statements must be separated by &, ; or a newline", LangBash|LangZsh),
	),
	errCase(
		"export ab=$0(",
		langErr("1:13: `export` must be followed by names or assignments", LangBash),
		langErr("1:13: reached EOF without matching `(` with `)`", LangZsh),
	),
	errCase(
		"declare {x,y}=(1 2)",
		langErr("1:15: `declare` must be followed by names or assignments", LangBash),
	),
	errCase(
		"a=(<)",
		langErr("1:4: array element values must be words", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"a=([)",
		langErr("1:4: `[` must be followed by an expression", LangBash|LangZsh),
	),
	errCase(
		"a=([i)",
		langErr("1:4: reached `)` without matching `[` with `]`", LangBash|LangZsh),
	),
	errCase(
		"a=([i])",
		langErr("1:4: `[x]` must be followed by `=`", LangBash|LangZsh),
		flipConfirmAll, // TODO: why is this valid?
	),
	errCase(
		"a[i]=(y)",
		langErr("1:5: arrays cannot be nested", LangBash),
	),
	errCase(
		"a=([i]=(y))",
		langErr("1:7: arrays cannot be nested", LangBash|LangZsh),
	),
	errCase(
		"o=([0]=#",
		langErr("1:8: array element values must be words", LangBash|LangZsh),
	),
	errCase(
		"a[b] ==[",
		langErr("1:1: `a[b]` must be followed by `=`", LangBash|LangZsh),
		flipConfirmAll, // stringifies
	),
	errCase(
		"a[b] +=c",
		langErr("1:1: `a[b]` must be followed by `=`", LangBash|LangZsh),
		flipConfirmAll, // stringifies
	),
	errCase(
		"a=(x y) foo",
		langErr("1:1: inline variables cannot be arrays", LangBash|LangZsh),
		flipConfirmAll, // stringifies
	),
	errCase(
		"a[2]=x foo",
		langErr("1:1: inline variables cannot be arrays", LangBash|LangZsh),
		flipConfirmAll, // stringifies
	),
	errCase(
		"function",
		langErr("1:1: `function` must be followed by a name", LangBash|LangMirBSDKorn),
		langErr("1:1: `foo()` must be followed by a statement", LangZsh),
	),
	errCase(
		"function foo(",
		langErr("1:1: `function foo(` must be followed by `)`", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"function `function",
		langErr("1:1: `function` must be followed by a name", LangBash|LangMirBSDKorn),
		langErr("1:11: `foo()` must be followed by a statement", LangZsh),
	),
	errCase(
		`function "foo"(){}`,
		langErr("1:1: `function` must be followed by a name", LangBash|LangMirBSDKorn),
		langErr("1:10: invalid func name", LangZsh),
	),
	errCase(
		"function foo()",
		langErr("1:1: `foo()` must be followed by a statement", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"@test",
		langErr("1:1: `@test` must be followed by a description word", LangBats),
	),
	errCase(
		"@test 'desc'",
		langErr("1:1: `@test \"desc\"` must be followed by a statement", LangBats),
	),
	errCase(
		"echo <<<",
		langErr("1:6: `<<<` must be followed by a word", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"a[",
		langErr("1:2: `[` must be followed by an expression", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"a[b",
		langErr("1:2: reached EOF without matching `[` with `]`", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"a[]",
		langErr("1:2: `[` must be followed by an expression", LangBash|LangMirBSDKorn|LangZsh),
		flipConfirmAll, // is cmd
	),
	errCase(
		"a[[",
		langErr("1:3: `[` must follow a name like a[i]", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"echo $((a[))",
		langErr("1:10: `[` must be followed by an expression", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"echo $((a[b))",
		langErr("1:10: reached `)` without matching `[` with `]`", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"echo $((a[]))",
		langErr("1:10: `[` must be followed by an expression", LangBash|LangMirBSDKorn|LangZsh),
		flipConfirm(LangMirBSDKorn), // wrong?
	),
	errCase(
		"echo $((x$t[",
		langErr("1:12: `[` must follow a name like a[i]", LangBash|LangMirBSDKorn),
		langErr("1:12: `[` must be followed by an expression", LangZsh),
	),
	errCase(
		"a[1]",
		langErr("1:1: `a[b]` must be followed by `=`", LangBash|LangMirBSDKorn|LangZsh),
		flipConfirmAll, // is cmd
	),
	errCase(
		"a[i]+",
		langErr("1:1: `a[b]+` must be followed by `=`", LangBash|LangMirBSDKorn|LangZsh),
		flipConfirmAll, // is cmd
	),
	errCase(
		"a[1]#",
		langErr("1:1: `a[b]` must be followed by `=`", LangBash|LangMirBSDKorn|LangZsh),
		flipConfirmAll, // is cmd
	),
	errCase(
		"echo $[foo",
		langErr("1:6: reached EOF without matching `$[` with `]`", LangBash),
	),
	errCase(
		"echo $'",
		langErr("1:6: reached EOF without closing quote `'`", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		`echo $"`,
		langErr("1:6: reached EOF without closing quote `\"`", LangBash|LangMirBSDKorn),
	),
	errCase(
		"echo @(",
		langErr("1:6: reached EOF without matching `@(` with `)`", LangBash|LangMirBSDKorn),
	),
	errCase(
		"echo @(a",
		langErr("1:6: reached EOF without matching `@(` with `)`", LangBash|LangMirBSDKorn),
	),
	errCase(
		"echo @([abc)])",
		langErr("1:14: a command can only contain words and redirects; encountered `)`", LangBash|LangMirBSDKorn),
	),
	errCase(
		"((@(",
		langErr("1:4: not a valid arithmetic operator: `(`", LangBash|LangMirBSDKorn),
	),
	errCase(
		"time { foo;",
		langErr("1:6: reached EOF without matching `{` with `}`", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"time ! foo",
		langErr("1:6: `!` can only be used in full statements", LangBash|LangMirBSDKorn|LangZsh),
		flipConfirm(LangBash), // TODO: why is this valid?
	),
	errCase(
		"coproc",
		langErr("1:1: coproc clause requires a command", LangBash),
	),
	errCase(
		"coproc\n$",
		langErr("1:1: coproc clause requires a command", LangBash),
	),
	errCase(
		"coproc declare (",
		langErr("1:16: `declare` must be followed by names or assignments", LangBash),
	),
	errCase(
		"echo ${foo[1 2]}",
		langErr("1:14: not a valid arithmetic operator: `2`", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"echo ${foo[}",
		langErr("1:11: `[` must be followed by an expression", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"echo ${foo]}",
		langErr("1:11: not a valid parameter expansion operator: `]`", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"echo ${foo[]}",
		langErr("1:11: `[` must be followed by an expression", LangBash|LangMirBSDKorn|LangZsh),
		flipConfirm(LangMirBSDKorn), // TODO: why is this valid?
	),
	errCase(
		"echo ${a/\n",
		langErr("1:6: reached EOF without matching `${` with `}`", LangBash|LangMirBSDKorn),
	),
	errCase(
		"echo ${a/''",
		langErr("1:6: reached EOF without matching `${` with `}`", LangBash|LangMirBSDKorn),
	),
	errCase(
		"echo ${a-\n",
		langErr("1:6: reached EOF without matching `${` with `}`", LangBash|LangMirBSDKorn),
	),
	errCase(
		"echo ${foo:",
		langErr("1:11: `:` must be followed by an expression", LangBash|LangMirBSDKorn),
	),
	errCase(
		"foo=force_expansion; echo ${foo:1 2}",
		langErr("1:35: not a valid arithmetic operator: `2`", LangBash|LangMirBSDKorn),
	),
	errCase(
		"echo ${foo:1",
		langErr("1:6: reached EOF without matching `${` with `}`", LangBash|LangMirBSDKorn),
	),
	errCase(
		"echo ${foo:1:",
		langErr("1:13: `:` must be followed by an expression", LangBash|LangMirBSDKorn),
	),
	errCase(
		"echo ${foo:1:2",
		langErr("1:6: reached EOF without matching `${` with `}`", LangBash|LangMirBSDKorn),
	),
	errCase(
		"echo ${foo:h",
		langErr("1:6: reached EOF without matching `${` with `}`", LangZsh),
	),
	errCase(
		"echo ${foo,",
		langErr("1:6: reached EOF without matching `${` with `}`", LangBash),
	),
	errCase(
		"echo ${foo@",
		langErr("1:11: @ expansion operator requires a literal", LangBash),
	),
	errCase(
		"foo=force_expansion; echo ${foo@}",
		langErr("1:33: @ expansion operator requires a literal", LangBash),
	),
	errCase(
		"echo ${foo@Q",
		langErr("1:6: reached EOF without matching `${` with `}`", LangBash),
	),
	errCase(
		"foo=force_expansion; echo ${foo@bar}",
		langErr("1:33: invalid @ expansion operator `bar`", LangBash),
	),
	errCase(
		"foo=force_expansion; echo ${foo@'Q'}",
		langErr("1:33: @ expansion operator requires a literal", LangBash),
	),
	errCase(
		`echo $((echo a); (echo b))`,
		langErr("1:14: not a valid arithmetic operator: `a`", LangBash|LangMirBSDKorn|LangZsh),
		flipConfirmAll, // note that we don't backtrack
	),
	errCase(
		`((echo a); (echo b))`,
		langErr("1:8: not a valid arithmetic operator: `a`", LangBash|LangMirBSDKorn|LangZsh),
		flipConfirmAll, // note that we don't backtrack
	),
	errCase(
		"for ((;;",
		langErr("1:5: reached EOF without matching `((` with `))`", LangBash),
	),
	errCase(
		"for ((;;0000000",
		langErr("1:5: reached EOF without matching `((` with `))`", LangBash),
	),
	errCase(
		"echo <(",
		langErr("1:6: `<` must be followed by a word", LangPOSIX|LangMirBSDKorn),
	),
	errCase(
		"echo >(",
		langErr("1:6: `>` must be followed by a word", LangPOSIX|LangMirBSDKorn),
	),
	errCase(
		"echo {var}>foo",
		langErr("1:6: `{varname}` redirects are a bash feature; tried parsing as LANG", LangPOSIX|LangMirBSDKorn),
		// shells treat {var} as an argument, but we are a bit stricter
		// so that users won't think this will work like they expect in POSIX shell.
		flipConfirmAll,
	),
	errCase(
		"echo ;&",
		langErr("1:7: `&` can only immediately follow a statement", LangPOSIX),
		langErr("1:6: `;&` can only be used in a case clause", LangBash|LangMirBSDKorn|LangZsh),
	),
	errCase(
		"echo ;;&",
		langErr("1:6: `;;` can only be used in a case clause", LangPOSIX|LangMirBSDKorn),
	),
	errCase(
		"echo ;|",
		langErr("1:7: `|` can only immediately follow a statement", LangPOSIX|LangBash),
	),
	errCase(
		"for i in 1 2 3; { echo; }",
		langErr("1:17: for loops with braces are a bash/mksh feature; tried parsing as LANG", LangPOSIX),
	),
	errCase(
		"echo !(a)",
		langErr("1:6: extended globs are a bash/mksh feature; tried parsing as LANG", LangPOSIX),
	),
	errCase(
		"echo $a@(b)",
		langErr("1:8: extended globs are a bash/mksh feature; tried parsing as LANG", LangPOSIX),
	),
	errCase(
		"foo=(1 2)",
		langErr("1:5: arrays are a bash/mksh/zsh feature; tried parsing as LANG", LangPOSIX),
	),
	errCase(
		"a=$c\n'",
		langErr("2:1: reached EOF without closing quote `'`"),
	),
	errCase(
		"echo ${!foo[@]}",
		langErr("1:6: `${!foo}` is a bash/mksh feature; tried parsing as LANG", LangPOSIX),
	),
	errCase(
		"foo << < bar",
		langErr("1:5: `<<` must be followed by a word", LangPOSIX),
	),
	errCase(
		"echo ${foo,bar}",
		langErr("1:11: this expansion operator is a bash feature; tried parsing as LANG", LangPOSIX|LangMirBSDKorn),
	),
	errCase(
		"echo ${foo@Q}",
		langErr("1:11: this expansion operator is a bash/mksh feature; tried parsing as LANG", LangPOSIX),
	),
	errCase(
		"echo ${foo@a}",
		langErr("1:12: this expansion operator is a bash feature; tried parsing as LANG", LangMirBSDKorn),
	),
	errCase(
		"echo ${foo@u}",
		langErr("1:12: this expansion operator is a bash feature; tried parsing as LANG", LangMirBSDKorn),
	),
	errCase(
		"echo ${foo@A}",
		langErr("1:12: this expansion operator is a bash feature; tried parsing as LANG", LangMirBSDKorn),
	),
	errCase(
		"echo ${foo@E}",
		langErr("1:12: this expansion operator is a bash feature; tried parsing as LANG", LangMirBSDKorn),
	),
	errCase(
		"echo ${foo@K}",
		langErr("1:12: this expansion operator is a bash feature; tried parsing as LANG", LangMirBSDKorn),
	),
	errCase(
		"echo ${foo@k}",
		langErr("1:12: this expansion operator is a bash feature; tried parsing as LANG", LangMirBSDKorn),
	),
	errCase(
		"echo ${foo@L}",
		langErr("1:12: this expansion operator is a bash feature; tried parsing as LANG", LangMirBSDKorn),
	),
	errCase(
		"echo ${foo@P}",
		langErr("1:12: this expansion operator is a bash feature; tried parsing as LANG", LangMirBSDKorn),
	),
	errCase(
		"echo ${foo@U}",
		langErr("1:12: this expansion operator is a bash feature; tried parsing as LANG", LangMirBSDKorn),
	),
	errCase(
		"foo=force_expansion; echo ${foo@#}",
		langErr("1:33: this expansion operator is a mksh feature; tried parsing as LANG", LangBash),
	),
	errCase(
		"`\"`\\",
		langErr("1:2: reached \"`\" without closing quote `\"`"),
	),
}

func TestInputName(t *testing.T) {
	t.Parallel()
	in := "("
	want := "some-file.sh:1:1: `(` must be followed by a statement list"
	p := NewParser()
	_, err := p.Parse(strings.NewReader(in), "some-file.sh")
	if err == nil {
		t.Fatalf("Expected error in %q: %v", in, want)
	}
	got := err.Error()
	if got != want {
		t.Fatalf("Error mismatch in %q\nwant: %s\ngot:  %s",
			in, want, got)
	}
}

var errBadReader = fmt.Errorf("write: expected error")

type badReader struct{}

func (b badReader) Read(p []byte) (int, error) { return 0, errBadReader }

func TestReadErr(t *testing.T) {
	t.Parallel()
	p := NewParser()
	_, err := p.Parse(badReader{}, "")
	if err == nil {
		t.Fatalf("Expected error with bad reader")
	}
	if err != errBadReader {
		t.Fatalf("Error mismatch with bad reader:\nwant: %v\ngot:  %v",
			errBadReader, err)
	}
}

type strictStringReader struct {
	*strings.Reader
	gaveEOF bool
}

func newStrictReader(s string) *strictStringReader {
	return &strictStringReader{Reader: strings.NewReader(s)}
}

func (r *strictStringReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if err == io.EOF {
		if r.gaveEOF {
			return n, fmt.Errorf("duplicate EOF read")
		}
		r.gaveEOF = true
	}
	return n, err
}

func TestParseStmtsSeq(t *testing.T) {
	t.Parallel()
	p := NewParser()
	inReader, inWriter := io.Pipe()
	recv := make(chan bool, 10)
	errc := make(chan error, 1)
	go func() {
		var firstErr error
		for _, err := range p.StmtsSeq(inReader) {
			recv <- true
			if firstErr == nil && err != nil {
				firstErr = err
			}
		}
		errc <- firstErr
	}()
	io.WriteString(inWriter, "foo\n")
	<-recv
	io.WriteString(inWriter, "bar; baz")
	inWriter.Close()
	<-recv
	<-recv
	if err := <-errc; err != nil {
		t.Fatalf("Expected no error: %v", err)
	}
}

func TestParseStmtsSeqStopEarly(t *testing.T) {
	t.Parallel()
	p := NewParser()
	inReader, inWriter := io.Pipe()
	defer inWriter.Close()
	recv := make(chan bool, 10)
	errc := make(chan error, 1)
	go func() {
		var firstErr error
		for stmt, err := range p.StmtsSeq(inReader) {
			recv <- true
			if firstErr == nil && err != nil {
				firstErr = err
			}
			if stmt.Background {
				break
			}
		}
		errc <- firstErr
	}()
	io.WriteString(inWriter, "a\n")
	<-recv
	io.WriteString(inWriter, "b &\n") // stop here
	<-recv
	if err := <-errc; err != nil {
		t.Fatalf("Expected no error: %v", err)
	}
}

func TestParseStmtsSeqError(t *testing.T) {
	t.Parallel()
	for _, in := range []string{
		"foo; )",
		"bar; <<EOF",
	} {
		t.Run("", func(t *testing.T) {
			p := NewParser()
			recv := make(chan bool, 10)
			errc := make(chan error, 1)
			go func() {
				var firstErr error
				for _, err := range p.StmtsSeq(strings.NewReader(in)) {
					recv <- true
					if firstErr == nil && err != nil {
						firstErr = err
					}
				}
				errc <- firstErr
			}()
			<-recv
			if err := <-errc; err == nil {
				t.Fatalf("Expected an error in %q, but got nil", in)
			}
		})
	}
}

func TestParseWords(t *testing.T) {
	t.Parallel()
	p := NewParser()
	inReader, inWriter := io.Pipe()
	recv := make(chan bool, 10)
	errc := make(chan error, 1)
	go func() {
		errc <- p.Words(inReader, func(w *Word) bool {
			recv <- true
			return true
		})
	}()
	// TODO: Allow a single space to end parsing a word. At the moment, the
	// parser must read the next non-space token (the next literal or
	// newline, in this case) to finish parsing a word.
	io.WriteString(inWriter, "foo ")
	io.WriteString(inWriter, "bar\n")
	<-recv
	io.WriteString(inWriter, "baz etc")
	inWriter.Close()
	<-recv
	<-recv
	<-recv
	if err := <-errc; err != nil {
		t.Fatalf("Expected no error: %v", err)
	}
}

func TestParseWordsStopEarly(t *testing.T) {
	t.Parallel()
	p := NewParser()
	r := strings.NewReader("a\nb\nc\n")
	parsed := 0
	err := p.Words(r, func(w *Word) bool {
		parsed++
		return w.Lit() != "b"
	})
	if err != nil {
		t.Fatalf("Expected no error: %v", err)
	}
	if want := 2; parsed != want {
		t.Fatalf("wanted %d words parsed, got %d", want, parsed)
	}
}

func TestParseWordsError(t *testing.T) {
	t.Parallel()
	in := "foo )"
	p := NewParser()
	recv := make(chan bool, 10)
	errc := make(chan error, 1)
	go func() {
		errc <- p.Words(strings.NewReader(in), func(w *Word) bool {
			recv <- true
			return true
		})
	}()
	<-recv
	want := "1:5: `)` is not a valid word"
	got := fmt.Sprintf("%v", <-errc)
	if got != want {
		t.Fatalf("Expected %q as an error, but got %q", want, got)
	}
}

var documentTests = []struct {
	in   string
	want []WordPart
}{
	{
		"foo",
		[]WordPart{lit("foo")},
	},
	{
		" foo  $bar",
		[]WordPart{
			lit(" foo  "),
			litParamExp("bar"),
		},
	},
	{
		"$bar\n\n",
		[]WordPart{
			litParamExp("bar"),
			lit("\n\n"),
		},
	},
}

func TestParseDocument(t *testing.T) {
	t.Parallel()
	p := NewParser()

	for _, tc := range documentTests {
		t.Run("", func(t *testing.T) {
			got, err := p.Document(strings.NewReader(tc.in))
			if err != nil {
				t.Fatal(err)
			}
			Walk(got, sanityChecker{tb: t, src: tc.in}.visit)
			want := &Word{Parts: tc.want}
			qt.Assert(t, qt.CmpEquals(got, want, cmpOpt))
		})
	}
}

func TestParseDocumentError(t *testing.T) {
	t.Parallel()
	in := "foo $("
	p := NewParser()
	_, err := p.Document(strings.NewReader(in))
	want := "1:5: reached EOF without matching `$(` with `)`"
	got := fmt.Sprintf("%v", err)
	if got != want {
		t.Fatalf("Expected %q as an error, but got %q", want, got)
	}
}

var arithmeticTests = []struct {
	in   string
	want ArithmExpr
}{
	{
		"foo",
		litWord("foo"),
	},
	{
		"3 + 4",
		&BinaryArithm{
			Op: Add,
			X:  litWord("3"),
			Y:  litWord("4"),
		},
	},
	{
		"3 + 4 + 5",
		&BinaryArithm{
			Op: Add,
			X: &BinaryArithm{
				Op: Add,
				X:  litWord("3"),
				Y:  litWord("4"),
			},
			Y: litWord("5"),
		},
	},
	{
		"1 ? 0 : 2",
		&BinaryArithm{
			Op: TernQuest,
			X:  litWord("1"),
			Y: &BinaryArithm{
				Op: TernColon,
				X:  litWord("0"),
				Y:  litWord("2"),
			},
		},
	},
	{
		"a = 3, ++a, a--",
		&BinaryArithm{
			Op: Comma,
			X: &BinaryArithm{
				Op: Comma,
				X: &BinaryArithm{
					Op: Assgn,
					X:  litWord("a"),
					Y:  litWord("3"),
				},
				Y: &UnaryArithm{
					Op: Inc,
					X:  litWord("a"),
				},
			},
			Y: &UnaryArithm{
				Op:   Dec,
				Post: true,
				X:    litWord("a"),
			},
		},
	},
}

func TestParseArithmetic(t *testing.T) {
	t.Parallel()
	p := NewParser()

	for _, tc := range arithmeticTests {
		t.Run("", func(t *testing.T) {
			got, err := p.Arithmetic(strings.NewReader(tc.in))
			if err != nil {
				t.Fatal(err)
			}
			Walk(got, sanityChecker{tb: t, src: tc.in}.visit)
			qt.Assert(t, qt.CmpEquals(got, tc.want, cmpOpt))
		})
	}
}

func TestParseArithmeticError(t *testing.T) {
	t.Parallel()
	in := "3 +"
	p := NewParser()
	_, err := p.Arithmetic(strings.NewReader(in))
	want := "1:3: `+` must be followed by an expression"
	got := fmt.Sprintf("%v", err)
	if got != want {
		t.Fatalf("Expected %q as an error, but got %q", want, got)
	}
}

var stopAtTests = []struct {
	in   string
	stop string
	want any
}{
	{
		"foo bar", "$$",
		litCall("foo", "bar"),
	},
	{
		"$foo $", "$$",
		call(word(litParamExp("foo")), litWord("$")),
	},
	{
		"echo foo $$", "$$",
		litCall("echo", "foo"),
	},
	{
		"$$", "$$",
		&File{},
	},
	{
		"echo foo\n$$\n", "$$",
		litCall("echo", "foo"),
	},
	{
		"echo foo; $$", "$$",
		litCall("echo", "foo"),
	},
	{
		"echo foo; $$", "$$",
		litCall("echo", "foo"),
	},
	{
		"echo foo;$$", "$$",
		litCall("echo", "foo"),
	},
	{
		"echo '$$'", "$$",
		call(litWord("echo"), word(sglQuoted("$$"))),
	},
}

func TestParseStopAt(t *testing.T) {
	t.Parallel()
	for _, c := range stopAtTests {
		p := NewParser(StopAt(c.stop))
		want := fullProg(c.want)
		t.Run("", singleParse(p, c.in, want))
	}
}

func TestValidName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"Empty", "", false},
		{"Simple", "foo", true},
		{"MixedCase", "Foo", true},
		{"Underscore", "_foo", true},
		{"NumberPrefix", "3foo", false},
		{"NumberSuffix", "foo3", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ValidName(tc.in)
			if got != tc.want {
				t.Fatalf("ValidName(%q) got %t, wanted %t",
					tc.in, got, tc.want)
			}
		})
	}
}

func TestIsIncomplete(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in       string
		notWords bool
		want     bool
	}{
		{in: "foo\n", want: false},
		{in: "foo;", want: false},
		{in: "\n", want: false},
		{in: "badsyntax)", want: false},
		{in: "foo 'incomp", want: true},
		{in: `foo "incomp`, want: true},
		{in: "foo ${incomp", want: true},

		{in: "foo; 'incomp", notWords: true, want: true},
		{in: `foo; "incomp`, notWords: true, want: true},
		{in: " (incomp", notWords: true, want: true},
	}
	p := NewParser()
	for i, tc := range tests {
		t.Run(fmt.Sprintf("Parse%02d", i), func(t *testing.T) {
			r := strings.NewReader(tc.in)
			_, err := p.Parse(r, "")
			if got := IsIncomplete(err); got != tc.want {
				t.Fatalf("%q got %t, wanted %t", tc.in, got, tc.want)
			}
		})
		t.Run(fmt.Sprintf("Interactive%02d", i), func(t *testing.T) {
			r := strings.NewReader(tc.in)
			var firstErr error
			for _, err := range p.InteractiveSeq(r) {
				if firstErr == nil && err != nil {
					firstErr = err
				}
			}
			if got := IsIncomplete(firstErr); got != tc.want {
				t.Fatalf("%q got %t, wanted %t", tc.in, got, tc.want)
			}
		})
		if !tc.notWords {
			t.Run(fmt.Sprintf("WordsSeq%02d", i), func(t *testing.T) {
				r := strings.NewReader(tc.in)
				var firstErr error
				for _, err := range p.WordsSeq(r) {
					if firstErr == nil && err != nil {
						firstErr = err
					}
				}
				if got := IsIncomplete(firstErr); got != tc.want {
					t.Fatalf("%q got %t, wanted %t", tc.in, got, tc.want)
				}
			})
		}
	}
}

func TestPosEdgeCases(t *testing.T) {
	in := "`\\\\foo`\n" + // one escaped backslash and 3 bytes
		"\x00foo\x00bar\n" // 8 bytes and newline
	p := NewParser()
	f, err := p.Parse(strings.NewReader(in), "")
	qt.Assert(t, qt.IsNil(err))
	cmdSubst := f.Stmts[0].Cmd.(*CallExpr).Args[0].Parts[0].(*CmdSubst)
	lit := cmdSubst.Stmts[0].Cmd.(*CallExpr).Args[0].Parts[0].(*Lit)

	qt.Check(t, qt.Equals(lit.Value, lit.Value))
	// Note that positions of literals with escape sequences inside backquote command substitutions
	// are weird, since we effectively skip over the double escaping in the literal value and positions.
	// Even though the input source has '\\foo' between columns 2 and 7 (length 5)
	// we end up keeping '\foo' between columns 3 and 7 (length 4).
	qt.Check(t, qt.Equals(lit.ValuePos.String(), "1:3"))
	qt.Check(t, qt.Equals(lit.ValueEnd.String(), "1:7"))

	// Check that we skip over null bytes when counting columns.
	qt.Check(t, qt.Equals(f.Stmts[1].Pos().String(), "2:2"))
	qt.Check(t, qt.Equals(f.Stmts[1].End().String(), "2:9"))
}

func TestParseRecoverErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		src string

		wantErr     bool
		wantMissing int
	}{
		{src: "foo;"},
		{src: "foo"},
		{
			src:         "'incomp",
			wantMissing: 1,
		},
		{
			src:         "foo; 'incomp",
			wantMissing: 1,
		},
		{
			src:         "{ incomp",
			wantMissing: 1,
		},
		{
			src:         "(incomp",
			wantMissing: 1,
		},
		{
			src:         "(incomp; foo",
			wantMissing: 1,
		},
		{
			src:         "$(incomp",
			wantMissing: 1,
		},
		{
			src:         "((incomp",
			wantMissing: 1,
		},
		{
			src:         "$((incomp",
			wantMissing: 1,
		},
		{
			src:         "if foo",
			wantMissing: 3,
		},
		{
			src:         "if foo; then bar",
			wantMissing: 1,
		},
		{
			src:         "for i in 1 2 3; echo $i; done",
			wantMissing: 1,
		},
		{
			src:         `"incomp`,
			wantMissing: 1,
		},
		{
			src:         "`incomp",
			wantMissing: 1,
		},
		{
			src:         "incomp >",
			wantMissing: 1,
		},
		{
			src:         "${incomp",
			wantMissing: 1,
		},
		{
			src:         "incomp | ",
			wantMissing: 1,
		},
		{
			src:         "incomp || ",
			wantMissing: 1,
		},
		{
			src:         "incomp && ",
			wantMissing: 1,
		},
		{
			src:         `(one | { two >`,
			wantMissing: 3,
		},
		{
			src:         `(one > ; two | ); { three`,
			wantMissing: 3,
		},
		{
			src:     "badsyntax)",
			wantErr: true,
		},
	}
	parser := NewParser(RecoverErrors(3))
	printer := NewPrinter()
	for _, tc := range tests {
		t.Run("", func(t *testing.T) {
			t.Logf("input: %s", tc.src)
			r := strings.NewReader(tc.src)
			f, err := parser.Parse(r, "")
			if tc.wantErr {
				qt.Assert(t, qt.Not(qt.IsNil(err)))
			} else {
				qt.Assert(t, qt.IsNil(err))
				switch len(f.Stmts) {
				case 0:
					t.Fatalf("result has no statements")
				case 1:
					if f.Stmts[0].Pos().IsRecovered() {
						t.Fatalf("result is only a recovered statement")
					}
				}
			}
			qt.Assert(t, qt.Equals(countRecoveredPositions(reflect.ValueOf(f)), tc.wantMissing))

			// Check that walking or printing the syntax tree still appears to work
			// even when the input source was incomplete.
			Walk(f, func(node Node) bool {
				if node == nil {
					return true
				}
				// Each position should either be valid, pointing to an offset within the input,
				// or invalid, which could be due to the position being recovered.
				for _, pos := range []Pos{node.Pos(), node.End()} {
					qt.Assert(t, qt.IsFalse(pos.IsValid() && pos.IsRecovered()), qt.Commentf("positions cannot be valid and recovered"))
					if !pos.IsValid() {
						qt.Assert(t, qt.Equals(pos.Offset(), 0), qt.Commentf("invalid positions have no offset"))
						qt.Assert(t, qt.Equals(pos.Line(), 0), qt.Commentf("invalid positions have no line"))
						qt.Assert(t, qt.Equals(pos.Col(), 0), qt.Commentf("invalid positions have no column"))
					}
				}
				return true
			})
			// Note that we don't particularly care about good formatting here.
			printer.Print(io.Discard, f)
		})
	}
}

func countRecoveredPositions(x reflect.Value) int {
	switch x.Kind() {
	case reflect.Interface:
		return countRecoveredPositions(x.Elem())
	case reflect.Pointer:
		if !x.IsNil() {
			return countRecoveredPositions(x.Elem())
		}
	case reflect.Slice:
		n := 0
		for i := range x.Len() {
			n += countRecoveredPositions(x.Index(i))
		}
		return n
	case reflect.Struct:
		if pos, ok := x.Interface().(Pos); ok {
			if pos.IsRecovered() {
				return 1
			}
			return 0
		}
		n := 0
		for i := range x.NumField() {
			n += countRecoveredPositions(x.Field(i))
		}
		return n
	}
	return 0
}
