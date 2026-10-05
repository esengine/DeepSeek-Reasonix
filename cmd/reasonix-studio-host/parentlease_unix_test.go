//go:build !windows

package main

import (
	"os"
	"syscall"
	"testing"
)

// The lease the shell actually hands over. The pipe case above is a FIFO, and
// no launch is given one: libuv hands a child's stdio a socketpair. The guard
// passed on a stream production does not use.
func TestASocketpairIsAParentLease(t *testing.T) {
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Skipf("socketpair unavailable: %v", err)
	}
	mine := os.NewFile(uintptr(fds[0]), "lease")
	theirs := os.NewFile(uintptr(fds[1]), "lease-peer")
	defer mine.Close()
	defer theirs.Close()

	if parentLease(mine) == nil {
		t.Fatal("the stream Node hands this host was not read as a parent holding it open")
	}

	// And it ends when the parent goes, which is the whole point of reading it.
	if err := theirs.Close(); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1)
	if _, err := mine.Read(buf); err == nil {
		t.Error("reading the lease after the parent let go did not end")
	}
}
