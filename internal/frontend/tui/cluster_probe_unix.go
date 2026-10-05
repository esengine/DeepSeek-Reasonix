//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package tui

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// clusterProbe is one rune plus an emoji-presentation selector: a terminal
// counting per rune advances one column for it, one drawing grapheme clusters
// two.
const clusterProbe = "\u26a0\ufe0f"

const clusterProbeTimeout = 80 * time.Millisecond

// clustersEmoji asks the terminal how far it advances for clusterProbe; ok is
// false when stdin and stdout are not the same answering terminal.
func clustersEmoji() (clusters, ok bool) {
	inFd, outFd := int(os.Stdin.Fd()), int(os.Stdout.Fd())
	if !term.IsTerminal(inFd) || !term.IsTerminal(outFd) {
		return false, false
	}
	old, err := term.MakeRaw(inFd)
	if err != nil {
		return false, false
	}
	defer term.Restore(inFd, old)
	flags, err := unix.FcntlInt(uintptr(inFd), unix.F_GETFL, 0)
	if err != nil {
		return false, false
	}
	if unix.SetNonblock(inFd, true) != nil {
		return false, false
	}
	defer func() { _, _ = unix.FcntlInt(uintptr(inFd), unix.F_SETFL, flags) }()

	if _, err := os.Stdout.WriteString("\r" + clusterProbe + "\x1b[6n"); err != nil {
		return false, false
	}
	defer func() { _, _ = os.Stdout.WriteString("\r\x1b[K") }()

	deadline := time.Now().Add(clusterProbeTimeout)
	buf := make([]byte, 64)
	var reply []byte
	for time.Now().Before(deadline) && len(reply) < 256 {
		n, err := unix.Read(inFd, buf)
		if n > 0 {
			reply = append(reply, buf[:n]...)
			if col, ok := cursorColumn(string(reply)); ok {
				return col-1 == 2, true
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
		return false, false
	}
	return false, false
}
