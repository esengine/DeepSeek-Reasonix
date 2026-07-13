// Package command loads custom slash commands from Markdown files. A command is
// a prompt template: invoking /name substitutes the arguments into the body and
// sends the result as a chat turn. Loading is pure and dependency-free — a small
// "key: value" frontmatter parser keeps Reasonix's single-(TOML)-dependency promise
// rather than pulling in a YAML library.
package command

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	fileencoding "reasonix/internal/fileutil/encoding"
	"reasonix/internal/frontmatter"
)

// Command is a custom slash command loaded from a .md file.
type Command struct {
	Name        string // "review" or "git:commit", derived from the file path
	Description string // from frontmatter
	ArgHint     string // from frontmatter (argument-hint)
	Body        string // template with $ARGUMENTS / $1..$N / $$
	Source      string // originating file path, for diagnostics
	Plugin      string // installed plugin package name; empty for user/project commands
	ShortName   string // original plugin command name before the package qualifier
	Hidden      bool   // compatibility-only short alias; invocable but omitted from listings
}

// Root is one command directory and its optional plugin-package owner. Plugin
// ownership is carried through loading so plugin commands can use stable,
// package-qualified display names without losing unambiguous short-name
// compatibility.
type Root struct {
	Path   string
	Plugin string
}

// substRe matches the substitution tokens recognised in a command body.
var substRe = regexp.MustCompile(`\$(\$|ARGUMENTS|[0-9]+)`)

// Render substitutes args into the command body: $ARGUMENTS is all args joined
// by spaces, $1..$N are positional (empty when absent), and $$ is a literal $.
func (c Command) Render(args []string) string {
	return substRe.ReplaceAllStringFunc(c.Body, func(m string) string {
		switch tok := m[1:]; tok {
		case "$":
			return "$"
		case "ARGUMENTS":
			return strings.Join(args, " ")
		default:
			n, _ := strconv.Atoi(tok) // regex guarantees digits
			if n >= 1 && n <= len(args) {
				return args[n-1]
			}
			return ""
		}
	})
}

// Load reads every *.md command file under each dir, in order, so a later dir
// overrides an earlier one on a name clash (pass the user dir first, project
// dir last). Missing dirs are skipped. Individual file failures are collected
// into the returned error but don't prevent the others from loading. The result
// is sorted by name.
func Load(dirs ...string) ([]Command, error) {
	roots := make([]Root, 0, len(dirs))
	for _, dir := range dirs {
		roots = append(roots, Root{Path: dir})
	}
	return LoadRoots(roots...)
}

// LoadRoots is Load with optional plugin ownership. Every plugin command is
// exposed canonically as /<plugin>:<name>. A short /<name> compatibility alias
// is retained only when exactly one plugin contributes that name and no user or
// project command owns it; the alias is hidden from completion and model-visible
// listings. An explicit command occupying the qualified name still wins.
func LoadRoots(roots ...Root) ([]Command, error) {
	byName := map[string]Command{}
	pluginCommands := map[string]map[string]Command{}
	var errs []string
	for _, spec := range roots {
		root, err := filepath.Abs(spec.Path)
		if err != nil {
			continue
		}
		// A symlink-following walk (filepath.WalkDir does not follow links), so a
		// symlinked command directory or a symlinked <name>.md is picked up like a
		// real one. visited (keyed by resolved path) guards against symlink cycles.
		visited := map[string]bool{}
		if real, err := filepath.EvalSymlinks(root); err == nil {
			visited[real] = true
		} else {
			visited[root] = true
		}
		walkCommands(root, root, visited, func(path string) {
			c, perr := parseFile(root, path)
			if perr != nil {
				errs = append(errs, perr.Error())
				return
			}
			c.Plugin = strings.TrimSpace(spec.Plugin)
			if c.Plugin == "" {
				byName[c.Name] = c
			} else {
				if pluginCommands[c.Plugin] == nil {
					pluginCommands[c.Plugin] = map[string]Command{}
				}
				pluginCommands[c.Plugin][c.Name] = c
			}
		})
	}
	byShortName := map[string][]Command{}
	for plugin, commands := range pluginCommands {
		for shortName, pluginCommand := range commands {
			pluginCommand.ShortName = shortName
			byShortName[shortName] = append(byShortName[shortName], pluginCommand)
			qualified := plugin + ":" + shortName
			if _, occupied := byName[qualified]; occupied {
				continue
			}
			pluginCommand.Name = qualified
			byName[qualified] = pluginCommand
		}
	}
	for shortName, candidates := range byShortName {
		if _, occupied := byName[shortName]; occupied || len(candidates) != 1 {
			continue
		}
		compat := candidates[0]
		compat.Name = shortName
		compat.Hidden = true
		byName[shortName] = compat
	}
	cmds := make([]Command, 0, len(byName))
	for _, c := range byName {
		cmds = append(cmds, c)
	}
	sort.Slice(cmds, func(i, j int) bool { return cmds[i].Name < cmds[j].Name })
	if len(errs) > 0 {
		return cmds, fmt.Errorf("command load: %s", strings.Join(errs, "; "))
	}
	return cmds, nil
}

// walkCommands recursively visits dir, following symlinks, and calls fn with the
// path of every *.md file (including symlinked files and files under symlinked
// directories). visited (resolved-path set) prevents infinite recursion through
// a symlink cycle. Unreadable directories are skipped, never fatal.
func walkCommands(root, dir string, visited map[string]bool, fn func(path string)) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		full := filepath.Join(dir, e.Name())
		isDir := e.IsDir()
		isFile := e.Type().IsRegular()
		if e.Type()&os.ModeSymlink != 0 {
			info, serr := os.Stat(full) // follow the link
			if serr != nil {
				continue // broken link
			}
			isDir = info.IsDir()
			isFile = info.Mode().IsRegular()
		}
		switch {
		case isDir:
			real, rerr := filepath.EvalSymlinks(full)
			if rerr != nil {
				real = full
			}
			if visited[real] {
				continue
			}
			visited[real] = true
			walkCommands(root, full, visited, fn)
		case isFile && strings.EqualFold(filepath.Ext(e.Name()), ".md"):
			fn(full)
		}
	}
}

// parseFile reads one command file and derives its name from the path relative
// to root: drop the .md suffix and turn subdirectories into ":" namespaces
// (git/commit.md → git:commit).
func parseFile(root, path string) (Command, error) {
	b, err := fileencoding.ReadFileUTF8(path)
	if err != nil {
		return Command{}, err
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = filepath.Base(path)
	}
	name := strings.ReplaceAll(strings.TrimSuffix(filepath.ToSlash(rel), ".md"), "/", ":")

	// Normalise line endings and strip a leading UTF-8 BOM if present.
	content := strings.TrimPrefix(strings.ReplaceAll(string(b), "\r\n", "\n"), string(rune(0xFEFF)))
	fm, body := frontmatter.Split(content)
	return Command{
		Name:        name,
		Description: fm["description"],
		ArgHint:     fm["argument-hint"],
		Body:        strings.TrimSpace(body),
		Source:      path,
	}, nil
}

// splitFrontmatter is a thin wrapper kept for test compatibility; the real
// parser lives in internal/frontmatter.
func splitFrontmatter(s string) (map[string]string, string) {
	return frontmatter.Split(s)
}
