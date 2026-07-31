package interp

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// tracer prints expressions like a shell would do if its
// options '-o' is set to either 'xtrace' or its shorthand, '-x'.
type tracer struct {
	buf       bytes.Buffer
	printer   *syntax.Printer
	output    io.Writer
	needsPlus bool
}

func (r *Runner) tracer() *tracer {
	if !r.opts[optXTrace] {
		return nil
	}

	return &tracer{
		printer:   syntax.NewPrinter(),
		output:    r.stderr,
		needsPlus: true,
	}
}

// string writes s to tracer.buf if tracer is non-nil,
// prepending "+" if tracer.needsPlus is true.
func (t *tracer) string(s string) {
	if t == nil {
		return
	}

	if t.needsPlus {
		t.buf.WriteString("+ ")
	}
	t.needsPlus = false
	t.buf.WriteString(s)
}

func (t *tracer) stringf(f string, a ...any) {
	if t == nil {
		return
	}

	t.string(fmt.Sprintf(f, a...))
}

// expr prints x to tracer.buf if tracer is non-nil,
// prepending "+" if tracer.isFirstPrint is true.
func (t *tracer) expr(x syntax.Node) {
	if t == nil {
		return
	}

	if t.needsPlus {
		t.buf.WriteString("+ ")
	}
	t.needsPlus = false
	if err := t.printer.Print(&t.buf, x); err != nil {
		panic(err)
	}
}

// flush writes the contents of tracer.buf to the tracer.stdout.
func (t *tracer) flush() {
	if t == nil {
		return
	}

	t.output.Write(t.buf.Bytes())
	t.buf.Reset()
}

// newLineFlush is like flush, but with extra new line before tracer.buf gets flushed.
func (t *tracer) newLineFlush() {
	if t == nil {
		return
	}

	t.buf.WriteString("\n")
	t.flush()
	// reset state
	t.needsPlus = true
}

// call prints a command and its arguments with varying formats depending on the cmd type,
// for example, built-in command's arguments are printed enclosed in single quotes,
// otherwise, call defaults to printing with double quotes.
func (t *tracer) call(cmd string, args ...string) {
	if t == nil {
		return
	}

	s := strings.Join(args, " ")
	if strings.TrimSpace(s) == "" {
		// fields may be empty for function () {} declarations
		t.string(cmd)
	} else if IsBuiltin(cmd) {
		if cmd == "set" {
			// TODO: only first occurrence of set is not printed, succeeding calls are printed
			return
		}

		qs, err := syntax.Quote(s, syntax.LangBash)
		if err != nil { // should never happen
			panic(err)
		}
		t.stringf("%s %s", cmd, qs)
	} else {
		t.stringf("%s %s", cmd, s)
	}
}
