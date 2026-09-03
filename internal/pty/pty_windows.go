//go:build windows

package pty

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
	"reasonix/internal/proc"
)

var (
	kernel32Dll = windows.NewLazySystemDLL("kernel32.dll")

	procCreatePseudoConsole = kernel32Dll.NewProc("CreatePseudoConsole")
	procResizePseudoConsole = kernel32Dll.NewProc("ResizePseudoConsole")
	procClosePseudoConsole  = kernel32Dll.NewProc("ClosePseudoConsole")
)

type conPTY struct {
	hPC    windows.Handle
	inPipe *os.File
	outPipe *os.File
	mu     sync.Mutex
	closed bool
}

func (c *conPTY) Read(p []byte) (int, error) {
	return c.outPipe.Read(p)
}

func (c *conPTY) Write(p []byte) (int, error) {
	return c.inPipe.Write(p)
}

func (c *conPTY) Resize(rows, cols uint16) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || procResizePseudoConsole.Find() != nil {
		return nil
	}
	size := uint32(cols) | (uint32(rows) << 16)
	r1, _, err := procResizePseudoConsole.Call(uintptr(c.hPC), uintptr(size))
	if r1 != 0 {
		return fmt.Errorf("ResizePseudoConsole failed: %w", err)
	}
	return nil
}

func (c *conPTY) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	_ = c.inPipe.Close()
	_ = c.outPipe.Close()
	if procClosePseudoConsole.Find() == nil {
		_, _, _ = procClosePseudoConsole.Call(uintptr(c.hPC))
	}
	return nil
}

type windowsPipeLowLevelPTY struct {
	stdin  io.WriteCloser
	stdout io.ReadCloser
}

func (w *windowsPipeLowLevelPTY) Read(p []byte) (int, error) {
	return w.stdout.Read(p)
}

func (w *windowsPipeLowLevelPTY) Write(p []byte) (int, error) {
	return w.stdin.Write(p)
}

func (w *windowsPipeLowLevelPTY) Close() error {
	_ = w.stdin.Close()
	return w.stdout.Close()
}

func (w *windowsPipeLowLevelPTY) Resize(rows, cols uint16) error {
	// Standard Windows pipe fallback does not support window resize
	return nil
}

// spawnOSPTY starts a command attached to a Windows PseudoConsole (ConPTY),
// with graceful fallback to standard I/O pipes on legacy Windows.
func spawnOSPTY(cmd *exec.Cmd, cols, rows uint16) (LowLevelPTY, error) {
	if cols == 0 {
		cols = DefaultTerminalCols
	}
	if rows == 0 {
		rows = DefaultTerminalRows
	}

	// Try ConPTY first if kernel32 exports CreatePseudoConsole
	if procCreatePseudoConsole.Find() == nil {
		var inRead, inWrite windows.Handle
		var outRead, outWrite windows.Handle

		if err := windows.CreatePipe(&inRead, &inWrite, nil, 0); err == nil {
			if err := windows.CreatePipe(&outRead, &outWrite, nil, 0); err == nil {
				var hPC windows.Handle
				size := uint32(cols) | (uint32(rows) << 16)
				r1, _, _ := procCreatePseudoConsole.Call(
					uintptr(size),
					uintptr(inRead),
					uintptr(outWrite),
					0,
					uintptr(unsafe.Pointer(&hPC)),
				)
				if r1 == 0 {
					// ConPTY created successfully
					_ = windows.CloseHandle(inRead)
					_ = windows.CloseHandle(outWrite)

					cmd.Stdin = os.NewFile(uintptr(inRead), "conpty_in")
					cmd.Stdout = os.NewFile(uintptr(outWrite), "conpty_out")
					cmd.Stderr = cmd.Stdout

					if err := cmd.Start(); err == nil {
						return &conPTY{
							hPC:     hPC,
							inPipe:  os.NewFile(uintptr(inWrite), "conpty_in_write"),
							outPipe: os.NewFile(uintptr(outRead), "conpty_out_read"),
						}, nil
					}
					// If start failed, cleanup handles and fall back
					_, _, _ = procClosePseudoConsole.Call(uintptr(hPC))
				}
				_ = windows.CloseHandle(outRead)
				_ = windows.CloseHandle(outWrite)
			}
			_ = windows.CloseHandle(inRead)
			_ = windows.CloseHandle(inWrite)
		}
	}

	// Fallback to anonymous pipes
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("windows pty stdin pipe failed: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("windows pty stdout pipe failed: %w", err)
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("failed to start windows command: %w", err)
	}

	return &windowsPipeLowLevelPTY{
		stdin:  stdin,
		stdout: stdout,
	}, nil
}

// signalSIGINT sends interrupt / Ctrl+C on Windows.
func signalSIGINT(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Signal(os.Interrupt)
}

// signalSIGTERM terminates the process on Windows.
func signalSIGTERM(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	proc.KillTree(cmd)
}

// defaultShellPath returns PowerShell or cmd.exe on Windows.
func defaultShellPath() string {
	if comspec := os.Getenv("COMSPEC"); comspec != "" {
		return comspec
	}
	return "powershell.exe"
}
