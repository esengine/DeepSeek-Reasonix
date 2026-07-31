// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package typedjson_test

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-quicktest/qt"

	"mvdan.cc/sh/v3/syntax"
	"mvdan.cc/sh/v3/syntax/typedjson"
)

var update = flag.Bool("u", false, "update output files")

func TestRoundtrip(t *testing.T) {
	t.Parallel()

	dir := filepath.Join("testdata", "roundtrip")
	shellPaths, err := filepath.Glob(filepath.Join(dir, "*.sh"))
	qt.Assert(t, qt.IsNil(err))
	for _, shellPath := range shellPaths {
		name := strings.TrimSuffix(filepath.Base(shellPath), ".sh")
		jsonPath := filepath.Join(dir, name+".json")
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			shellInput, err := os.ReadFile(shellPath)
			qt.Assert(t, qt.IsNil(err))
			jsonInput, err := os.ReadFile(jsonPath)
			if !*update { // allow it to not exist
				qt.Assert(t, qt.IsNil(err))
			}
			sb := new(strings.Builder)

			// Parse the shell source and check that it is well formatted.
			parser := syntax.NewParser(syntax.KeepComments(true))
			node, err := parser.Parse(bytes.NewReader(shellInput), "")
			qt.Assert(t, qt.IsNil(err))

			printer := syntax.NewPrinter()
			sb.Reset()
			err = printer.Print(sb, node)
			qt.Assert(t, qt.IsNil(err))
			qt.Assert(t, qt.Equals(sb.String(), string(shellInput)))

			// Validate writing the pretty JSON.
			sb.Reset()
			encOpts := typedjson.EncodeOptions{Indent: "\t"}
			err = encOpts.Encode(sb, node)
			qt.Assert(t, qt.IsNil(err))
			got := sb.String()
			if *update {
				err := os.WriteFile(jsonPath, []byte(got), 0o666)
				qt.Assert(t, qt.IsNil(err))
			} else {
				qt.Assert(t, qt.Equals(got, string(jsonInput)))
			}

			// Ensure we don't use the originally parsed node again.
			node = nil

			// Validate reading the pretty JSON and check that it formats the same.
			node2, err := typedjson.Decode(bytes.NewReader(jsonInput))
			qt.Assert(t, qt.IsNil(err))

			sb.Reset()
			err = printer.Print(sb, node2)
			qt.Assert(t, qt.IsNil(err))
			qt.Assert(t, qt.Equals(sb.String(), string(shellInput)))

			// Validate that emitting the JSON again produces the same result.
			sb.Reset()
			err = encOpts.Encode(sb, node2)
			qt.Assert(t, qt.IsNil(err))
			got = sb.String()
			qt.Assert(t, qt.Equals(got, string(jsonInput)))
		})
	}
}
