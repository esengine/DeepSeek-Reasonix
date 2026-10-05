package builtin

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func ripgrepTarget(path string, directory bool) (string, error) {
	if !directory {
		return path, nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	// The argument and cwd must share the physical root for slash globs.
	// Keep the argument absolute: sandbox setup can change an unmapped cwd.
	return filepath.EvalSymlinks(abs)
}

func restoreRipgrepPath(line, target, requested string) string {
	sep := string(filepath.Separator)
	if relative, ok := strings.CutPrefix(line, strings.TrimSuffix(target, sep)+sep); ok {
		return strings.TrimSuffix(requested, sep) + sep + relative
	}
	return line
}

type grepDisplayError struct {
	cause   error
	message string
}

func (e *grepDisplayError) Error() string { return e.message }
func (e *grepDisplayError) Unwrap() error { return e.cause }

func ripgrepDisplayError(err error, target, requested string, rp ResolvedPath) error {
	message := err.Error()
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		displayPath := pathErr.Path
		if target == "" || displayPath == target {
			displayPath = requested
		} else {
			displayPath = restoreRipgrepPath(displayPath, target, requested)
		}
		message = (&os.PathError{Op: pathErr.Op, Path: rp.DisplayFor(displayPath), Err: pathErr.Err}).Error()
	} else {
		if target != "" {
			message = strings.ReplaceAll(message, target, requested)
		}
		message = rp.ErrorText(errors.New(message))
	}
	return &grepDisplayError{cause: err, message: "ripgrep: " + message}
}
