package schedule

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/BurntSushi/toml"
)

// maxConfigBytes bounds the configuration file read for the [schedule] table.
const maxConfigBytes = 1 << 20

// LoadOverrides reads the [schedule] table of one configuration file. A missing
// file or table is no overrides. The caller passes the user-level file: this
// package cannot tell a project file from a user one, and a value from a
// project layer must go through Resolve's project argument instead.
func LoadOverrides(path string) (Overrides, error) {
	if path == "" {
		return Overrides{}, nil
	}
	data, err := readLimited(path)
	if errors.Is(err, os.ErrNotExist) {
		return Overrides{}, nil
	}
	if err != nil {
		return Overrides{}, fmt.Errorf("%w: read %s: %w", ErrPolicyInvalid, path, err)
	}
	var doc struct {
		Schedule Overrides `toml:"schedule"`
	}
	if _, err := toml.Decode(string(data), &doc); err != nil {
		return Overrides{}, fmt.Errorf("%w: parse %s: %w", ErrPolicyInvalid, path, err)
	}
	return doc.Schedule, nil
}

func readLimited(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxConfigBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxConfigBytes {
		return nil, errors.New("configuration file is too large")
	}
	return data, nil
}
