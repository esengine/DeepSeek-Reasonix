//go:build darwin

package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func darwinInstalledApplicationIndex() map[string]string {
	home, _ := os.UserHomeDir()
	roots := []string{
		"/Applications",
		"/System/Applications",
		"/System/Applications/Utilities",
		"/System/Library/CoreServices",
	}
	if home != "" {
		roots = append(roots, filepath.Join(home, "Applications"))
	}
	index := make(map[string]string)
	add := func(path string) {
		path = strings.TrimSpace(path)
		if info, err := os.Stat(path); err == nil && info.IsDir() && strings.HasSuffix(strings.ToLower(path), ".app") {
			name := strings.ToLower(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
			if _, exists := index[name]; !exists {
				index[name] = path
			}
		}
	}
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if strings.HasSuffix(strings.ToLower(entry.Name()), ".app") {
				add(filepath.Join(root, entry.Name()))
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/bin/mdfind", "kMDItemContentType == 'com.apple.application-bundle'").Output()
	if err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			add(line)
		}
	}
	return index
}

func darwinApplicationPath(index map[string]string, names ...string) string {
	for _, name := range names {
		if path := index[strings.ToLower(strings.TrimSpace(name))]; path != "" {
			return path
		}
	}
	return ""
}

func platformExternalOpenerSpecs() []externalOpenerSpec {
	installed := darwinInstalledApplicationIndex()
	var specs []externalOpenerSpec
	addMode := func(id, name, kind, mode, _ string, appNames ...string) {
		if path := darwinApplicationPath(installed, appNames...); path != "" {
			specs = append(specs, externalOpenerSpec{
				View:       ExternalOpenerView{ID: id, Name: name, Kind: kind},
				Target:     path,
				LaunchMode: mode,
				IconSource: path,
			})
		}
	}
	add := func(id, name, kind, bundleID string, appNames ...string) {
		addMode(id, name, kind, "application", bundleID, appNames...)
	}

	// Keep the order stable and close to the native Codex menu: common editors,
	// the platform browser, terminals, then the broader IDE catalog.
	add("vscode", "VS Code", externalOpenerEditor, "com.microsoft.VSCode", "Visual Studio Code")
	add("vscode-insiders", "VS Code Insiders", externalOpenerEditor, "com.microsoft.VSCodeInsiders", "Visual Studio Code - Insiders")
	add("cursor", "Cursor", externalOpenerEditor, "com.todesktop.230313mzl4w4u92", "Cursor")
	add("finder", "Finder", externalOpenerFileManager, "com.apple.finder", "Finder")
	add("terminal", "Terminal", externalOpenerTerminal, "com.apple.Terminal", "Terminal")
	add("iterm", "iTerm2", externalOpenerTerminal, "com.googlecode.iterm2", "iTerm", "iTerm2")
	addMode("ghostty", "Ghostty", externalOpenerTerminal, "ghostty", "com.mitchellh.ghostty", "Ghostty")
	add("xcode", "Xcode", externalOpenerEditor, "com.apple.dt.Xcode", "Xcode")
	add("android-studio", "Android Studio", externalOpenerEditor, "com.google.android.studio", "Android Studio")
	add("goland", "GoLand", externalOpenerEditor, "com.jetbrains.goland", "GoLand")
	add("pycharm", "PyCharm", externalOpenerEditor, "com.jetbrains.pycharm", "PyCharm", "PyCharm CE")
	add("intellij-idea", "IntelliJ IDEA", externalOpenerEditor, "com.jetbrains.intellij", "IntelliJ IDEA", "IntelliJ IDEA Ultimate", "IntelliJ IDEA CE")
	add("webstorm", "WebStorm", externalOpenerEditor, "com.jetbrains.WebStorm", "WebStorm")
	add("datagrip", "DataGrip", externalOpenerEditor, "com.jetbrains.datagrip", "DataGrip")
	add("codebuddy", "CodeBuddy", externalOpenerEditor, "com.tencent.codebuddy", "CodeBuddy")
	add("windsurf", "Windsurf", externalOpenerEditor, "com.exafunction.windsurf", "Windsurf")
	add("zed", "Zed", externalOpenerEditor, "dev.zed.Zed", "Zed")
	add("sublime-text", "Sublime Text", externalOpenerEditor, "com.sublimetext.4", "Sublime Text")
	add("kiro", "Kiro", externalOpenerEditor, "dev.kiro.desktop", "Kiro")
	return specs
}

func darwinBundleIconPath(appPath string) string {
	resources := filepath.Join(appPath, "Contents", "Resources")
	infoPlist := filepath.Join(appPath, "Contents", "Info.plist")
	for _, key := range []string{"CFBundleIconFile", "CFBundleIconName"} {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		out, err := exec.CommandContext(ctx, "/usr/libexec/PlistBuddy", "-c", "Print :"+key, infoPlist).Output()
		cancel()
		if err != nil {
			continue
		}
		name := strings.TrimSpace(string(out))
		if name == "" {
			continue
		}
		if filepath.Ext(name) == "" {
			name += ".icns"
		}
		path := filepath.Join(resources, name)
		if info, statErr := os.Stat(path); statErr == nil && !info.IsDir() {
			return path
		}
	}

	// Older and some Electron bundles omit the icon key. The application icon
	// is normally the largest ICNS resource; document-type icons are much smaller.
	matches, _ := filepath.Glob(filepath.Join(resources, "*.icns"))
	var best string
	var bestSize int64
	for _, candidate := range matches {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Size() > bestSize {
			best = candidate
			bestSize = info.Size()
		}
	}
	return best
}

func platformExternalOpenerIconDataURL(spec externalOpenerSpec) string {
	iconPath := darwinBundleIconPath(spec.IconSource)
	if iconPath == "" {
		return ""
	}
	tempDir, err := os.MkdirTemp("", "reasonix-opener-icon-*")
	if err != nil {
		return ""
	}
	defer os.RemoveAll(tempDir)
	outPath := filepath.Join(tempDir, "icon.png")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/sips", "-s", "format", "png", "-z", "64", "64", iconPath, "--out", outPath)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return ""
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		return ""
	}
	return externalOpenerPNGDataURL(data)
}

func darwinExternalOpenerCommand(spec externalOpenerSpec, path string) *exec.Cmd {
	if spec.LaunchMode == "ghostty" {
		return exec.Command("/usr/bin/open", "-na", spec.Target, "--args", "--working-directory="+path)
	}
	return exec.Command("/usr/bin/open", "-a", spec.Target, path)
}

func launchPlatformExternalOpener(spec externalOpenerSpec, path string) error {
	return startDetachedExternalOpener(darwinExternalOpenerCommand(spec, path))
}
