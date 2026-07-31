//go:generate ./gen.py

package tomltest

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
)

type testType uint8

const (
	TypeValid testType = iota
	TypeInvalid
)

//go:embed tests/*
var embeddedTests embed.FS

// EmbeddedTests are the tests embedded in toml-test, rooted to the "test/"
// directory.
func EmbeddedTests() fs.FS {
	f, err := fs.Sub(embeddedTests, "tests")
	if err != nil {
		panic(err)
	}
	return f
}

// Runner runs a set of tests.
//
// The validity of the parameters is not checked extensively; the caller should
// verify this if need be. See ./cmd/toml-test for an example.
type Runner struct {
	Files      fs.FS             // Test files.
	Encoder    bool              // Are we testing an encoder?
	RunTests   []string          // Tests to run; run all if blank.
	SkipTests  []string          // Tests to skip.
	Parser     Parser            // Send data to a parser.
	Version    string            // TOML version to run tests for.
	Parallel   int               // Number of tests to run in parallel
	Timeout    time.Duration     // Maximum time for parse.
	IntAsFloat bool              // Int values have type=float.
	Errors     map[string]string // Expected errors list.
}

// A Parser instance is used to call the TOML parser we test.
//
// By default this is done through an external command.
type Parser interface {
	// Encode a JSON string to TOML.
	//
	// The output is the TOML string; if outputIsError is true then it's assumed
	// that an encoding error occurred.
	//
	// An error return should only be used in case an unrecoverable error
	// occurred; failing to encode to TOML is not an error, but the encoder
	// unexpectedly panicking is.
	Encode(ctx context.Context, jsonInput string) (output string, outputIsError bool, err error)

	// Decode a TOML string to JSON. The same semantics as Encode apply.
	Decode(ctx context.Context, tomlInput string) (output string, outputIsError bool, err error)
}

// CommandParser calls an external command.
type CommandParser struct {
	fsys fs.FS
	cmd  []string
}

// Tests are tests to run.
type Tests struct {
	Tests []Test

	// Set when test are run.

	Skipped                      int
	PassedValid, FailedValid     int
	PassedInvalid, FailedInvalid int
}

// Result is the result of a single test.
type Test struct {
	Path string // Path of test, e.g. "valid/string-test"

	// Set when a test is run.

	Skipped          bool          // Skipped this test?
	Failure          string        // Failure message.
	Key              string        // TOML key the failure occured on; may be blank.
	Encoder          bool          // Encoder test?
	Input            string        // The test case that we sent to the external program.
	Output           string        // Output from the external program.
	Want             string        // The output we want.
	OutputFromStderr bool          // The Output came from stderr, not stdout.
	Timeout          time.Duration // Maximum time for parse.
	IntAsFloat       bool          // Int values have type=float.
}

type timeoutError struct{ d time.Duration }

func (err timeoutError) Error() string {
	return fmt.Sprintf("command timed out after %s; increase -timeout if this isn't an infinite loop or pathological behaviour", err.d)
}

// List all tests in Files for the current TOML version.
func (r Runner) List() ([]string, error) {
	if r.Version == "" {
		r.Version = "1.0.0"
	}
	if _, ok := versions[r.Version]; !ok {
		v := make([]string, 0, len(versions))
		for k := range versions {
			v = append(v, k)
		}
		sort.Strings(v)
		return nil, fmt.Errorf("tomltest.Runner.Run: unknown version: %q (supported: \"%s\")",
			r.Version, strings.Join(v, `", "`))
	}

	var (
		v       = versions[r.Version]
		exclude = make([]string, 0, 8)
	)
	for {
		exclude = append(exclude, v.exclude...)
		if v.inherit == "" {
			break
		}
		v = versions[v.inherit]
	}

	ls := make([]string, 0, 256)
	if err := r.findTOML("valid", &ls, exclude); err != nil {
		return nil, fmt.Errorf("reading 'valid/' dir: %w", err)
	}

	d := "invalid" + map[bool]string{true: "-encoder", false: ""}[r.Encoder]
	if err := r.findTOML(d, &ls, exclude); err != nil {
		return nil, fmt.Errorf("reading %q dir: %w", d, err)
	}

	return ls, nil
}

