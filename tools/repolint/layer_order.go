// layer_order.go — the kernel's layers, read from where a package lives.
//
// A package's layer is the directory it sits in under internal/, so where a
// package is kept is its declaration: moving it is the one way to change what it
// may depend on, and a reader sees the layer before opening a file. A package
// may import its own layer and any layer below; directories sharing a rank are
// independent peers, not a pool. What sits above internal/ — cmd, desktop,
// benchmarks, tools — are hosts and assemble everything.
package main

import (
	"fmt"
	"path"
	"strings"
)

const ruleLayerOrder = "layer-order"

// layerRank orders the directories under internal/. A package imports its own
// directory and strictly lower ranks; two directories sharing a rank are
// independent of each other, so neither can start a cycle between them.
var layerRank = map[string]int{
	"base":     0,
	"contract": 1,
	"safety":   2,
	"state":    2,
	"model":    3,
	"platform": 4,
	"ext":      5,
	"tools":    5,
	"runtime":  6,
	"session":  7,
	"assembly": 8,
	"frontend": 9,
}

// layerOf names the layer directory a package lives in, or "" for one outside
// internal/.
func layerOf(pkg string) (string, bool) {
	rest, ok := strings.CutPrefix(pkg, "internal/")
	if !ok {
		return "", false
	}
	layer, _, _ := strings.Cut(rest, "/")
	return layer, true
}

func checkLayerOrder(imports map[string][]importRef) []Finding {
	var out []Finding
	for _, rel := range sortedKeys(imports) {
		pkg := path.Dir(rel)
		layer, inside := layerOf(pkg)
		if !inside {
			continue
		}
		rank, known := layerRank[layer]
		if !known {
			out = append(out, Finding{rel, 1, ruleLayerOrder, fmt.Sprintf("%s is not in a layer directory; kernel packages live under internal/<layer>/", pkg), 1})
			continue
		}
		for _, ref := range imports[rel] {
			dep, ok := strings.CutPrefix(ref.path, modulePrefix)
			if !ok {
				continue
			}
			depLayer, inside := layerOf(dep)
			if !inside || depLayer == layer || layerRank[depLayer] < rank {
				continue
			}
			out = append(out, Finding{rel, ref.line, ruleLayerOrder,
				fmt.Sprintf("%s (%s) may not import %s (%s): a layer depends only on itself and the layers below it", pkg, layer, dep, depLayer), 1})
		}
	}
	return out
}
