package agentpreset

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
)

// CaptureHookResources keeps sibling code and package data beside script entries.
func CaptureHookResources(paths []string) (DirectorySnapshot, map[string]string, error) {
	asset := DirectorySnapshot{Files: []AssetFile{}}
	mapping := map[string]string{}
	direct := []string{}
	parents := map[string]bool{}
	for _, path := range paths {
		switch strings.ToLower(filepath.Ext(path)) {
		case ".py", ".js", ".mjs", ".cjs", ".ts", ".sh", ".bash", ".ps1", ".rb", ".pl":
			parents[filepath.Dir(path)] = true
		default:
			direct = append(direct, path)
		}
	}
	for _, parent := range slices.Sorted(maps.Keys(parents)) {
		bundle, err := CaptureDirectory(parent)
		if err != nil {
			return asset, nil, err
		}
		prefix := fmt.Sprintf("bundles/%d", len(mapping))
		for _, file := range bundle.Files {
			original := filepath.Join(parent, filepath.FromSlash(file.Path))
			file.Path = filepath.ToSlash(filepath.Join(prefix, file.Path))
			mapping[original] = filepath.FromSlash(file.Path)
			asset.Files = append(asset.Files, file)
		}
		if err := validateAssets(asset); err != nil {
			return asset, nil, err
		}
	}
	files, entries, err := CaptureFiles(direct)
	if err != nil {
		return asset, nil, err
	}
	for original, target := range entries {
		if _, bundled := mapping[original]; !bundled {
			mapping[original] = target
		}
	}
	asset.Files = append(asset.Files, files.Files...)
	return asset, mapping, validateAssets(asset)
}
