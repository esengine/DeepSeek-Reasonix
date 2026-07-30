//go:build !darwin && !windows

package main

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type linuxDesktopEntry struct {
	Path string
	ID   string
	Name string
	Icon string
}

func linuxExecutable(names ...string) string {
	for _, name := range names {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return ""
}

func linuxDesktopEntries() []linuxDesktopEntry {
	home, _ := os.UserHomeDir()
	roots := []string{"/usr/share/applications", "/usr/local/share/applications"}
	if home != "" {
		roots = append([]string{filepath.Join(home, ".local", "share", "applications")}, roots...)
	}
	var entries []linuxDesktopEntry
	for _, root := range roots {
		files, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, file := range files {
			if file.IsDir() || !strings.HasSuffix(strings.ToLower(file.Name()), ".desktop") {
				continue
			}
			path := filepath.Join(root, file.Name())
			if entry, ok := parseLinuxDesktopEntry(path); ok {
				entries = append(entries, entry)
			}
		}
	}
	return entries
}

func parseLinuxDesktopEntry(path string) (linuxDesktopEntry, bool) {
	file, err := os.Open(path)
	if err != nil {
		return linuxDesktopEntry{}, false
	}
	defer file.Close()
	entry := linuxDesktopEntry{Path: path, ID: strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))}
	inDesktopEntry := false
	hidden := false
	entryType := ""
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inDesktopEntry = line == "[Desktop Entry]"
			continue
		}
		if !inDesktopEntry || line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		switch strings.TrimSpace(key) {
		case "Name":
			entry.Name = strings.TrimSpace(value)
		case "Icon":
			entry.Icon = strings.TrimSpace(value)
		case "Type":
			entryType = strings.TrimSpace(value)
		case "Hidden":
			hidden = strings.EqualFold(strings.TrimSpace(value), "true")
		}
	}
	if scanner.Err() != nil || hidden || (entryType != "" && entryType != "Application") || entry.Name == "" {
		return linuxDesktopEntry{}, false
	}
	return entry, true
}

func normalizeLinuxAppName(value string) string {
	value = strings.ToLower(value)
	return strings.NewReplacer(" ", "", "-", "", "_", "", ".", "").Replace(value)
}

func findLinuxDesktopEntry(entries []linuxDesktopEntry, aliases ...string) (linuxDesktopEntry, bool) {
	for _, alias := range aliases {
		want := normalizeLinuxAppName(alias)
		for _, entry := range entries {
			if normalizeLinuxAppName(entry.ID) == want || normalizeLinuxAppName(entry.Name) == want {
				return entry, true
			}
		}
	}
	for _, alias := range aliases {
		want := normalizeLinuxAppName(alias)
		if len(want) < 5 {
			continue
		}
		for _, entry := range entries {
			if strings.Contains(normalizeLinuxAppName(entry.ID), want) || strings.Contains(normalizeLinuxAppName(entry.Name), want) {
				return entry, true
			}
		}
	}
	return linuxDesktopEntry{}, false
}

func linuxDefaultDirectoryDesktopEntry(entries []linuxDesktopEntry) (linuxDesktopEntry, bool) {
	xdgMime := linuxExecutable("xdg-mime")
	if xdgMime != "" {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		out, err := exec.CommandContext(ctx, xdgMime, "query", "default", "inode/directory").Output()
		cancel()
		if err == nil {
			id := strings.TrimSpace(string(out))
			if entry, ok := findLinuxDesktopEntry(entries, id, strings.TrimSuffix(id, ".desktop")); ok {
				return entry, true
			}
		}
	}
	return findLinuxDesktopEntry(entries, "org.gnome.Nautilus", "org.kde.dolphin", "thunar")
}

func resolveLinuxDesktopIcon(icon string) string {
	icon = strings.TrimSpace(icon)
	if icon == "" {
		return ""
	}
	if filepath.IsAbs(icon) {
		if info, err := os.Stat(icon); err == nil && !info.IsDir() {
			return icon
		}
		return ""
	}
	ext := filepath.Ext(icon)
	name := strings.TrimSuffix(icon, ext)
	exts := []string{".png", ".svg", ".webp", ".jpg"}
	if ext != "" {
		exts = []string{ext}
	}
	home, _ := os.UserHomeDir()
	iconRoots := []string{"/usr/share/icons", "/usr/local/share/icons"}
	if home != "" {
		iconRoots = append([]string{filepath.Join(home, ".local", "share", "icons"), filepath.Join(home, ".icons")}, iconRoots...)
	}
	sizes := []string{"64x64", "128x128", "48x48", "256x256", "512x512", "scalable"}
	for _, root := range iconRoots {
		for _, size := range sizes {
			for _, suffix := range exts {
				patterns := []string{
					filepath.Join(root, "hicolor", size, "apps", name+suffix),
					filepath.Join(root, "*", size, "apps", name+suffix),
				}
				for _, pattern := range patterns {
					matches, _ := filepath.Glob(pattern)
					for _, match := range matches {
						if info, err := os.Stat(match); err == nil && !info.IsDir() {
							return match
						}
					}
				}
			}
		}
	}
	for _, root := range []string{"/usr/share/pixmaps", "/usr/local/share/pixmaps"} {
		for _, suffix := range exts {
			path := filepath.Join(root, name+suffix)
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				return path
			}
		}
	}
	return ""
}