// Run all tests listed in t.RunTests.
//
// TODO: give option to:
// - Run all tests with \n replaced with \r\n
// - Run all tests with EOL removed
// - Run all tests with '# comment' appended to every line.
func (r Runner) Run() (Tests, error) {
	skipped, err := r.findTests()
	if err != nil {
		return Tests{}, fmt.Errorf("tomltest.Runner.Run: %w", err)
	}
	if r.Parallel == 0 {
		r.Parallel = 1
	}
	if r.Timeout == 0 {
		r.Timeout = 1 * time.Second
	}
	if r.Errors == nil {
		r.Errors = make(map[string]string)
	}
	nerr := make(map[string]string)
	for k, v := range r.Errors {
		if !strings.HasPrefix(k, "invalid/") {
			k = path.Join("invalid", k)
		}
		nerr[strings.TrimSuffix(k, ".toml")] = v
	}
	r.Errors = nerr

	var (
		tests = Tests{
			Tests:   make([]Test, 0, len(r.RunTests)),
			Skipped: skipped,
		}
		limit = make(chan struct{}, r.Parallel)
		wg    sync.WaitGroup
		mu    sync.Mutex
	)
	for _, p := range r.RunTests {
		invalid := strings.Contains(p, "invalid/")
		t := Test{
			Path:       p,
			Encoder:    r.Encoder,
			Timeout:    r.Timeout,
			IntAsFloat: r.IntAsFloat,
		}
		if r.hasSkip(p) {
			tests.Skipped++
			mu.Lock()
			t.Skipped = true
			tests.Tests = append(tests.Tests, t)
			mu.Unlock()
			continue
		}

		limit <- struct{}{}
		wg.Add(1)
		go func(p string) {
			defer func() { <-limit; wg.Done() }()

			t = t.Run(r.Parser, r.Files)

			mu.Lock()
			if e, ok := r.Errors[p]; invalid && ok && !t.Failed() && !strings.Contains(t.Output, e) {
				t.Failure = fmt.Sprintf("%q does not contain %q", t.Output, e)
			}
			delete(r.Errors, p)

			tests.Tests = append(tests.Tests, t)
			if t.Failed() {
				if invalid {
					tests.FailedInvalid++
				} else {
					tests.FailedValid++
				}
			} else {
				if invalid {
					tests.PassedInvalid++
				} else {
					tests.PassedValid++
				}
			}
			mu.Unlock()
		}(p)
	}
	wg.Wait()

	sort.Slice(tests.Tests, func(i, j int) bool {
		return strings.Replace(tests.Tests[i].Path, "invalid/", "zinvalid", 1) <
			strings.Replace(tests.Tests[j].Path, "invalid/", "zinvalid", 1)
	})

	if len(r.Errors) > 0 {
		keys := make([]string, 0, len(r.Errors))
		for k := range r.Errors {
			keys = append(keys, k)
		}
		return tests, fmt.Errorf("errors didn't match anything: %q", keys)
	}
	return tests, nil
}

// find all TOML files in 'path' relative to the test directory.
func (r Runner) findTOML(path string, appendTo *[]string, exclude []string) error {
	// It's okay if the directory doesn't exist.
	if _, err := fs.Stat(r.Files, path); errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	return fs.WalkDir(r.Files, path, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".toml") {
			return nil
		}

		path = strings.TrimSuffix(path, ".toml")
		for _, e := range exclude {
			if ok, _ := filepath.Match(e, path); ok {
				return nil
			}
		}

		*appendTo = append(*appendTo, path)
		return nil
	})
}

// Expand RunTest glob patterns, or return all tests if RunTests if empty.
func (r *Runner) findTests() (int, error) {
	ls, err := r.List()
	if err != nil {
		return 0, err
	}

	var skip int
	if len(r.RunTests) == 0 {
		r.RunTests = ls
	} else {
		run := make([]string, 0, len(r.RunTests))
		for _, l := range ls {
			for _, r := range r.RunTests {
				if m, _ := filepath.Match(r, l); m {
					run = append(run, l)
					break
				}
			}
		}
		r.RunTests, skip = run, len(ls)-len(run)
	}

	// Expand invalid tests ending in ".multi.toml"
	expanded := make([]string, 0, len(r.RunTests))
	for _, path := range r.RunTests {
		if !strings.HasSuffix(path, ".multi") {
			expanded = append(expanded, path)
			continue
		}

		d, err := fs.ReadFile(r.Files, path+".toml")
		if err != nil {
			return 0, err
		}

		fmt.Println(string(d))
	}
	r.RunTests = expanded

	return skip, nil
}

func (r Runner) hasSkip(path string) bool {
	for _, s := range r.SkipTests {
		if m, _ := filepath.Match(s, path); m {
			return true
		}
	}
	return false
}

func (c CommandParser) Encode(ctx context.Context, input string) (output string, outputIsError bool, err error) {
	stdout, stderr := new(bytes.Buffer), new(bytes.Buffer)
	cmd := exec.CommandContext(ctx, c.cmd[0])
	cmd.Args = c.cmd
	cmd.Stdin, cmd.Stdout, cmd.Stderr = strings.NewReader(input), stdout, stderr

	err = cmd.Run()
	if err != nil {
		eErr := &exec.ExitError{}
		if errors.As(err, &eErr) && eErr.ExitCode() == 1 {
			fmt.Fprintf(stderr, "\nExit %d\n", eErr.ProcessState.ExitCode())
			err = nil
		}
	}

	if stderr.Len() > 0 {
		return strings.TrimSpace(stderr.String()) + "\n", true, err
	}
	return strings.TrimSpace(stdout.String()) + "\n", false, err
}
func NewCommandParser(fsys fs.FS, cmd []string) CommandParser { return CommandParser{fsys, cmd} }
func (c CommandParser) Decode(ctx context.Context, input string) (string, bool, error) {
	return c.Encode(ctx, input)
}

