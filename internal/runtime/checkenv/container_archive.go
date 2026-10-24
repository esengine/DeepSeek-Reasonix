package checkenv

import (
	"archive/tar"
	"context"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

func (i *containerEnvironment) sourceArchive(ctx context.Context) (string, error) {
	entries, err := scanTree(ctx, i.source, treePolicy{})
	if err != nil || digest(entries) != i.sourceDigest {
		return "", errors.Join(err, errors.New("frozen source changed"))
	}
	file, err := os.CreateTemp(i.directory, "source-*.tar")
	if err != nil {
		return "", err
	}
	writer := tar.NewWriter(file)
	root, err := os.OpenRoot(i.source)
	if err != nil {
		file.Close()
		return "", err
	}
	defer root.Close()
	for _, entry := range entries {
		if err = ctx.Err(); err != nil {
			break
		}
		name := path.Join(strings.TrimPrefix(i.sourcePath, "/"), entry.Path)
		header := &tar.Header{Name: name, Mode: int64(entry.Mode.Perm()), Uid: 65534, Gid: 65534, ModTime: time.Unix(0, entry.Modified), Typeflag: tar.TypeReg, Size: entry.Size}
		switch {
		case entry.Mode.IsDir():
			header.Typeflag, header.Size = tar.TypeDir, 0
		case entry.Mode&os.ModeSymlink != 0:
			if path.IsAbs(entry.Link) || strings.Contains(entry.Link, "\\") || !filepath.IsLocal(path.Join(path.Dir(entry.Path), entry.Link)) {
				err = errors.New("container verification cannot preserve an escaping source link")
			} else {
				header.Typeflag, header.Linkname, header.Size = tar.TypeSymlink, entry.Link, 0
			}
		}
		if err != nil {
			break
		}
		if err = writer.WriteHeader(header); err != nil {
			break
		}
		if header.Typeflag == tar.TypeReg {
			var input *os.File
			input, err = root.Open(entry.Path)
			if err != nil {
				break
			}
			_, copyErr := io.Copy(writer, input)
			err = errors.Join(copyErr, input.Close())
			if err != nil {
				break
			}
		}
	}
	err = errors.Join(err, writer.Close(), file.Close())
	return file.Name(), err
}
