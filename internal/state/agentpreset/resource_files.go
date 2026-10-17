package agentpreset

import (
	"fmt"
	"os"
	"path/filepath"

	"reasonix/internal/base/secrets"
	"reasonix/internal/contract/agentcomposition"
)

func CaptureFiles(paths []string) (DirectorySnapshot, map[string]string, error) {
	asset := DirectorySnapshot{Files: []AssetFile{}}
	mapping := map[string]string{}
	for i, path := range paths {
		info, err := os.Lstat(path)
		if err != nil {
			return asset, nil, err
		}
		if !info.Mode().IsRegular() || secrets.SensitiveFileName(info.Name()) {
			return asset, nil, fmt.Errorf("%w: unsupported hook resource", agentcomposition.ErrUnavailable)
		}
		if info.Size() > maxAssetBytes {
			return asset, nil, agentcomposition.ErrUnavailable
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return asset, nil, err
		}
		rel := fmt.Sprintf("files/%d/%s", i, filepath.Base(path))
		mapping[filepath.Clean(path)] = rel
		asset.Files = append(asset.Files, AssetFile{Path: filepath.ToSlash(rel), Data: data, Mode: uint32(info.Mode().Perm() & 0o700)})
		if err := validateAssets(asset); err != nil {
			return asset, nil, err
		}
	}
	return asset, mapping, nil
}