// Run this test.
func (t Test) Run(p Parser, fsys fs.FS) Test {
	if t.Type() == TypeInvalid {
		return t.runInvalid(p, fsys)
	}
	return t.runValid(p, fsys)
}

func (t Test) runInvalid(p Parser, fsys fs.FS) Test {
	var err error
	_, t.Input, err = t.ReadInput(fsys)
	if err != nil {
		return t.bug(err.Error())
	}

	ctx, cancel := context.WithTimeout(context.Background(), t.Timeout)
	defer cancel()

	if t.Encoder {
		t.Output, t.OutputFromStderr, err = p.Encode(ctx, t.Input)
	} else {
		t.Output, t.OutputFromStderr, err = p.Decode(ctx, t.Input)
	}
	if ctx.Err() != nil {
		err = timeoutError{t.Timeout}
	}
	if err != nil {
		return t.fail(err.Error())
	}
	if !t.OutputFromStderr {
		return t.fail("Expected an error, but no error was reported.")
	}
	return t
}

func (t Test) runValid(p Parser, fsys fs.FS) Test {
	var err error
	_, t.Input, err = t.ReadInput(fsys)
	if err != nil {
		return t.bug(err.Error())
	}

	ctx, cancel := context.WithTimeout(context.Background(), t.Timeout)
	defer cancel()

	if t.Encoder {
		t.Output, t.OutputFromStderr, err = p.Encode(ctx, t.Input)
	} else {
		t.Output, t.OutputFromStderr, err = p.Decode(ctx, t.Input)
	}
	if ctx.Err() != nil {
		err = timeoutError{t.Timeout}
	}
	if err != nil {
		return t.fail(err.Error())
	}
	if t.OutputFromStderr {
		return t.fail(t.Output)
	}
	if t.Output == "" {
		// Special case: we expect an empty output here.
		if t.Path != "valid/empty-file" {
			return t.fail("stdout is empty")
		}
	}

	// Compare for encoder test
	if t.Encoder {
		want, err := t.ReadWantTOML(fsys)
		if err != nil {
			return t.bug(err.Error())
		}
		var have any
		if _, err := toml.Decode(t.Output, &have); err != nil {
			//return t.fail("decode TOML from encoder %q:\n  %s", cmd, err)
			return t.fail("decode TOML from encoder:\n  %s", err)
		}
		return t.CompareTOML(want, have)
	}

	// Compare for decoder test
	want, err := t.ReadWantJSON(fsys)
	if err != nil {
		return t.fail(err.Error())
	}

	var have any
	if err := json.Unmarshal([]byte(t.Output), &have); err != nil {
		return t.fail("decode JSON output from parser:\n  %s", err)
	}

	return t.CompareJSON(want, have)
}

// ReadInput reads the file sent to the encoder.
func (t Test) ReadInput(fsys fs.FS) (path, data string, err error) {
	path = t.Path + map[bool]string{true: ".json", false: ".toml"}[t.Encoder]
	d, err := fs.ReadFile(fsys, path)
	if err != nil {
		return path, "", err
	}
	return path, string(d), nil
}

func (t Test) ReadWant(fsys fs.FS) (path, data string, err error) {
	if t.Type() == TypeInvalid {
		panic("testoml.Test.ReadWant: invalid tests do not have a 'correct' version")
	}

	path = t.Path + map[bool]string{true: ".toml", false: ".json"}[t.Encoder]
	d, err := fs.ReadFile(fsys, path)
	if err != nil {
		return path, "", err
	}
	return path, string(d), nil
}

func (t *Test) ReadWantJSON(fsys fs.FS) (v any, err error) {
	var path string
	path, t.Want, err = t.ReadWant(fsys)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal([]byte(t.Want), &v); err != nil {
		return nil, fmt.Errorf("decode JSON file %q:\n  %s", path, err)
	}
	return v, nil
}
func (t *Test) ReadWantTOML(fsys fs.FS) (v any, err error) {
	var path string
	path, t.Want, err = t.ReadWant(fsys)
	if err != nil {
		return nil, err
	}
	_, err = toml.Decode(t.Want, &v)
	if err != nil {
		return nil, fmt.Errorf("could not decode TOML file %q:\n  %s", path, err)
	}
	return v, nil
}

// Test type: "valid", "invalid"
func (t Test) Type() testType {
	if strings.HasPrefix(t.Path, "invalid") {
		return TypeInvalid
	}
	return TypeValid
}

func (t Test) fail(format string, v ...any) Test {
	t.Failure = fmt.Sprintf(format, v...)
	return t
}
func (t Test) bug(format string, v ...any) Test {
	return t.fail("BUG IN TEST CASE: "+format, v...)
}

func (t Test) Failed() bool { return t.Failure != "" }
