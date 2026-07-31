package toml_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/BurntSushi/toml/internal/tag"
	tomltest "github.com/BurntSushi/toml/internal/toml-test"
)

// Test if the error message matches what we want for invalid tests. Every slice
// entry is tested with strings.Contains.
//
// Filepaths are glob'd
var errorTests = map[string][]string{
	"encoding/bad-utf8*":             {"invalid UTF-8 byte"},
	"encoding/utf16*":                {"files cannot contain NULL bytes; probably using UTF-16"},
	"string/multiline-bad-escape-02": {`invalid escape: '\ '`},
}

// Test metadata; all keys listed as "keyname: type".
var metaTests = map[string]string{
	"implicit-and-explicit-after": `
		a.b.c:         Hash
		a.b.c.answer:  Integer
		a:             Hash
		a.better:      Integer
	`,
	"implicit-and-explicit-before": `
		a:             Hash
		a.better:      Integer
		a.b.c:         Hash
		a.b.c.answer:  Integer
	`,
	"key/case-sensitive": `
		sectioN:       String
		section:       Hash
		section.name:  String
		section.NAME:  String
		section.Name:  String
		Section:       Hash
		Section.name:  String
		Section."μ":   String
		Section."Μ":   String
		Section.M:     String
	`,
	"key/dotted-01": `
		name.first:            String
		name.last:             String
		many.dots.dot.dot.dot: Integer
	`,
	"key/dotted-02": `
		count.a: Integer
		count.b: Integer
		count.c: Integer
		count.d: Integer
		count.e: Integer
		count.f: Integer
		count.g: Integer
		count.h: Integer
		count.i: Integer
		count.j: Integer
		count.k: Integer
		count.l: Integer
	`,
	"key/dotted-03": `
		top.key:     Integer
		tbl:         Hash
		tbl.a.b.c:   Float
		a.few.dots:                   Hash
		a.few.dots.polka.dot:         String
		a.few.dots.polka.dance-with:  String
	`,
	"key/dotted-04": `
		top.key:     Integer
		arr:         ArrayHash
		arr.a.b.c:   Integer
		arr.a.b.d:   Integer
		arr:         ArrayHash
		arr.a.b.c:   Integer
		arr.a.b.d:   Integer
	 `,
	"key/empty-01": `
		"": String
	`,
	"key/quoted-dots": `
		plain:                          Integer
		"with.dot":                     Integer
		plain_table:                    Hash
		plain_table.plain:              Integer
		plain_table."with.dot":         Integer
		table.withdot:                  Hash
		table.withdot.plain:            Integer
		table.withdot."key.with.dots":  Integer
		table.withdot."escaped.dot":  Integer
	`,
	// TODO: doesn't deal correctly with the double spaces; probably an error in
	// the test rather than code
	// "key/space": `
	// 	"a b": Integer
	// 	" c d ": Integer
	// 	"  much \t\t  whitespace  \t\n  \r\n  ": Integer
	// 	" tbl ": Hash
	// 	" tbl "."\ttab\ttab\t": String
	// `,
	"key/special-chars": "\n" +
		"\"=~!@$^&*()_+-`1234567890[]|/?><.,;:'=\": Integer\n",

	// TODO: "(albums): Hash" is missing; the problem is that this is an
	// "implied key", which is recorded in the parser in implicits, rather than
	// in keys. This is to allow "redefining" tables, for example:
	//
	//    [a.b.c]
	//    answer = 42
	//    [a]
	//    better = 43
	//
	// However, we need to actually pass on this information to the MetaData so
	// we can use it.
	//
	// Keys are supposed to be in order, for the above right now that's:
	//
	//     (a).(b).(c):           Hash
	//     (a).(b).(c).(answer):  Integer
	//     (a):                   Hash
	//     (a).(better):          Integer
	//
	// So if we want to add "(a).(b): Hash", where should this be in the order?
	"table/array-implicit": `
		albums.songs:       ArrayHash
		albums.songs.name:  String
	`,

	// TODO: people and people.* listed many times; not entirely sure if that's
	// what we want?
	//
	// It certainly causes problems, because keys is a slice, and types a map.
	// So if array entry 1 differs in type from array entry 2 then that won't be
	// recorded right. This related to the problem in the above comment.
	//
	// people:                ArrayHash
	//
	// people[0]:             Hash
	// people[0].first_name:  String
	// people[0].last_name:   String
	//
	// people[1]:             Hash
	// people[1].first_name:  String
	// people[1].last_name:   String
	//
	// people[2]:             Hash
	// people[2].first_name:  String
	// people[2].last_name:   String
	"table/array-many": `
		people:             ArrayHash
		people.first_name:  String
		people.last_name:   String
		people:             ArrayHash
		people.first_name:  String
		people.last_name:   String
		people:             ArrayHash
		people.first_name:  String
		people.last_name:   String
	`,
	"table/array-nest": `
		albums:             ArrayHash
		albums.name:        String
		albums.songs:       ArrayHash
		albums.songs.name:  String
		albums.songs:       ArrayHash
		albums.songs.name:  String
		albums:             ArrayHash
		albums.name:        String
		albums.songs:       ArrayHash
		albums.songs.name:  String
		albums.songs:       ArrayHash
		albums.songs.name:  String
	`,
	"table/array-one": `
		people:             ArrayHash
		people.first_name:  String
		people.last_name:   String
	`,
	"table/array-table-array": `
		a:        ArrayHash
		a.b:      ArrayHash
		a.b.c:    Hash
		a.b.c.d:  String
		a.b:      ArrayHash
		a.b.c:    Hash
		a.b.c.d:  String
	`,
	"table/empty": `
		a: Hash
	`,
	"table/keyword": `
		true:   Hash
		false:  Hash
		inf:    Hash
		nan:    Hash
	`,
	"table/names": `
		a.b.c:    Hash
		a."b.c":  Hash
		a."d.e":  Hash
		a." x ":  Hash
		d.e.f:    Hash
		g.h.i:    Hash
		j."ʞ".l:  Hash
		x.1.2:    Hash
	`,
	"table/no-eol": `
		table: Hash
	`,
	"table/sub-empty": `
		a:    Hash
		a.b:  Hash
	`,
	"table/whitespace": `
		"valid key": Hash
	`,
	"table/with-literal-string": `
		a:                   Hash
		a."\"b\"":           Hash
		a."\"b\"".c:         Hash
		a."\"b\"".c.answer:  Integer
	`,
	"table/with-pound": `
		"key#group":         Hash
		"key#group".answer:  Integer
	`,
	"table/with-single-quotes": `
		a:             Hash
		a.b:           Hash
		a.b.c:         Hash
		a.b.c.answer:  Integer
	`,
	"table/without-super": `
		x.y.z.w:  Hash
		x:        Hash
	`,
	"table/without-super-with-values": `
		x.y.z.w: Hash
		x.y.z.w.a: Integer
		x.y.z.w.b: Integer
		x: Hash
		x.c: Integer
		x.d: Integer
	`,
}

