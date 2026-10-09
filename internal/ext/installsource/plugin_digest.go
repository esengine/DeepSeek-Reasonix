package installsource

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/ext/pluginpkg"
)

func snapshotPlugin(pkg pluginpkg.Package) (pluginpkg.Package, string, func(), error) {
	root, err := os.MkdirTemp("", "reasonix-plugin-preview-*")
	if err != nil {
		return pkg, "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(root) }
	if err := copyDir(pkg.Root, root, tarballTotalLimit); err != nil {
		cleanup()
		return pkg, "", nil, err
	}
	if err := verifyCopiedCapabilities(pkg, root); err != nil {
		cleanup()
		return pkg, "", nil, err
	}
	snapshot, _, err := pluginpkg.ParseDir(root)
	if err != nil {
		cleanup()
		return pkg, "", nil, err
	}
	for i := range snapshot.Compatibility.Skipped {
		issue := &snapshot.Compatibility.Skipped[i]
		issue.Reason = strings.ReplaceAll(issue.Reason, root, ".")
	}
	digest, err := copiedPluginDigest(root)
	if err != nil {
		cleanup()
		return pkg, "", nil, err
	}
	return snapshot, digest, cleanup, nil
}

func copiedPluginDigest(root string) (string, error) {
	type fileDigest struct {
		Path string
		Hash string
	}
	var files []fileDigest
	var total int64
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return newErr(ErrDigestMismatch, "copied plugin contains a non-regular file")
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		h := sha256.New()
		n, readErr := io.Copy(h, io.LimitReader(f, tarballTotalLimit-total+1))
		closeErr := f.Close()
		if readErr != nil {
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}
		total += n
		if total > tarballTotalLimit {
			return newErr(ErrDigestMismatch, "copied plugin exceeds its content budget")
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, fileDigest{filepath.ToSlash(rel), hex.EncodeToString(h.Sum(nil))})
		return nil
	})
	if err != nil {
		return "", err
	}
	body, _ := json.Marshal(files)
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func verifyPluginDigest(root, expected string) error {
	got, err := copiedPluginDigest(root)
	if err != nil {
		return err
	}
	if got != expected {
		return newErr(ErrDigestMismatch, "copied plugin content differs from the approved snapshot")
	}
	return nil
}
