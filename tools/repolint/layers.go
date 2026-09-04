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
// One may reach another: the layer is closed, so that drags nothing in, and
// refusing it is what leaves the same judgement copied into several of them.
var leaves = []string{
	"internal/ablation",
	"internal/agentgraph",
	"internal/agentpreset",
	"internal/billing",
	"internal/diff",
	"internal/extension/rpcwire",
	"internal/execjournal",
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
	"internal/redirectguard",
	"internal/releaseasset",
	"internal/retrieval",
	"internal/shellparse",
	"internal/store",
	"internal/surface",
	"internal/sysproxy",
	"internal/textutil",
	"internal/tokencount",
	"internal/usagereport",
	"internal/visionimage",
	"internal/workspaceid",
	"internal/workspacelease",
}

// A host adapter implements a frontend's driven port and is shared by the hosts
// that need it. It sits beside the frontends rather than below them: it consumes
// their contracts by design, nothing in the kernel may reach it, and only a host
// assembles one. remotehost holds the machines a Studio opens a workspace on —
// the connection, not the window — so neither shell owns it.
var hostAdapters = []string{
	"internal/remotehost",
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
	case matches(leaves, pkg) && !matches(leaves, dep):
		return fmt.Sprintf("%s is a utility-layer package and must not import %s", pkg, dep)
	case under(dep, "internal/control") && !matches(frontends, pkg) && !under(pkg, "internal/control") && !host(pkg):
		return fmt.Sprintf("%s may not import %s: the controller is reachable from frontends and entrypoints only", pkg, dep)
	case matches(frontends, dep) && !above(pkg):
		return fmt.Sprintf("%s may not import the %s frontend: move shared behavior below the controller", pkg, dep)
	case matches(hostAdapters, dep) && !above(pkg):
		return fmt.Sprintf("%s may not import the %s host adapter: it is assembled by hosts, not reached from below", pkg, dep)
	}
	return ""
}

// A host assembles the whole system to run it. benchmarks/* are hosts in that
// sense — each is a main that builds a real agent — and a harness reproducing
// the shipped surface has to reach the frontend that defines it. This opens
// nothing to the kernel: the rule that matters is that nothing below a
// frontend may import one, and benchmarks are above every frontend.
func host(pkg string) bool {
	return under(pkg, "cmd") || under(pkg, "desktop") || under(pkg, "benchmarks")
}

// above reports whether a package sits at or over the frontend line, which is
// the only place a frontend or a host adapter may be imported from.
func above(pkg string) bool {
	return matches(frontends, pkg) || matches(hostAdapters, pkg) || host(pkg)
}

func under(pkg, root string) bool { return pkg == root || strings.HasPrefix(pkg, root+"/") }

func matches(roots []string, pkg string) bool {
	for _, root := range roots {
		if under(pkg, root) {
			return true
		}
	}
	return false
}
