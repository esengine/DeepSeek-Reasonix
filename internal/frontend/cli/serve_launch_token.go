package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/base/secrets"
	"reasonix/internal/contract/config"
)

// writeLaunchTokenFile leaves the token an auth-disabled serve's changes need
// in a 0600 file under the remote state directory, which runtime sandboxes
// deny. The terminal is no place for it: a sandboxed command can read back a
// multiplexer's scrollback.
func writeLaunchTokenFile(dir, token string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		return "", errors.New("cannot store the launch token: Reasonix home is empty")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("launch-%d.token", os.Getpid()))
	_ = os.Remove(path)
	if err := fileutil.AtomicCreateFile(path, []byte(token+"\n"), 0o600); err != nil {
		return "", err
	}
	secrets.RegisterHostSecretPath(path)
	return path, nil
}

// launchTokenLocation is where an auth-disabled serve tells the operator to
// find its launch token: the --token-file it was given, or a file it writes.
func launchTokenLocation(token string, opts serveFrontendOptions, resources *serveFrontendResources) (string, error) {
	if opts.tokenFile != "" {
		return opts.tokenFile, nil
	}
	path, err := writeLaunchTokenFile(config.RemoteStateDir(), token)
	if err != nil {
		return "", err
	}
	resources.artifacts = append(resources.artifacts, path)
	return path, nil
}

// withoutFragment drops a #token= fragment before a URL reaches the terminal.
func withoutFragment(raw string) string {
	before, _, _ := strings.Cut(raw, "#")
	return before
}
