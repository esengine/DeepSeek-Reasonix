package pluginpkg

import (
	"io"
	"os"
	"path/filepath"

	fileencoding "reasonix/internal/base/fileutil/encoding"
)

const maxAgentSourceBytes = 1 << 20

func applyClaudeAgentDirs(path string, manifest *Manifest) {
	root, err := os.OpenRoot(path)
	if err != nil {
		return
	}
	defer root.Close()
	for _, rel := range claudeConventionAgentDirs {
		if containsPathEntry(manifest.Agents, rel) {
			continue
		}
		for _, ref := range loadAgentRefs(root, filepath.FromSlash(rel)) {
			if filepath.Dir(ref.Path) == filepath.Join(path, filepath.FromSlash(rel)) {
				manifest.Agents = append(manifest.Agents, rel)
				break
			}
		}
	}
}

func agentPathInfo(root *os.Root, path string) (os.FileInfo, *os.File) {
	file, err := root.OpenFile(path, os.O_RDONLY|agentReadFlags, 0)
	if err != nil {
		return nil, nil
	}
	info, err := file.Stat()
	if err != nil || (!info.IsDir() && !info.Mode().IsRegular()) {
		file.Close()
		return nil, nil
	}
	return info, file
}

func agentSourceBody(root *os.Root, path string) []byte {
	info, file := agentPathInfo(root, path)
	if file == nil {
		return nil
	}
	defer file.Close()
	if !info.Mode().IsRegular() || info.Size() > maxAgentSourceBytes {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(file, maxAgentSourceBytes+1))
	if err != nil || len(body) > maxAgentSourceBytes {
		return nil
	}
	return fileencoding.DecodeToUTF8(body)
}
