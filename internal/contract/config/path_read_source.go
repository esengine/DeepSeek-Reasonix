package config

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"

	fileencoding "reasonix/internal/base/fileutil/encoding"
)

type configReadPathKey struct {
	path string
	user bool
}

type configReadPathResult struct {
	path     string
	rootPath string
	name     string
	err      error
	root     *os.Root
	uncached bool
}

// configReadPaths owns resolution and directory authorities for one load.
// Unsupported directory access retains the guarded, uncached logical-path route.
type configReadPaths map[configReadPathKey]configReadPathResult

func (readPaths configReadPaths) close() {
	for _, result := range readPaths {
		if result.root != nil {
			_ = result.root.Close()
		}
	}
}

func (readPaths configReadPaths) result(path string, user bool) configReadPathResult {
	key := configReadPathKey{path: filepath.Clean(path), user: user}
	if result, ok := readPaths[key]; ok {
		return result
	}
	var result configReadPathResult
	if pinned, ok := pinnedConfigEditPath(path); ok {
		result.path, result.rootPath = pinned, filepath.Dir(pinned)
	} else {
		result.path, result.rootPath, result.err = resolveConfigAccessPathWithRoot(path, user)
	}
	if result.err == nil {
		result.name, result.err = filepath.Rel(result.rootPath, result.path)
	}
	readPaths[key] = result
	return result
}

func (readPaths configReadPaths) resolve(path string, user bool) (string, error) {
	if readPaths == nil {
		return resolveConfigAccessPath(path, user)
	}
	result := readPaths.result(path, user)
	if result.uncached {
		return resolveConfigAccessPath(path, user)
	}
	return result.path, result.err
}

func (readPaths configReadPaths) withoutRoot(path string, result configReadPathResult) configReadPathResult {
	if result.root != nil {
		_ = result.root.Close()
	}
	result.root, result.uncached = nil, true
	readPaths[configReadPathKey{path: filepath.Clean(path), user: isUserConfigPath(path)}] = result
	return result
}

func (readPaths configReadPaths) prepare(path string) (configReadPathResult, error) {
	result := readPaths.result(path, isUserConfigPath(path))
	if result.err != nil {
		return result, result.err
	}
	if result.root != nil || result.uncached {
		return result, nil
	}
	var err error
	result.root, err = os.OpenRoot(result.rootPath)
	if err != nil {
		if os.IsNotExist(err) {
			return result, err
		}
		// Acquiring the validated authority cannot refuse a relative-path escape.
		// A failed acquisition retains the original logical-path checks.
		return readPaths.withoutRoot(path, result), nil
	}
	readPaths[configReadPathKey{path: filepath.Clean(path), user: isUserConfigPath(path)}] = result
	return result, nil
}

func configReadRootUnavailable(err error) bool {
	return os.IsPermission(err) || errors.Is(err, errors.ErrUnsupported) || configReadRootPlatformUnavailable(err)
}

func (readPaths configReadPaths) stat(path string) (string, bool, error) {
	if readPaths == nil {
		return statConfigPath(path)
	}
	result, err := readPaths.prepare(path)
	if result.err != nil {
		return "", false, result.err
	}
	if result.uncached {
		return statConfigPath(path)
	}
	if err == nil {
		_, err = result.root.Stat(result.name)
		if configReadRootUnavailable(err) {
			readPaths.withoutRoot(path, result)
			return statConfigPath(path)
		}
	}
	if err != nil {
		if os.IsNotExist(err) {
			return result.path, false, nil
		}
		return "", false, configReadPathError("stat", result.path, err)
	}
	return result.path, true, nil
}

func (readPaths configReadPaths) readFile(path string) ([]byte, error) {
	if readPaths == nil {
		return readTOMLFileBytes(path)
	}
	result, err := readPaths.prepare(path)
	if result.err != nil {
		return nil, result.err
	}
	if result.uncached {
		return readTOMLFileBytes(path)
	}
	if err != nil {
		return nil, configReadPathError("open", result.path, err)
	}
	data, err := result.root.ReadFile(result.name)
	if configReadRootUnavailable(err) {
		readPaths.withoutRoot(path, result)
		return readTOMLFileBytes(path)
	}
	if err != nil {
		op := "open"
		var pathErr *os.PathError
		if errors.As(err, &pathErr) {
			op = pathErr.Op
		}
		return nil, configReadPathError(op, result.path, err)
	}
	return fileencoding.DecodeToUTF8(data), nil
}

func (readPaths configReadPaths) decodeTOMLFile(path string, value any) (toml.MetaData, error) {
	data, err := readPaths.readFile(path)
	if err != nil {
		return toml.MetaData{}, err
	}
	return decodeTOMLBytes(data, value)
}

func configReadPathError(op, path string, err error) error {
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		err = pathErr.Err
	}
	return &os.PathError{Op: op, Path: path, Err: err}
}
