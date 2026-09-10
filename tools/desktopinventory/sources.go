package main

import (
	"fmt"
	"regexp"
	"strings"
)

var artifactRe = regexp.MustCompile(`^#\s+(?:macOS|Windows|Linux)?:?\s*(Reasonix-[a-z]+-<arch>[^\s]*)\s+\((.*)\)`)

var ciJobs = map[string]struct {
	class class
	owner string
}{
	"ci.yml/desktop":                              {classKeepBusiness, "aggregate gate, unchanged"},
	"ci.yml/desktop-frontend":                     {classKeepBusiness, "React gates unchanged"},
	"ci.yml/desktop-browser":                      {classKeepBusiness, "Playwright browser gates unchanged"},
	"ci.yml/desktop-prepare":                      {classKeepBusiness, "go run . -emit-contract drift gate; pnpm workspace root"},
	"ci.yml/desktop-go":                           {classKeepBusiness, "hostrpc + module tests; no WebKitGTK toolchain"},
	"ci.yml/desktop-macos":                        {classKeepBusiness, "Electron packaging smoke"},
	"ci.yml/desktop-windows":                      {classKeepBusiness, "Electron packaging smoke"},
	"app-memory.yml/app-memory":                   {classKeepBusiness, "browser memory screening unchanged"},
	"app-memory.yml/prepare":                      {classKeepBusiness, "browser memory screening unchanged"},
	"app-memory.yml/shard":                        {classKeepBusiness, "browser memory screening unchanged"},
	"app-memory.yml/changes":                      {classKeepBusiness, "path filter unchanged"},
	"release-desktop.yml/resolve":                 {classKeepBusiness, "version/channel resolution unchanged"},
	"release-desktop.yml/orchestration-guard":     {classKeepBusiness, "unchanged"},
	"release-desktop.yml/release-gate":            {classKeepBusiness, "unchanged"},
	"release-desktop.yml/signing-contract":        {classKeepBusiness, "payload list covers the Electron executables and native modules"},
	"release-desktop.yml/cache-guard":             {classKeepBusiness, "unchanged"},
	"release-desktop.yml/build":                   {classKeepBusiness, "desktop-build.sh packages the Electron app with the same NSIS/nfpm/signing steps"},
	"release-desktop.yml/publish":                 {classKeepBusiness, "manifest, minisign and mirror unchanged"},
	"release-desktop.yml/attest-signing-contract": {classKeepBusiness, "attests the extended payload list"},
	"release-desktop.yml/mirror":                  {classKeepBusiness, "unchanged"},
}

var ciJobRe = regexp.MustCompile(`(?m)^  ([a-z][a-z0-9_-]*):$`)

var ciTriggerKeys = map[string]bool{"push": true, "pull_request": true, "workflow_dispatch": true, "workflow_call": true, "schedule": true}

func scanSources(root string, inv *inventory) error {
	build, err := readFile(root, "scripts/desktop-build.sh")
	if err != nil {
		return err
	}
	for i, line := range strings.Split(build, "\n") {
		m := artifactRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		inv.add(entry{
			Kind:     kindArtifact,
			Name:     m[1],
			Detail:   m[2],
			Location: fmt.Sprintf("scripts/desktop-build.sh:%d", i+1),
			Class:    classKeepBusiness,
			Owner:    "same file name and installer identity; Electron payload inside",
		})
	}
	for _, workflow := range []string{"ci.yml", "app-memory.yml", "release-desktop.yml"} {
		text, err := readFile(root, ".github/workflows/"+workflow)
		if err != nil {
			return err
		}
		for _, m := range ciJobRe.FindAllStringSubmatch(text, -1) {
			job := m[1]
			if ciTriggerKeys[job] || (workflow == "ci.yml" && !strings.HasPrefix(job, "desktop")) {
				continue
			}
			key := workflow + "/" + job
			e := entry{Kind: kindCIJob, Name: key, Location: ".github/workflows/" + workflow}
			if rule, ok := ciJobs[key]; ok {
				e.Class, e.Owner = rule.class, rule.owner
			}
			inv.add(e)
		}
	}
	return nil
}
