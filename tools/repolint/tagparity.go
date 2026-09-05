package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Where the two copies of the host's transient-block list live. The kernel's is
// the authority; the page keeps a hand-written second copy because it rebuilds
// a transcript from /history and must not print host markup as if a person had
// typed it.
const (
	goTagFile = "internal/agent/preview.go"
	tsTagFile = "desktop/frontend-next/src/state/session.ts"
)

var (
	goTagListRe = regexp.MustCompile(`(?s)var TransientUserBlockTags = \[\]string\{(.*?)\n\}`)
	goTagRe     = regexp.MustCompile(`"([a-z-]+)"`)
	tsTagListRe = regexp.MustCompile(`(?s)const CONTROL =\s*/<\((.*?)\)\[`)
)

// checkTransientTagParity holds the page's control-block list to the kernel's.
// This list has drifted twice: the history index named five of eleven, the page
// three of thirteen — and a tag the page does not know is markup it prints as
// something the user said. Compared as sets: the alternation and the slice have
// no reason to agree on order.
func checkTransientTagParity(root string) []Finding {
	kernel, err := tagsIn(root, goTagFile, goTagListRe, goTagRe)
	if err != nil {
		return []Finding{{goTagFile, 1, ruleWireParity, err.Error(), 1}}
	}
	page, err := tagsIn(root, tsTagFile, tsTagListRe, regexp.MustCompile(`([a-z-]+)`))
	if err != nil {
		return []Finding{{tsTagFile, 1, ruleWireParity, err.Error(), 1}}
	}

	var out []Finding
	for _, tag := range kernel {
		if !slices.Contains(page, tag) {
			out = append(out, Finding{tsTagFile, tagLine(root, tsTagFile, "const CONTROL"), ruleWireParity,
				fmt.Sprintf("<%s> is prepended to user turns but the page does not strip it: a replayed session prints it as something the user said", tag), 1})
		}
	}
	for _, tag := range page {
		if !slices.Contains(kernel, tag) {
			out = append(out, Finding{goTagFile, tagLine(root, goTagFile, "var TransientUserBlockTags"), ruleWireParity,
				fmt.Sprintf("the page strips <%s> and nothing prepends it: one of the two copies is stale", tag), 1})
		}
	}
	return out
}

// tagsIn pulls one declaration's tag names out of a file. block selects the
// declaration; name is applied to what it captured.
func tagsIn(root, rel string, block, name *regexp.Regexp) ([]string, error) {
	body, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		return nil, fmt.Errorf("transient-tag list unreadable: %w", err)
	}
	m := block.FindSubmatch(body)
	if m == nil {
		return nil, fmt.Errorf("the transient-tag list is no longer where %s declares it", rel)
	}
	var tags []string
	for _, hit := range name.FindAllStringSubmatch(string(m[1]), -1) {
		tags = append(tags, hit[1])
	}
	if len(tags) == 0 {
		return nil, fmt.Errorf("the transient-tag list in %s parsed as empty", rel)
	}
	return tags, nil
}

func tagLine(root, rel, needle string) int {
	body, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		return 1
	}
	for i, line := range strings.Split(string(body), "\n") {
		if strings.Contains(line, needle) {
			return i + 1
		}
	}
	return 1
}