func platformExternalOpenerSpecs() []externalOpenerSpec {
	entries := linuxDesktopEntries()
	gio := linuxExecutable("gio")
	var specs []externalOpenerSpec
	add := func(id, name, kind, mode string, executables, desktopAliases []string) {
		target := linuxExecutable(executables...)
		entry, hasEntry := findLinuxDesktopEntry(entries, desktopAliases...)
		launchMode := mode
		if target == "" && hasEntry && gio != "" {
			target = entry.Path
			launchMode = "gio"
		}
		if target == "" {
			return
		}
		iconSource := ""
		if hasEntry {
			iconSource = resolveLinuxDesktopIcon(entry.Icon)
		}
		specs = append(specs, externalOpenerSpec{
			View:       ExternalOpenerView{ID: id, Name: name, Kind: kind},
			Target:     target,
			LaunchMode: launchMode,
			IconSource: iconSource,
		})
	}

	add("vscode", "VS Code", externalOpenerEditor, "path", []string{"code"}, []string{"code", "visual-studio-code", "com.visualstudio.code"})
	add("vscode-insiders", "VS Code Insiders", externalOpenerEditor, "path", []string{"code-insiders"}, []string{"code-insiders", "visual-studio-code-insiders"})
	add("cursor", "Cursor", externalOpenerEditor, "path", []string{"cursor"}, []string{"cursor", "com.todesktop.230313mzl4w4u92"})
	if target := linuxExecutable("xdg-open"); target != "" {
		name := "File Manager"
		iconSource := ""
		if entry, ok := linuxDefaultDirectoryDesktopEntry(entries); ok {
			name = entry.Name
			iconSource = resolveLinuxDesktopIcon(entry.Icon)
		}
		specs = append(specs, externalOpenerSpec{
			View:       ExternalOpenerView{ID: "file-manager", Name: name, Kind: externalOpenerFileManager},
			Target:     target,
			LaunchMode: "path",
			IconSource: iconSource,
		})
	}
	add("ghostty", "Ghostty", externalOpenerTerminal, "ghostty", []string{"ghostty"}, []string{"com.mitchellh.ghostty", "ghostty"})
	add("gnome-terminal", "GNOME Terminal", externalOpenerTerminal, "gnome-terminal", []string{"gnome-terminal"}, []string{"org.gnome.Terminal", "gnome-terminal"})
	add("konsole", "Konsole", externalOpenerTerminal, "konsole", []string{"konsole"}, []string{"org.kde.konsole", "konsole"})
	add("kitty", "Kitty", externalOpenerTerminal, "kitty", []string{"kitty"}, []string{"kitty"})
	add("alacritty", "Alacritty", externalOpenerTerminal, "alacritty", []string{"alacritty"}, []string{"Alacritty", "alacritty"})
	add("terminal", "Terminal", externalOpenerTerminal, "cwd", []string{"x-terminal-emulator"}, []string{"terminal"})
	add("android-studio", "Android Studio", externalOpenerEditor, "path", []string{"android-studio", "studio", "studio.sh"}, []string{"android-studio", "com.google.AndroidStudio"})
	add("goland", "GoLand", externalOpenerEditor, "path", []string{"goland", "goland.sh"}, []string{"jetbrains-goland", "goland"})
	add("pycharm", "PyCharm", externalOpenerEditor, "path", []string{"pycharm", "pycharm.sh"}, []string{"jetbrains-pycharm", "pycharm"})
	add("intellij-idea", "IntelliJ IDEA", externalOpenerEditor, "path", []string{"idea", "idea.sh", "intellij-idea"}, []string{"jetbrains-idea", "intellij-idea"})
	add("webstorm", "WebStorm", externalOpenerEditor, "path", []string{"webstorm", "webstorm.sh"}, []string{"jetbrains-webstorm", "webstorm"})
	add("datagrip", "DataGrip", externalOpenerEditor, "path", []string{"datagrip", "datagrip.sh"}, []string{"jetbrains-datagrip", "datagrip"})
	add("codebuddy", "CodeBuddy", externalOpenerEditor, "path", []string{"codebuddy"}, []string{"codebuddy", "com.tencent.codebuddy"})
	add("windsurf", "Windsurf", externalOpenerEditor, "path", []string{"windsurf"}, []string{"windsurf", "com.exafunction.windsurf"})
	add("zed", "Zed", externalOpenerEditor, "path", []string{"zed"}, []string{"dev.zed.Zed", "zed"})
	add("sublime-text", "Sublime Text", externalOpenerEditor, "path", []string{"subl", "sublime_text"}, []string{"sublime_text", "sublime-text"})
	add("kiro", "Kiro", externalOpenerEditor, "path", []string{"kiro"}, []string{"kiro", "dev.kiro.desktop"})
	return specs
}

func platformExternalOpenerIconDataURL(spec externalOpenerSpec) string {
	return externalOpenerIconFileDataURL(spec.IconSource)
}

func linuxExternalOpenerCommand(spec externalOpenerSpec, path string) (*exec.Cmd, error) {
	target := spec.Target
	var args []string
	switch spec.LaunchMode {
	case "path":
		args = []string{path}
	case "gio":
		args = []string{"launch", spec.Target, path}
		target = linuxExecutable("gio")
	case "ghostty", "gnome-terminal":
		args = []string{"--working-directory=" + path}
	case "konsole":
		args = []string{"--workdir", path}
	case "kitty":
		args = []string{"--directory", path}
	case "alacritty":
		args = []string{"--working-directory", path}
	}
	if target == "" {
		return nil, os.ErrNotExist
	}
	cmd := exec.Command(target, args...)
	cmd.Dir = path
	return cmd, nil
}

func launchPlatformExternalOpener(spec externalOpenerSpec, path string) error {
	cmd, err := linuxExternalOpenerCommand(spec, path)
	if err != nil {
		return err
	}
	return startDetachedExternalOpener(cmd)
}
