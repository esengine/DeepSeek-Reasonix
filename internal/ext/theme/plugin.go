package theme

import (
	"path/filepath"
	"strings"

	"reasonix/internal/contract/config"
	"reasonix/internal/ext/pluginpkg"
)

// A pack an enabled plugin package contributes is read in place from the
// plugin's root, never copied into the user's library. Its id names the plugin
// and the pack's directory, so it can shadow neither a shipped nor a user pack,
// and it goes away with the plugin rather than lingering after an uninstall.
const pluginPrefix = "plugin:"

type pluginPack struct {
	id  string
	dir string
}

func isPluginID(id string) bool { return strings.HasPrefix(id, pluginPrefix) }

// pluginPacks lists every theme.json an enabled plugin declares under
// contributes.themes. Other theme files are not packs this reader knows.
func pluginPacks() []pluginPack {
	installed, _ := pluginpkg.LoadInstalled(config.RootDir(config.RootHome))
	var out []pluginPack
	for _, item := range installed {
		for _, ref := range item.Package.ThemeFiles() {
			if filepath.Base(ref.Path) != manifestName {
				continue
			}
			dir := filepath.Dir(ref.Path)
			out = append(out, pluginPack{id: pluginPrefix + item.Installed.Name + ":" + filepath.Base(dir), dir: dir})
		}
	}
	return out
}

func pluginDir(id string) (string, bool) {
	for _, p := range pluginPacks() {
		if p.id == id {
			return p.dir, true
		}
	}
	return "", false
}

// assetDir is where a pack's images live on disk. A plugin id is never joined
// onto the user library path: its colon is not a file name on every system.
func assetDir(id string) (string, bool) {
	if isPluginID(id) {
		return pluginDir(id)
	}
	return filepath.Join(Dir(), id), true
}
