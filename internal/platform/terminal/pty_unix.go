//go:build !windows

package terminal

import (
	"errors"
	"os"
	"os/exec"
	"sync"

	cpty "github.com/creack/pty"
	"golang.org/x/sys/unix"
)

type unixPty struct {
	f    *os.File
	cmd  *exec.Cmd
	once sync.Once
}

func startPty(o Options) (pty, error) {
	cmd := exec.Command(o.Shell, o.Args...)
	cmd.Dir = o.Dir
	if len(o.Env) > 0 {
		cmd.Env = o.Env
	}
	f, err := cpty.StartWithSize(cmd, &cpty.Winsize{Cols: o.Cols, Rows: o.Rows})
	if err != nil {
		return nil, fail(CodeStartFailed, "%v", err)
	}
	return &unixPty{f: f, cmd: cmd}, nil
}

func (p *unixPty) Read(b []byte) (int, error)  { return p.f.Read(b) }
func (p *unixPty) Write(b []byte) (int, error) { return p.f.Write(b) }
func (p *unixPty) Close() error {
	var err error
	p.once.Do(func() { err = p.f.Close() })
	return err
}
func (p *unixPty) Resize(cols, rows uint16) error {
	return cpty.Setsize(p.f, &cpty.Winsize{Cols: cols, Rows: rows})
}

func (p *unixPty) Wait() (int, error) {
	err := p.cmd.Wait()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), nil
	}
	if err != nil {
		return -1, err
	}
	return 0, nil
}

func (p *unixPty) Kill() error {
	if p.cmd.Process == nil {
		return nil
	}
	return p.cmd.Process.Kill()
}

// Busy is whether the terminal's foreground process group is not the shell's
// own: a command it started and is waiting on holds the terminal.
func (p *unixPty) Busy() bool {
	if p.cmd.Process == nil {
		return false
	}
	group, err := unix.IoctlGetInt(int(p.f.Fd()), unix.TIOCGPGRP)
	return err == nil && group != p.cmd.Process.Pid
}
