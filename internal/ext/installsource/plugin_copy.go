package installsource

import (
	"os"
	"path/filepath"
	"runtime"

	"reasonix/internal/ext/pluginpkg"
)

// siblingPublication is true where a directory held open by another process
// cannot be renamed, so a replacement is published beside the old tree.
var siblingPublication = runtime.GOOS == "windows"

func installPluginCopy(pkg pluginpkg.Package, sourceRoot, target string, replace bool, expected string) (string, func(), error) {
	if !replace || !siblingPublication {
		if err := installCopiedPlugin(pkg, sourceRoot, target, replace, expected); err != nil {
			return "", nil, err
		}
		if replace {
			return target, func() {}, nil
		}
		return target, func() { _ = os.RemoveAll(target) }, nil
	}
	// A sibling keeps the old tree intact through content validation and pointer
	// publication, including Windows watches that deny ancestor renames.
	root, err := stagePluginCopy(pkg, sourceRoot, target, expected)
	if err != nil {
		return "", nil, err
	}
	return root, func() { _ = os.RemoveAll(root) }, nil
}

func stagePluginCopy(pkg pluginpkg.Package, sourceRoot, target string, expected ...string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}
	staging, err := os.MkdirTemp(filepath.Dir(target), "."+filepath.Base(target)+".staging-")
	if err != nil {
		return "", err
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.RemoveAll(staging)
		}
	}()
	if err := copyDir(sourceRoot, staging, tarballTotalLimit); err != nil {
		return "", err
	}
	// Copying a symlink must not silently remove approved capabilities.
	if err := verifyCopiedCapabilities(pkg, staging); err != nil {
		return "", err
	}
	if len(expected) != 0 {
		if err := verifyPluginDigest(staging, expected[0]); err != nil {
			return "", err
		}
	}
	if err := os.Chmod(staging, 0o755); err != nil {
		return "", err
	}
	if err := syncPluginCopy(staging); err != nil {
		return "", err
	}
	complete = true
	return staging, nil
}

func syncPluginCopy(root string) error {
	var dirs []string
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			dirs = append(dirs, path)
			return nil
		}
		f, err := os.OpenFile(path, os.O_RDWR, 0)
		if err != nil {
			return err
		}
		syncErr := f.Sync()
		closeErr := f.Close()
		if syncErr != nil {
			return syncErr
		}
		return closeErr
	}); err != nil {
		return err
	}
	// Directory fsync is unsupported on Windows; the registry remains the sole
	// restart authority even when directory-entry durability is unavailable.
	for i := len(dirs) - 1; i >= 0; i-- {
		if f, err := os.Open(dirs[i]); err == nil {
			_ = f.Sync()
			_ = f.Close()
		}
	}
	return nil
}
