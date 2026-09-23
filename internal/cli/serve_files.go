package cli

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"

	"reasonix/internal/fileutil"
	"reasonix/internal/i18n"
	"reasonix/internal/serve"
)

// readServeTokenFile loads the auth=token pre-shared token from a file so the
// secret never appears in argv (visible via ps). The file must hold a single
// non-empty line and, on POSIX systems, must not be group/world accessible.
func readServeTokenFile(path string) (string, error) {
	lines, err := readPrivateLines(path, 1)
	if err != nil {
		return "", err
	}
	return lines[0], nil
}

// readPrivateLines reads a file this serve's owner alone may read, holding
// exactly want non-empty lines. A secret kept in a file is only as private as
// the file, and a line count checked here is one no caller re-derives.
func readPrivateLines(path string, want int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("token file %s must be a regular file", path)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("token file %s must not be group/world accessible (chmod 600)", path)
	}
	b, err := io.ReadAll(io.LimitReader(f, (64<<10)+1))
	if err != nil {
		return nil, err
	}
	if len(b) > 64<<10 {
		return nil, fmt.Errorf("token file %s is too large", path)
	}
	lines := strings.Split(strings.TrimSpace(strings.ReplaceAll(string(b), "\r\n", "\n")), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil, fmt.Errorf("token file %s is empty", path)
	}
	if len(lines) != want {
		if want == 1 {
			return nil, fmt.Errorf("token file %s must hold a single line", path)
		}
		return nil, fmt.Errorf("token file %s must hold %d lines, has %d", path, want, len(lines))
	}
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
		if lines[i] == "" {
			return nil, fmt.Errorf("token file %s has an empty line", path)
		}
	}
	return lines, nil
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

// printPasswordHash answers --hash-password: print a bcrypt hash of password
// and stop, rather than starting a server. The second return says whether the
// caller is done; the first is its exit code.
func printPasswordHash(asked bool, password string) (int, bool) {
	if !asked {
		return 0, false
	}
	if password == "" {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, "--hash-password requires --password")
		return 1, true
	}
	h, err := serve.HashPassword(password)
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1, true
	}
	fmt.Println(h)
	return 0, true
}
