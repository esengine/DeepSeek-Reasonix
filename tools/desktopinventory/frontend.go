package main

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	runtimeUseRe   = regexp.MustCompile(`\b(?:window\.runtime|runtime|rt)[!?]?\.(EventsOn|EventsOff|BrowserOpenURL|WindowSet[A-Za-z]+|WindowGet[A-Za-z]+|WindowIsMaximised|Clipboard[A-Za-z]+|OnFileDrop[A-Za-z]*)\b`)
	goBindingRe    = regexp.MustCompile(`window\.go\??\.main\??\.App`)
	eventsOnRe     = regexp.MustCompile(`(?:EventsOn|events\.on)\(\s*("([^"]+)"|` + "`([^`]+)`" + `)`)
	draggableRe    = regexp.MustCompile(`--reasonix-draggable`)
	dropTargetRe   = regexp.MustCompile(`data-native-drop-target`)
	frontendGlobRe = regexp.MustCompile(`\.(ts|tsx|css|html)$`)
)

var frontendNativeOwner = map[string]string{
	"EventsOn":                    "desktopHost().events.on",
	"BrowserOpenURL":              "native.openExternal → host shell.openExternal",
	"ClipboardSetText":            "native.clipboardWriteText",
	"ClipboardGetText":            "native.clipboardReadText",
	"WindowSetSystemDefaultTheme": "native.setWindowTheme(system)",
	"WindowSetLightTheme":         "native.setWindowTheme(light)",
	"WindowSetDarkTheme":          "native.setWindowTheme(dark)",
	"WindowSetBackgroundColour":   "native.setWindowBackground",
	"WindowGetSize":               "native.getWindowBounds",
	"WindowGetPosition":           "native.getWindowBounds",
	"WindowIsMaximised":           "native.getWindowBounds",
	"OnFileDrop":                  "native.onFilesDropped (HTML5 drop + getPathForFile)",
	"OnFileDropOff":               "native.onFilesDropped unsubscribe",
}

func scanFrontend(root string, inv *inventory) error {
	base := filepath.Join(root, "desktop", "frontend")
	var files []string
	err := filepath.WalkDir(filepath.Join(base, "src"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "__tests__" || d.Name() == "__fixtures__" || d.Name() == "generated" {
				return filepath.SkipDir
			}
			return nil
		}
		if frontendGlobRe.MatchString(d.Name()) {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	files = append(files, filepath.Join(base, "index.html"))
	sort.Strings(files)

	native := map[string]string{}
	events := map[string]string{}
	for _, path := range files {
		text, err := readFile(root, mustRel(root, path))
		if err != nil {
			return err
		}
		rel := mustRel(root, path)
		for i, line := range strings.Split(text, "\n") {
			loc := fmt.Sprintf("%s:%d", rel, i+1)
			for _, m := range runtimeUseRe.FindAllStringSubmatch(line, -1) {
				key := "window.runtime." + m[1]
				if _, seen := native[key]; !seen {
					native[key] = loc
				}
			}
			if goBindingRe.MatchString(line) {
				if _, seen := native["window.go.main.App"]; !seen {
					native["window.go.main.App"] = loc
				}
			}
			for _, m := range eventsOnRe.FindAllStringSubmatch(line, -1) {
				name := m[2]
				if name == "" {
					name = m[3]
				}
				if _, seen := events[name]; !seen {
					events[name] = loc
				}
			}
		}
		if draggableRe.MatchString(text) {
			inv.add(entry{Kind: kindCSSMarker, Name: "--reasonix-draggable", Location: rel, Class: classKeepBusiness, Owner: "rewritten to -webkit-app-region by scripts/shell-css.mjs for the Electron bundle"})
		}
		if dropTargetRe.MatchString(text) {
			inv.add(entry{Kind: kindCSSMarker, Name: "data-native-drop-target", Location: rel, Class: classKeepBusiness, Owner: "native.onFilesDropped (HTML5 drop + getPathForFile)"})
		}
	}
	for name, loc := range native {
		owner := "desktopHost() adapter (AppBindings proxy over desktop/invoke)"
		if method, ok := strings.CutPrefix(name, "window.runtime."); ok {
			owner = frontendNativeOwner[method]
			if owner == "" {
				owner = "desktopHost().native (unmapped)"
			}
		}
		inv.add(entry{Kind: kindFrontendNative, Name: name, Location: loc, Class: classMigrateHost, Owner: owner})
	}
	for name, loc := range events {
		inv.add(entry{Kind: kindFrontendEvent, Name: name, Location: loc, Class: classKeepBusiness, Owner: "desktopHost().events.on, same payload"})
	}
	return nil
}

func mustRel(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}
