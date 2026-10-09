package proc

import (
	"os"
	"path/filepath"
	"strings"
)

// defaultPathExt is what Windows tries when PATHEXT is unset.
const defaultPathExt = ".COM;.EXE;.BAT;.CMD"

// CommandNames lists the file names a launcher tries for command, in order. On
// Windows the name is also tried with every PATHEXT extension, dotted or not,
// as os/exec does: `mcp` runs `mcp.cmd`, and `mcp.v2` runs `mcp.v2.cmd`.
func CommandNames(command, pathext string, windows bool) []string {
	if !windows {
		return []string{command}
	}
	if strings.TrimSpace(pathext) == "" {
		pathext = defaultPathExt
	}
	names := []string{command}
	seen := map[string]bool{strings.ToLower(command): true}
	for ext := range strings.SplitSeq(pathext, ";") {
		ext = strings.TrimSpace(ext)
		if ext == "" {
			continue
		}
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		name := command + ext
		if key := strings.ToLower(name); !seen[key] {
			seen[key] = true
			names = append(names, name)
		}
	}
	return names
}

// LookPathIn finds command in the absolute directories of pathList, trying
// CommandNames in each; relative entries are skipped.
func LookPathIn(command, pathList, pathext string, windows bool) (string, bool) {
	for _, dir := range filepath.SplitList(pathList) {
		if dir == "" || !filepath.IsAbs(dir) {
			continue
		}
		for _, name := range CommandNames(command, pathext, windows) {
			candidate := filepath.Join(dir, name)
			if IsExecutableFile(candidate, windows) {
				return candidate, true
			}
		}
	}
	return "", false
}

// IsExecutableFile reports a file a launcher would start: any regular file on
// Windows, one with an execute bit elsewhere.
func IsExecutableFile(path string, windows bool) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return windows || info.Mode().Perm()&0o111 != 0
}
