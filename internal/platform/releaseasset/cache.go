package releaseasset

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"path/filepath"
)

// cachedDownloadFromBase is DownloadCLI against an arbitrary base, so a test
// can serve a release without reaching the network.
func cachedDownloadFromBase(ctx context.Context, client *http.Client, base, dir string, line Line, version, goos, goarch string, official bool) ([]byte, error) {
	// Before dir is joined with either: an unreleasable version must not name a
	// directory, and the download would refuse it anyway.
	if err := supportedCLITarget(version, goos, goarch); err != nil {
		return nil, err
	}
	tag := line.tag(version)
	if dir == "" {
		return downloadCLIFromBase(ctx, client, base, tag, goos, goarch, official)
	}
	path := filepath.Join(dir, "cli", tag, goos+"-"+goarch)
	if binary, err := readCachedCLI(path); err == nil {
		return binary, nil
	}
	binary, err := downloadCLIFromBase(ctx, client, base, tag, goos, goarch, official)
	if err != nil {
		return nil, err
	}
	// Best effort: a machine that cannot write its cache can still install.
	_ = writeCachedCLI(path, binary)
	return binary, nil
}

// readCachedCLI returns the cached bytes only when they match the digest kept
// beside them. That catches a truncated write or a bad sector and nothing else:
// whatever can write the binary can write the digest, so this is an integrity
// check on this machine's disk, never a boundary against access to it.
func readCachedCLI(path string) ([]byte, error) {
	want, err := os.ReadFile(path + ".sha256")
	if err != nil {
		return nil, err
	}
	binary, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	got := sha256.Sum256(binary)
	if hex.EncodeToString(got[:]) != string(want) {
		return nil, errors.New("cached CLI does not match its digest")
	}
	return binary, nil
}

// writeCachedCLI puts both files in place by rename. The two renames are not
// atomic together and do not need to be: a reader that meets a mismatched pair
// fails the digest and downloads, which costs exactly what a miss costs.
func writeCachedCLI(path string, binary []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	sum := sha256.Sum256(binary)
	if err := writeFileAtomic(path+".sha256", []byte(hex.EncodeToString(sum[:])), 0o600); err != nil {
		return err
	}
	return writeFileAtomic(path, binary, 0o600)
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
