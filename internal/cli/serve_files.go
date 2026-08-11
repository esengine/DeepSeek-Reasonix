package cli

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"

	"reasonix/internal/fileutil"
)

// readServeTokenFile loads the auth=token pre-shared token from a file so the
// secret never appears in argv (visible via ps). The file must hold a single
// non-empty line and, on POSIX systems, must not be group/world accessible.
func readServeTokenFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !fi.Mode().IsRegular() {
		return "", fmt.Errorf("token file %s must be a regular file", path)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("token file %s must not be group/world accessible (chmod 600)", path)
	}
	b, err := io.ReadAll(io.LimitReader(f, (64<<10)+1))
	if err != nil {
		return "", err
	}
	if len(b) > 64<<10 {
		return "", fmt.Errorf("token file %s is too large", path)
	}
	tok := strings.TrimSpace(string(b))
	if tok == "" {
		return "", fmt.Errorf("token file %s is empty", path)
	}
	if strings.ContainsAny(tok, "\r\n") {
		return "", fmt.Errorf("token file %s must hold a single line", path)
	}
	return tok, nil
}

// writeServeAddrFile records the actual bound listen address (host:port) so a
// supervisor that started serve with --addr 127.0.0.1:0 can discover the real
// port. Written atomically with owner-only permissions.
func writeServeAddrFile(path, addr string) error {
	return fileutil.AtomicWriteFile(path, []byte(addr+"\n"), 0o600)
}

// writeServePidFile records the server's pid for supervisors that cannot
// capture the shell's $! (or want a belt-and-braces check).
func writeServePidFile(path string) error {
	return fileutil.AtomicWriteFile(path, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600)
}