func TestToml(t *testing.T) {
	for k := range errorTests { // Make sure patterns are valid.
		_, err := filepath.Match(k, "")
		if err != nil {
			t.Fatal(err)
		}
	}

	// TODO: bit of a hack to make sure not all test run; without this "-run=.."
	// will still run all tests, but just report the errors for the -run value.
	// This is annoying in cases where you have some debug printf.
	//
	// Need to update toml-test a bit to make this easier, but this good enough
	// for now.
	var runTests []string
	for _, a := range os.Args {
		if strings.HasPrefix(a, "-test.run=TestToml/") {
			a = strings.TrimPrefix(a, "-test.run=TestToml/encode/")
			a = strings.TrimPrefix(a, "-test.run=TestToml/decode/")
			runTests = []string{a, a + "/*"}
			break
		}
	}

	// Make sure the keys in metaTests and errorTests actually exist; easy to
	// make a typo and nothing will get tested.
	var (
		shouldExistValid   = make(map[string]struct{})
		shouldExistInvalid = make(map[string]struct{})
	)
	if len(runTests) == 0 {
		for k := range metaTests {
			shouldExistValid["valid/"+k] = struct{}{}
		}
		for k := range errorTests {
			shouldExistInvalid["invalid/"+k] = struct{}{}
		}
	}

	run := func(t *testing.T, enc bool) {
		r := tomltest.Runner{
			Version:  "1.1.0",
			Files:    tomltest.EmbeddedTests(),
			Encoder:  enc,
			Parser:   parser{},
			RunTests: runTests,
			SkipTests: []string{
				// Will be fixed in Go 1.23: https://github.com/BurntSushi/toml/issues/407
				"invalid/datetime/offset-overflow-hour",
				"invalid/datetime/offset-overflow-minute",

				// These tests are fine, just doesn't deal well with empty output.
				"valid/comment/noeol",
				"valid/comment/nonascii",
				"valid/empty-crlf",
				"valid/empty-lf",
				"valid/empty-nothing",
				"valid/empty-space",
				"valid/empty-tab",

				// TODO: fix this; we allow appending to tables, but shouldn't.
				"invalid/array/extend-defined-aot",
				"invalid/inline-table/duplicate-key-03",
				"invalid/table/duplicate-key-04",
				"invalid/table/duplicate-key-05",
				"invalid/inline-table/overwrite-02",
				"invalid/inline-table/overwrite-07",
				"invalid/inline-table/overwrite-08",
				"invalid/spec-1.0.0/inline-table-2-0",
				"invalid/spec-1.0.0/table-9-1",
				"invalid/spec-1.1.0/common-46-1",
				"invalid/spec-1.1.0/common-49-0",
				"invalid/table/append-to-array-with-dotted-keys",
				"invalid/table/append-with-dotted-keys-01",
				"invalid/table/append-with-dotted-keys-02",
				"invalid/table/append-with-dotted-keys-03",
				"invalid/table/append-with-dotted-keys-05",
				"invalid/table/duplicate-key-dotted-table-01",
				"invalid/table/duplicate-key-dotted-table-02",
				"invalid/table/redefine-02",
				"invalid/table/redefine-03",
			},
		}

		tests, err := r.Run()
		if err != nil {
			t.Fatal(err)
		}

		for _, test := range tests.Tests {
			t.Run(test.Path, func(t *testing.T) {
				if test.Failed() {
					t.Fatalf("\nError:\n%s\n\nInput:\n%s\nOutput:\n%s\nWant:\n%s\n",
						test.Failure, test.Input, test.Output, test.Want)
					return
				}

				// Test error message.
				if test.Type() == tomltest.TypeInvalid {
					testError(t, test, shouldExistInvalid)
				}
				// Test metadata
				if !enc && test.Type() == tomltest.TypeValid {
					delete(shouldExistValid, test.Path)
					testMeta(t, test)
				}
			})
		}

		t.Logf("  valid: passed %d; failed %d", tests.PassedValid, tests.FailedValid)
		t.Logf("invalid: passed %d; failed %d", tests.PassedInvalid, tests.FailedInvalid)
		t.Logf("skipped: %d", tests.Skipped)
	}

	t.Run("decode", func(t *testing.T) { run(t, false) })
	t.Run("encode", func(t *testing.T) { run(t, true) })

	if len(shouldExistValid) > 0 {
		var s []string
		for k := range shouldExistValid {
			s = append(s, k)
		}
		t.Errorf("the following meta tests didn't match any files: %s", strings.Join(s, ", "))
	}
	if len(shouldExistInvalid) > 0 {
		var s []string
		for k := range shouldExistInvalid {
			s = append(s, k)
		}
		t.Errorf("the following meta tests didn't match any files: %s", strings.Join(s, ", "))
	}
}

