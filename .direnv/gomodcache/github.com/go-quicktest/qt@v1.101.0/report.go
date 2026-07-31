// Licensed under the MIT license, see LICENSE file for details.

package qt

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// reportParams holds parameters for reporting a test error.
type reportParams struct {
	// paramNames holds the names for the parameters of the checker
	// (including got as the first name).
	paramNames []string
	// args holds all the arguments passed to the checker.
	args []Arg
	// comments optionally holds comments passed when performing the check.
	comments []Comment
	// notes holds notes added while doing the check.
	notes []note
}

// Unquoted indicates that the string must not be pretty printed in the failure
// output. This is useful when a checker calls note and does not want the
// provided value to be quoted.
type Unquoted string

// SuppressedIfLong indicates that the value must be suppressed if verbose
// testing is off and the pretty printed version of the value is long. This is
// useful when a checker calls note and does not want the provided value to be
// printed in non-verbose test runs if the value is too long.
type SuppressedIfLong struct {
	// Value holds the original annotated value.
	Value any
}

// longValueLines holds the number of lines after which a value is long.
const longValueLines = 10

// report generates a failure report for the given error, optionally including
// in the output the checker arguments, comment and notes included in the
// provided report parameters.
func report(err error, p reportParams) string {
	var buf bytes.Buffer
	buf.WriteByte('\n')
	writeError(&buf, err, p)
	writeStack(&buf)
	return buf.String()
}

// writeError writes a pretty formatted output of the given error using the
// provided report parameters.
func writeError(w io.Writer, err error, p reportParams) {
	ptrs := make(map[string]any)
	values := make(map[string]string)

	printPair := func(key string, value any) {
		fmt.Fprintln(w, key+":")
		var v string

		if u, ok := value.(Unquoted); ok {
			// Output the raw string without quotes.
			v = string(u)
		} else if s, ok := value.(SuppressedIfLong); ok {
			// Check whether the output is too long and must be suppressed.
			v = Format(s.Value)
			if !testingVerbose() {
				if n := strings.Count(v, "\n"); n > longValueLines {
					fmt.Fprint(w, prefixf(prefix, "<suppressed due to length (%d lines), use -v for full output>", n))
					return
				}
			}
		} else {
			// Check whether the output has been already seen.
			v = Format(value)
			isPtr := reflect.ValueOf(value).Kind() == reflect.Pointer
			if k := values[v]; k != "" {
				if previousValue, ok := ptrs[k]; ok && isPtr && previousValue != value {
					fmt.Fprint(w, prefixf(prefix, "<same as %q but different pointer value>", k))
					return
				}
				fmt.Fprint(w, prefixf(prefix, "<same as %q>", k))
				return
			}
			if isPtr {
				ptrs[key] = value
			}
		}

		values[v] = key
		fmt.Fprint(w, prefixf(prefix, "%s", v))
	}

	// Write the checker error.
	if err != ErrSilent {
		printPair("error", Unquoted(err.Error()))
	}

	// Write comments if provided.
	for _, c := range p.comments {
		if comment := c.String(); comment != "" {
			printPair("comment", Unquoted(comment))
		}
	}

	// Write notes if present.
	for _, n := range p.notes {
		printPair(n.key, n.value)
	}
	if IsBadCheck(err) || err == ErrSilent {
		// For errors in the checker invocation or for silent errors, do not
		// show output from args.
		return
	}
	// Write provided args.
	for _, arg := range p.args {
		printPair(arg.Name, arg.Value)
	}
}

// testingVerbose is defined as a variable for testing.
var testingVerbose = func() bool {
	return testing.Verbose()
}

// writeStack writes the traceback information for the current failure into the
// provided writer.
func writeStack(w io.Writer) {
	fmt.Fprintln(w, "stack:")
	pc := make([]uintptr, 8)
	sg := &stmtGetter{
		fset:  token.NewFileSet(),
		files: make(map[string]*ast.File, 8),
		config: &printer.Config{
			Mode:     printer.UseSpaces,
			Tabwidth: 4,
		},
	}
	runtime.Callers(5, pc)
	frames := runtime.CallersFrames(pc)
	thisPackage := reflect.TypeOf(Unquoted("")).PkgPath() + "."
	for {
		frame, more := frames.Next()
		if strings.HasPrefix(frame.Function, "testing.") {
			// Stop before getting back to stdlib test runner calls.
			break
		}
		if fname := strings.TrimPrefix(frame.Function, thisPackage); fname != frame.Function {
			if ast.IsExported(fname) {
				// Continue without printing frames for quicktest exported API.
				continue
			}
			// Stop when entering quicktest internal calls.
			// This is useful for instance when using qtsuite.
			break
		}
		fmt.Fprint(w, prefixf(prefix, "%s:%d", frame.File, frame.Line))
		if strings.HasSuffix(frame.File, ".go") {
			stmt, err := sg.Get(frame.File, frame.Line)
			if err != nil {
				fmt.Fprint(w, prefixf(prefix+prefix, "<%s>", err))
			} else {
				fmt.Fprint(w, prefixf(prefix+prefix, "%s", stmt))
			}
		}
		if !more {
			// There are no more callers.
			break
		}
	}
}

type stmtGetter struct {
	fset   *token.FileSet
	files  map[string]*ast.File
	config *printer.Config
}

// Get returns the lines of code of the statement at the given file and line.
func (sg *stmtGetter) Get(file string, line int) (string, error) {
	f := sg.files[file]
	if f == nil {
		var err error
		f, err = parser.ParseFile(sg.fset, file, nil, parser.ParseComments)
		if err != nil {
			return "", fmt.Errorf("cannot parse source file: %s", err)
		}
		sg.files[file] = f
	}
	var stmt string
	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil || stmt != "" {
			return false
		}
		pos := sg.fset.Position(n.Pos()).Line
		end := sg.fset.Position(n.End()).Line
		// Go < v1.9 reports the line where the statements ends, not the line
		// where it begins.
		if line == pos || line == end {
			var buf bytes.Buffer
			// TODO: include possible comment after the statement.
			sg.config.Fprint(&buf, sg.fset, &printer.CommentedNode{
				Node:     n,
				Comments: f.Comments,
			})
			stmt = buf.String()
			return false
		}
		return pos < line && line <= end
	})
	return stmt, nil
}

// prefixf formats the given string with the given args. It also inserts the
// final newline if needed and indentation with the given prefix.
func prefixf(prefix, format string, args ...any) string {
	var buf []byte
	s := strings.TrimSuffix(fmt.Sprintf(format, args...), "\n")
	for _, line := range strings.Split(s, "\n") {
		buf = append(buf, prefix...)
		buf = append(buf, line...)
		buf = append(buf, '\n')
	}
	return string(buf)
}

// note holds a key/value annotation.
type note struct {
	key   string
	value any
}

// prefix is the string used to indent blocks of output.
const prefix = "  "
