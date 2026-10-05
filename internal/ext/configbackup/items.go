package configbackup

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"reasonix/internal/base/secrets"
	"reasonix/internal/contract/config"
	"reasonix/internal/ext/hook"
	"reasonix/internal/ext/pluginpkg"
)

// The payload of each kind. Config-owned kinds carry TOML under the config
// file's own keys, so the snapshot schema moves with the config schema instead
// of restating it.
type (
	tomlData struct {
		TOML string `json:"toml"`
	}
	generalData struct {
		DefaultModel string `json:"defaultModel,omitempty"`
		Language     string `json:"language,omitempty"`
	}
	fileData struct {
		Path string `json:"path"`
		Data []byte `json:"data"`
	}
	skillData struct {
		Files []fileData `json:"files"`
	}
	memoryData struct {
		Root string `json:"root"`
		fileData
	}
	pluginData struct {
		Source  string `json:"source"`
		Version string `json:"version,omitempty"`
		Commit  string `json:"commit,omitempty"`
	}
	hookData struct {
		Event string          `json:"event"`
		Hook  hook.HookConfig `json:"hook"`
	}
	statuslineData struct {
		Command string `json:"command"`
	}
	secretData struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
)

// interfaceSettings is the part of [ui] and [desktop] that describes how the
// app looks. Machine facts (editor command, pinned version, wallpaper file) and
// posture (default approval mode) are left out on purpose.
type interfaceSettings struct {
	UI      config.UIConfig `toml:"ui"`
	Desktop desktopLook     `toml:"desktop"`
}

type desktopLook struct {
	Language             string   `toml:"language"`
	Theme                string   `toml:"theme"`
	ThemeStyle           string   `toml:"theme_style"`
	ThemePack            string   `toml:"theme_pack"`
	TerminalTheme        string   `toml:"terminal_theme"`
	CloseBehavior        string   `toml:"close_behavior"`
	Tray                 string   `toml:"tray"`
	DisplayMode          string   `toml:"display_mode"`
	StatusBarStyle       string   `toml:"status_bar_style"`
	StatusBarItems       []string `toml:"status_bar_items"`
	ReasoningDisplayMode string   `toml:"reasoning_display_mode"`
	ConversationWidth    string   `toml:"conversation_width"`
	Zoom                 float64  `toml:"zoom"`
	ReadSize             float64  `toml:"read_size"`
	FontUI               string   `toml:"font_ui"`
	FontMono             string   `toml:"font_mono"`
	WallpaperOpacity     float64  `toml:"wallpaper_opacity"`
	WallpaperDim         float64  `toml:"wallpaper_dim"`
}

func lookOf(cfg *config.Config) interfaceSettings {
	d, a := cfg.Desktop, cfg.Desktop.Appearance
	return interfaceSettings{UI: cfg.UI, Desktop: desktopLook{
		Language: d.Language, Theme: d.Theme, ThemeStyle: d.ThemeStyle, ThemePack: d.ThemePack,
		TerminalTheme: d.TerminalTheme, CloseBehavior: d.CloseBehavior, Tray: d.Tray,
		DisplayMode: d.DisplayMode, StatusBarStyle: d.StatusBarStyle, StatusBarItems: d.StatusBarItems,
		ReasoningDisplayMode: d.ReasoningDisplayMode, ConversationWidth: d.ConversationWidth,
		Zoom: a.Zoom, ReadSize: a.ReadSize, FontUI: a.FontUI, FontMono: a.FontMono,
		WallpaperOpacity: a.Wallpaper.Opacity, WallpaperDim: a.Wallpaper.Dim,
	}}
}

