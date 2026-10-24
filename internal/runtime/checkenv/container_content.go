package checkenv

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path"
	"strings"
)

func (i *containerEnvironment) verifyUploadedSource(ctx context.Context) error {
	file, err := os.CreateTemp(i.directory, "received-*.tar")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	cmd := i.client.command(ctx, "cp", "--archive", i.journal.Run+":"+i.sourcePath, "-")
	cmd.Stdout, cmd.Stderr = &containerOutput{destination: file, remaining: (2 << 30) + (200_000 * 2048)}, io.Discard
	if err := cmd.Run(); err != nil {
		return err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	expected, err := scanTree(ctx, i.source, treePolicy{})
	if err != nil {
		return err
	}
	return compareContainerArchive(ctx, tar.NewReader(file), path.Base(i.sourcePath), expected)
}

func compareContainerArchive(ctx context.Context, archive *tar.Reader, prefix string, expected []treeEntry) error {
	entries := make(map[string]treeEntry, len(expected))
	for _, entry := range expected {
		entries[entry.Path] = entry
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		name := strings.TrimSuffix(header.Name, "/")
		if name == prefix {
			name = "."
		} else {
			var ok bool
			name, ok = strings.CutPrefix(name, prefix+"/")
			if !ok {
				return errors.New("container copy escaped source root")
			}
		}
		entry, ok := entries[name]
		if !ok || header.FileInfo().Mode() != entry.Mode || header.Uid != 65534 || header.Gid != 65534 {
			return errors.New("container source identity or mode differs from frozen source")
		}
		switch header.Typeflag {
		case tar.TypeReg:
			hash := sha256.New()
			if _, err := io.Copy(hash, archive); err != nil {
				return err
			}
			if header.Size != entry.Size || hex.EncodeToString(hash.Sum(nil)) != entry.Digest {
				return errors.New("container source content differs from frozen source")
			}
		case tar.TypeSymlink:
			if header.Linkname != entry.Link {
				return errors.New("container source link differs from frozen source")
			}
		case tar.TypeDir:
		default:
			return errors.New("container source contains unsupported entry")
		}
		delete(entries, name)
	}
	if len(entries) != 0 {
		return errors.New("container source copy is incomplete")
	}
	return nil
}
