//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package cli

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

const (
	terminalBGQueryTimeout  = 80 * time.Millisecond
	terminalBGQueryMaxBytes = 256
)

func queryTerminalBackground() (terminalRGB, bool) {
	if !colorEnabled {
		return terminalRGB{}, false
	}
	inFd := int(os.Stdin.Fd())
	outFd := int(os.Stdout.Fd())
	if !term.IsTerminal(inFd) || !term.IsTerminal(outFd) {
		return terminalRGB{}, false
	}

	oldState, err := term.MakeRaw(inFd)
	if err != nil {
		return terminalRGB{}, false
	}
	defer term.Restore(inFd, oldState)

	flags, err := unix.FcntlInt(uintptr(inFd), unix.F_GETFL, 0)
	if err != nil {
		return terminalRGB{}, false
	}
	if err := unix.SetNonblock(inFd, true); err != nil {
		return terminalRGB{}, false
	}
	defer func() { _, _ = unix.FcntlInt(uintptr(inFd), unix.F_SETFL, flags) }()

	if _, err := os.Stdout.Write([]byte("\x1b]11;?\x07")); err != nil {
		return terminalRGB{}, false
	}

	deadline := time.Now().Add(terminalBGQueryTimeout)
	buf := make([]byte, 64)
	var response []byte
	for time.Now().Before(deadline) && len(response) < terminalBGQueryMaxBytes {
		n, err := unix.Read(inFd, buf)
		if n > 0 {
			response = append(response, buf[:n]...)
			if rgb, ok := parseOSC11Response(string(response)); ok {
				return rgb, true
			}
			continue
		}
		if err == nil || errors.Is(err, unix.EINTR) {
			continue
		}
		if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) {
			time.Sleep(5 * time.Millisecond)
			continue
		}
		return terminalRGB{}, false
	}
	return parseOSC11Response(string(response))
}
