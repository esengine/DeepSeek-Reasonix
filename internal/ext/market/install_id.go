package market

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"reasonix/internal/base/fileutil"
)

var installIDShape = regexp.MustCompile(`^[0-9a-f]{32}$`)

// InstallID is the anonymous id an install report carries: random, kept only
// for the market under the Reasonix home, and unrelated to any other id this
// machine sends, so the registry's counts cannot be joined to usage statistics.
func InstallID(home string) (string, error) {
	if strings.TrimSpace(home) == "" {
		return "", errors.New("market: empty home")
	}
	dir := filepath.Join(home, "market")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "install-id")
	if b, err := os.ReadFile(path); err == nil {
		if id := strings.TrimSpace(string(b)); installIDShape.MatchString(id) {
			return id, nil
		}
		id, err := newInstallID()
		if err != nil {
			return "", err
		}
		return id, fileutil.AtomicWriteFile(path, []byte(id+"\n"), 0o600)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	id, err := newInstallID()
	if err != nil {
		return "", err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		// Another process created it first; its id is the one to keep.
		if b, readErr := os.ReadFile(path); readErr == nil && installIDShape.MatchString(strings.TrimSpace(string(b))) {
			return strings.TrimSpace(string(b)), nil
		}
		return "", err
	}
	if _, err := io.WriteString(f, id+"\n"); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", err
	}
	return id, f.Close()
}

func newInstallID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
