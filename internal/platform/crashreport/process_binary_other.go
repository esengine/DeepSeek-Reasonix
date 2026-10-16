//go:build !linux

package crashreport

// sameBinary cannot be answered off Linux, so it answers "yes".
//
// The callers use it to decide whether a live pid is the dump's own writer, and
// the only platform where that question changes the answer is the one with a
// /proc to read. Windows reads the process's exit code directly and does not
// suffer the reuse this guards against, and the remaining hosts report liveness
// conservatively already. Answering "yes" keeps every file whose removal this
// cannot justify.
func sameBinary(pid int) bool { return pid > 0 }
