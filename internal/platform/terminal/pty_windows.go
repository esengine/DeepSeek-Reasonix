//go:build windows

package terminal

import (
	"context"
	"errors"
	"strings"
	"sync"
	"syscall"

	"github.com/UserExistsError/conpty"
)

type winPty struct {
	c    *conpty.ConPty
	once sync.Once
}

func startPty(o Options) (pty, error) {
	parts := make([]string, 0, len(o.Args)+1)
	parts = append(parts, syscall.EscapeArg(o.Shell))
	for _, a := range o.Args {
		parts = append(parts, syscall.EscapeArg(a))
	}
	opts := []conpty.ConPtyOption{conpty.ConPtyDimensions(int(o.Cols), int(o.Rows))}
	if o.Dir != "" {
		opts = append(opts, conpty.ConPtyWorkDir(o.Dir))
	}
	if len(o.Env) > 0 {
		opts = append(opts, conpty.ConPtyEnv(o.Env))
	}
	c, err := conpty.Start(strings.Join(parts, " "), opts...)
	if err != nil {
		return nil, ptyStartError(err)
	}
	return &winPty{c: c}, nil
}

// ptyStartError separates a Windows that has no pseudoconsole (before 1809)
// from a shell that failed to start: the first can never succeed, so a person
// is told the terminal is unsupported rather than to try again.
func ptyStartError(err error) *Error {
	if errors.Is(err, conpty.ErrConPtyUnsupported) {
		return fail(CodeUnsupported, "%v", err)
	}
	return fail(CodeStartFailed, "%v", err)
}

func (p *winPty) Read(b []byte) (int, error)  { return p.c.Read(b) }
func (p *winPty) Write(b []byte) (int, error) { return p.c.Write(b) }
func (p *winPty) Close() error {
	var err error
	p.once.Do(func() { err = p.c.Close() })
	return err
}
func (p *winPty) Resize(cols, rows uint16) error {
	return p.c.Resize(int(cols), int(rows))
}
func (p *winPty) Wait() (int, error) {
	code, err := p.c.Wait(context.Background())
	return int(code), err
}

// Kill ends the shell by closing its console, which takes its children with it.
func (p *winPty) Kill() error { return p.Close() }
