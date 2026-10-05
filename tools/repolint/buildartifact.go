// buildartifact.go — a command's binary must not be something git can notice.
// `go build ./tools/repolint` writes the binary into the working directory, so
// every command in the root module can drop its own name at the repo root,
// where `git add -A` picks it up. Two of them were committed that way.
package main

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// rootCommand is one main package of the root module and the file that
// declares it, so the finding lands where the command is defined.
type rootCommand struct {
	name string
	file string
}

// checkBuildArtifacts holds every root-module command's output name to a root
// .gitignore entry. The names come from the package list, never from what is
// lying around: a command added today is covered before anyone builds it, and
// on a clean checkout there is nothing to observe.
func checkBuildArtifacts(root string) []Finding {
	ignored, err := rootIgnoredNames(root)
	if err != nil {
		return nil
	}
	var out []Finding
	for _, cmd := range rootModuleCommands(root) {
		if ignored[cmd.name] {
			continue
		}
		out = append(out, Finding{cmd.file, 1, ruleBuildArtifact,
			fmt.Sprintf("building this command writes %q at the repo root; add \"/%s\" to .gitignore so the artifact cannot be committed", cmd.name, cmd.name), 1})
	}
	return out
}

// rootIgnoredNames reads the entries that cover a file dropped at the root: a
// leading slash and nothing else. Without the slash the pattern matches
// anywhere, and a trailing one matches a directory — a binary is neither.
func rootIgnoredNames(root string) (map[string]bool, error) {
	data, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for line := range strings.SplitSeq(string(data), "\n") {
		name, ok := strings.CutPrefix(strings.TrimSpace(line), "/")
		if ok && name != "" && !strings.Contains(name, "/") {
			out[name] = true
		}
	}
	return out, nil
}

// rootModuleCommands walks for main packages, stopping at a nested go.mod: a
// command in another module builds into that module's directory and is not the
// root .gitignore's business.
func rootModuleCommands(root string) []rootCommand {
	var out []rootCommand
	seen := map[string]bool{}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return nil
		case d.IsDir():
			if path == root {
				return nil
			}
			if skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
				return filepath.SkipDir
			}
			return nil
		case !strings.HasSuffix(d.Name(), ".go") || seen[filepath.Dir(path)]:
			return nil
		}
		if !declaresMain(path) {
			return nil
		}
		dir := filepath.Dir(path)
		seen[dir] = true
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		out = append(out, rootCommand{name: filepath.Base(dir), file: filepath.ToSlash(rel)})
		return nil
	})
	return out
}

func declaresMain(path string) bool {
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.PackageClauseOnly)
	return err == nil && f.Name != nil && f.Name.Name == "main"
}
