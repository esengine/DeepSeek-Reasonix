package cli

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"reasonix/internal/i18n"

	"github.com/spf13/pflag"
)

type commandFlagSet interface {
	Output() io.Writer
	Parse([]string) error
	SetOutput(io.Writer)
}

// commandFlagError keeps parse output attached to the returned error so pure
// syntax parsers can defer user-facing reporting to their command boundary.
type commandFlagError struct {
	err         error
	output      io.Writer
	parseOutput string
}

func (e *commandFlagError) Error() string { return e.err.Error() }
func (e *commandFlagError) Unwrap() error { return e.err }

// commandHelpRequested reports explicit help flags in the positional prefix a
// command validates before constructing its FlagSet. Once parsing starts, the
// FlagSet's ErrHelp path remains the source of truth.
func commandHelpRequested(args []string, positionalPrefix int) bool {
	if positionalPrefix > len(args) {
		positionalPrefix = len(args)
	}
	for _, arg := range args[:positionalPrefix] {
		if arg == "-h" || arg == "--help" {
			return true
		}
	}
	return false
}

func parseCommandFlagSet(fs commandFlagSet, args []string) error {
	output := fs.Output()
	var parseOutput bytes.Buffer
	fs.SetOutput(&parseOutput)
	err := fs.Parse(args)
	fs.SetOutput(output)
	if err == nil {
		return nil
	}
	return &commandFlagError{err: err, output: output, parseOutput: parseOutput.String()}
}

func reportCommandFlagError(err error) (exitCode int, handled bool) {
	var parseErr *commandFlagError
	if !errors.As(err, &parseErr) {
		return 0, false
	}
	if errors.Is(parseErr, flag.ErrHelp) || errors.Is(parseErr, pflag.ErrHelp) {
		_, _ = io.WriteString(os.Stdout, parseErr.parseOutput)
		return 0, true
	}
	fmt.Fprintln(parseErr.output, i18n.M.ErrorPrefix, parseErr.err)
	return 2, true
}

// parseCommandFlags gives standard flag and pflag commands the same public
// behavior: help is successful, while malformed input prints one concise error
// and returns the conventional command-line usage exit code.
func parseCommandFlags(fs commandFlagSet, args []string) (exitCode int, proceed bool) {
	err := parseCommandFlagSet(fs, args)
	if err == nil {
		return 0, true
	}
	if code, ok := reportCommandFlagError(err); ok {
		return code, false
	}
	fmt.Fprintln(fs.Output(), i18n.M.ErrorPrefix, err)
	return 2, false
}