var reCollapseSpace = regexp.MustCompile(` +`)

func testMeta(t *testing.T, test tomltest.Test) {
	want, ok := metaTests[strings.TrimPrefix(test.Path, "valid/")]
	if !ok {
		return
	}

	var s any
	meta, err := toml.Decode(test.Input, &s)
	if err != nil {
		t.Fatal(err)
	}

	b := new(strings.Builder)
	for i, k := range meta.Keys() {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(b, "%s: %s", k, meta.Type(k...))
	}
	have := b.String()

	want = reCollapseSpace.ReplaceAllString(strings.ReplaceAll(strings.TrimSpace(want), "\t", ""), " ")
	if have != want {
		t.Errorf("MetaData wrong\nhave:\n%s\nwant:\n%s", have, want)
	}
}

func testError(t *testing.T, test tomltest.Test, shouldExist map[string]struct{}) {
	path := strings.TrimPrefix(test.Path, "invalid/")

	errs, ok := errorTests[path]
	if ok {
		delete(shouldExist, "invalid/"+path)
	}
	if !ok {
		for k := range errorTests {
			ok, _ = filepath.Match(k, path)
			if ok {
				delete(shouldExist, "invalid/"+k)
				errs = errorTests[k]
				break
			}
		}
	}
	if !ok {
		return
	}

	for _, e := range errs {
		if !strings.Contains(test.Output, e) {
			t.Errorf("\nwrong error message\nhave: %s\nwant: %s", test.Output, e)
		}
	}
}

type parser struct{}

func (p parser) Encode(ctx context.Context, input string) (output string, outputIsError bool, retErr error) {
	defer func() {
		if r := recover(); r != nil {
			switch rr := r.(type) {
			case error:
				retErr = rr
			default:
				retErr = fmt.Errorf("%s", rr)
			}
		}
	}()

	var tmp any
	err := json.Unmarshal([]byte(input), &tmp)
	if err != nil {
		return "", false, err
	}

	rm, err := tag.Remove(tmp)
	if err != nil {
		return err.Error(), true, retErr
	}

	buf := new(bytes.Buffer)
	err = toml.NewEncoder(buf).Encode(rm)
	if err != nil {
		return err.Error(), true, retErr
	}

	return buf.String(), false, retErr
}

func (p parser) Decode(ctx context.Context, input string) (output string, outputIsError bool, retErr error) {
	defer func() {
		if r := recover(); r != nil {
			switch rr := r.(type) {
			case error:
				retErr = rr
			default:
				retErr = fmt.Errorf("%s", rr)
			}
		}
	}()

	var d any
	if _, err := toml.Decode(input, &d); err != nil {
		return err.Error(), true, retErr
	}

	j, err := json.MarshalIndent(tag.Add("", d), "", "  ")
	if err != nil {
		return "", false, err
	}
	return string(j), false, retErr
}