func (s interfaceSettings) applyTo(cfg *config.Config) {
	cfg.UI = s.UI
	d, l := &cfg.Desktop, s.Desktop
	d.Language, d.Theme, d.ThemeStyle, d.ThemePack = l.Language, l.Theme, l.ThemeStyle, l.ThemePack
	d.TerminalTheme, d.CloseBehavior, d.Tray = l.TerminalTheme, l.CloseBehavior, l.Tray
	d.DisplayMode, d.StatusBarStyle, d.StatusBarItems = l.DisplayMode, l.StatusBarStyle, l.StatusBarItems
	d.ReasoningDisplayMode, d.ConversationWidth = l.ReasoningDisplayMode, l.ConversationWidth
	a := &d.Appearance
	a.Zoom, a.ReadSize, a.FontUI, a.FontMono = l.Zoom, l.ReadSize, l.FontUI, l.FontMono
	if a.Wallpaper.File != "" {
		a.Wallpaper.Opacity, a.Wallpaper.Dim = l.WallpaperOpacity, l.WallpaperDim
	}
}

func encodeTOML(v any) (json.RawMessage, error) {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(v); err != nil {
		return nil, err
	}
	return json.Marshal(tomlData{TOML: buf.String()})
}

func decodeTOML(raw json.RawMessage, v any) error {
	var d tomlData
	if err := json.Unmarshal(raw, &d); err != nil {
		return err
	}
	_, err := toml.Decode(d.TOML, v)
	return err
}

// stripSecrets runs the plugin exporter's own credential stripper over the
// env and headers of one named unit, so a backup without secrets leaves the
// machine exactly as an exported plugin package does.
func stripSecrets(scope string, env, headers map[string]string) (map[string]string, map[string]string) {
	if len(env) == 0 && len(headers) == 0 {
		return env, headers
	}
	doc := map[string]any{"mcpServers": map[string]any{scope: map[string]any{"env": env, "headers": headers}}}
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, nil
	}
	stripped, _, err := pluginpkg.StripCredentials(raw)
	if err != nil {
		return nil, nil
	}
	var back struct {
		MCPServers map[string]struct {
			Env     map[string]string `json:"env"`
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	if json.Unmarshal(stripped, &back) != nil {
		return nil, nil
	}
	unit := back.MCPServers[scope]
	return unit.Env, unit.Headers
}

// keepLocalSecrets undoes a strip on the restoring side: a placeholder that
// stands for a value this machine already holds must not overwrite it.
func keepLocalSecrets(restored, local map[string]string) map[string]string {
	for k, v := range restored {
		if isPlaceholder(v) {
			if have, ok := local[k]; ok {
				restored[k] = have
			}
		}
	}
	return restored
}

func isPlaceholder(v string) bool {
	v = strings.TrimSpace(v)
	return strings.HasPrefix(v, "${") && strings.HasSuffix(v, "}")
}

// stripURLSecrets masks credentials in a URL (userinfo, credential-named
// query and path parameters). A value that is not a URL (a scp-style git
// source, a bare path) has no endpoint to project and passes through.
func stripURLSecrets(raw string) string {
	if !strings.Contains(raw, "://") {
		return raw
	}
	return secrets.RedactEndpoint(raw)
}

// holdsLocal reports whether a restored, redacted MCP entry is the local one
// with its credentials masked, so the local URL and args can be kept.
func holdsLocal(restored, have config.PluginEntry) bool {
	return have.Name == restored.Name && have.Command == restored.Command &&
		(have.URL == restored.URL || secrets.RedactEndpoint(have.URL) == restored.URL) &&
		(slices.Equal(have.Args, restored.Args) || slices.Equal(secrets.RedactArgs(have.Args), restored.Args))
}

func hookName(event string, h hook.HookConfig) string {
	sum := sha256.Sum256([]byte(event + "\x00" + h.Match + "\x00" + h.Command))
	return event + "#" + hex.EncodeToString(sum[:6])
}

func secretsRedacted(p config.ProviderEntry) config.ProviderEntry {
	for _, u := range []*string{&p.BaseURL, &p.ChatURL, &p.RequestURL, &p.ModelsURL, &p.BalanceURL} {
		*u = stripURLSecrets(*u)
	}
	return p
}
