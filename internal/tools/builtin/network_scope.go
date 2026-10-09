package builtin

import (
	"errors"
	"fmt"
	"runtime"
	"slices"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/contract/tool"
)

// CodeNetworkPathOutsideScope identifies a file-tool path refused because it
// names another machine. Windows connects to that machine to resolve the name,
// so the refusal is made on the spelling, before any lookup.
const CodeNetworkPathOutsideScope = fileutil.CodeNetworkPathOutsideScope

// windowsPaths is the host's path rule; tests replace it to hold the Windows
// reading of a path on any platform.
var windowsPaths = runtime.GOOS == "windows"

// refuseNetworkPath is the one decision every file tool asks before it resolves
// a model-supplied path. A network path passes only below a root that is itself
// a network path (see fileutil.NetworkScope); local paths always pass.
func refuseNetworkPath(target string, roots ...[]string) error {
	err := fileutil.NetworkScopeOn(windowsPaths, target, slices.Concat(roots...))
	if err == nil {
		return nil
	}
	return tool.Refusal{Code: CodeNetworkPathOutsideScope, Message: fmt.Sprintf(
		"refused: `%s` is a network path, a file on another machine, outside this workspace. "+
			"It was refused by its spelling and never looked up; file tools reach a network path only "+
			"below a workspace folder that is itself on the network", target)}
}

func isNetworkRefusal(err error) bool {
	var r tool.Refusal
	return errors.As(err, &r) && r.Code == CodeNetworkPathOutsideScope
}

// refuseNetwork applies refuseNetworkPath to a resolved read argument; a folder
// ref the host registered counts as a root of its own.
func (p ResolvedPath) refuseNetwork(roots []string) error {
	if p.External {
		return refuseNetworkPath(p.Path, roots, []string{p.Root})
	}
	return refuseNetworkPath(p.Path, roots)
}

// networkRoots lists the configured spellings of the folders a run works in,
// unresolved: resolving a root that names a share would itself be a lookup, and
// a root's resolved form may spell the machine differently from the model's path.
func networkRoots(dir string, groups ...[]string) []string {
	roots := []string{dir}
	for _, g := range groups {
		roots = append(roots, g...)
	}
	return roots
}

// networkSpellings keeps the roots spelled as network paths, as configured: a
// write root's resolved form may spell the machine differently, and a path built
// from the configured spelling is compared before anything is resolved.
func networkSpellings(roots []string) []string {
	var out []string
	for _, r := range roots {
		if windowsPaths && r != "" && fileutil.IsNetworkPath(r) {
			out = append(out, r)
		}
	}
	return out
}
