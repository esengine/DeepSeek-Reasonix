package main

import (
	"fmt"
	"path"
	"strings"
)

const modulePrefix = "reasonix/"

// Transport-agnostic control.Controller sits behind every frontend; nothing
// below it may reach up. See REASONIX.md.
var frontends = []string{
	"internal/acp",
	"internal/boot",
	"internal/bot",
	"internal/botruntime",
	"internal/cli",
	"internal/serve",
}

// Utility layer: these packages carry no knowledge of the kernel and must
// stay importable from anywhere without dragging a dependency graph along.
var leaves = []string{
	"internal/ablation",
	"internal/agentpreset",
	"internal/billing",
	"internal/diff",
	"internal/extension/rpcwire",
	"internal/extensioncontract",
	"internal/filelock",
	"internal/fileref",
	"internal/fileutil",
	"internal/fileutil/encoding",
	"internal/frontmatter",
	"internal/i18n",
	"internal/mcpdiag",
	"internal/nilutil",
	"internal/planmode",
	"internal/proc",
	"internal/releaseasset",
	"internal/retrieval",
	"internal/shellparse",
	"internal/store",
	"internal/sysproxy",
	"internal/taskintent",
	"internal/textutil",
	"internal/workspacelease",
}

func checkLayering(imports map[string][]importRef) []Finding {
	var out []Finding
	for _, rel := range sortedKeys(imports) {
		pkg := path.Dir(rel)
		for _, ref := range imports[rel] {
			dep, ok := strings.CutPrefix(ref.path, modulePrefix)
			if !ok {
				continue
			}
			if msg := violates(pkg, dep); msg != "" {
				out = append(out, Finding{rel, ref.line, ruleLayering, msg, 1})
			}
		}
	}
	return out
}

func violates(pkg, dep string) string {
	switch {
	case matches(leaves, pkg):
		return fmt.Sprintf("%s is a utility-layer package and must not import %s", pkg, dep)
	case under(dep, "internal/control") && !matches(frontends, pkg) && !under(pkg, "internal/control") && !host(pkg):
		return fmt.Sprintf("%s may not import %s: the controller is reachable from frontends and entrypoints only", pkg, dep)
	case matches(frontends, dep) && !matches(frontends, pkg) && !host(pkg):
		return fmt.Sprintf("%s may not import the %s frontend: move shared behavior below the controller", pkg, dep)
	}
	return ""
}

func host(pkg string) bool { return under(pkg, "cmd") || under(pkg, "desktop") }

func under(pkg, root string) bool { return pkg == root || strings.HasPrefix(pkg, root+"/") }

func matches(roots []string, pkg string) bool {
	for _, root := range roots {
		if under(pkg, root) {
			return true
		}
	}
	return false
}
