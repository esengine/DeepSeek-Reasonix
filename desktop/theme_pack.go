package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

// Theme Pack V2 is a controlled, non-executable desktop skin. V1 manifests
// remain readable and fall back to one shared scene image.
// See docs/THEME_PACK.md for the public contract.

const (
	themePackSchemaVersion    = 2
	themePackMinSchemaVersion = 1
	themePackMaxZipBytes      = 36 << 20 // two bounded scene images + manifest
	themePackMaxManifest      = 1 << 20  // 1 MiB
	themePackMaxImageBytes    = 16 << 20 // 16 MiB
	themePackMaxImageEdge     = 8192
	themePackMaxIDLen         = 64
	themePackMaxNameLen       = 80
	themePackMaxTextLen       = 240
	themePackManifestName     = "theme.json"
	themePackExt              = ".reasonix-theme"
	themeStateFileName        = "desktop-theme-state.json"
	// Schema v2: activeThemeId may only reference official or user packs.
	// Base style ids (graphite/…) live exclusively in desktop.theme_style.
	themeStateSchemaVer   = 2
	themeStateSchemaVerV1 = 1
	themeDirName          = "themes"
)

// Allowed base styles match the existing desktop theme directions.
var themePackBaseStyles = map[string]struct{}{
	"graphite": {},
	"aurora":   {},
	"slate":    {},
	"carbon":   {},
	"nocturne": {},
	"amber":    {},
}

// Token keys that a pack may override. Values must be #RRGGBB or #RRGGBBAA.
var themePackTokenKeys = map[string]string{
	"bg":             "--bg",
	"bgSoft":         "--bg-soft",
	"bgElev":         "--bg-elev",
	"panel":          "--panel",
	"sidebar":        "--sidebar-bg",
	"chat":           "--chat-bg",
	"workspace":      "--workspace-preview-bg",
	"workspaceFiles": "--workspace-files-bg",
	"border":         "--border",
	"borderSoft":     "--border-soft",
	"fg":             "--fg",
	"fgDim":          "--fg-dim",
	"fgFaint":        "--fg-faint",
	"accent":         "--accent",
	"accentFg":       "--accent-fg",
	"ok":             "--ok",
	"warn":           "--warn",
	"err":            "--err",
}

var (
	themePackIDRe    = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}[a-z0-9]$|^[a-z]$`)
	themePackColorRe = regexp.MustCompile(`^#([0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)
	themePackImageRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,120}\.(png|jpe?g|webp)$`)
)

// ThemePackManifest is the on-disk theme.json contract.
type ThemePackManifest struct {
	SchemaVersion  int                       `json:"schemaVersion"`
	ID             string                    `json:"id"`
	Name           string                    `json:"name"`
	Author         string                    `json:"author,omitempty"`
	Description    string                    `json:"description,omitempty"`
	License        string                    `json:"license,omitempty"`
	BaseStyle      string                    `json:"baseStyle"`
	Tokens         ThemePackTokens           `json:"tokens"`
	Recipes        ThemePackRecipes          `json:"recipes"`
	Background     *ThemePackBackground      `json:"background,omitempty"`
	TaskBackground *ThemePackSceneBackground `json:"taskBackground,omitempty"`
	Extra          map[string]interface{}    `json:"-"` // rejected on parse when present as unknown top-level
}

// ThemePackTokens holds optional light/dark semantic color overrides.
type ThemePackTokens struct {
	Light map[string]string `json:"light,omitempty"`
	Dark  map[string]string `json:"dark,omitempty"`
}

// ThemePackRecipes maps density/corner enums to bounded component variables.
type ThemePackRecipes struct {
	Density string `json:"density,omitempty"` // compact|comfortable
	Corners string `json:"corners,omitempty"` // square|soft|round
}

// ThemePackBackground is an optional local background image with focus/safe area.
type ThemePackBackground struct {
	Image           string   `json:"image,omitempty"`
	FocusX          float64  `json:"focusX"`
	FocusY          float64  `json:"focusY"`
	SafeArea        string   `json:"safeArea,omitempty"` // left|right|center
	HomeOpacity     float64  `json:"homeOpacity"`
	TaskOpacity     float64  `json:"taskOpacity"`
	OverlayStrength float64  `json:"overlayStrength"`
	PaneOpacity     *float64 `json:"paneOpacity,omitempty"` // home scene pane transparency (0=clear, 1=opaque)
}

// ThemePackSceneBackground optionally overrides the task/workspace scene.
// V1 packs omit it and continue using Background with TaskOpacity.
type ThemePackSceneBackground struct {
	Image           string   `json:"image,omitempty"`
	FocusX          float64  `json:"focusX"`
	FocusY          float64  `json:"focusY"`
	SafeArea        string   `json:"safeArea,omitempty"` // left|right|center
	Opacity         float64  `json:"opacity"`
	OverlayStrength float64  `json:"overlayStrength"`
	PaneOpacity     *float64 `json:"paneOpacity,omitempty"` // task scene pane transparency (0=clear, 1=opaque)
}

// ThemeDesktopState is the versioned active-theme pointer (not config.toml).
type ThemeDesktopState struct {
	SchemaVersion int    `json:"schemaVersion"`
	ActiveThemeID string `json:"activeThemeId,omitempty"`
}

// ThemePackView is the frontend-safe summary of a theme (base, official or user).
type ThemePackView struct {
	ID                string                    `json:"id"`
	Name              string                    `json:"name"`
	Author            string                    `json:"author,omitempty"`
	Description       string                    `json:"description,omitempty"`
	License           string                    `json:"license,omitempty"`
	BaseStyle         string                    `json:"baseStyle"`
	Builtin           bool                      `json:"builtin"`
	Kind              string                    `json:"kind"` // "base" | "official" | "user"
	Active            bool                      `json:"active"`
	HasBackground     bool                      `json:"hasBackground"`
	BackgroundURL     string                    `json:"backgroundUrl,omitempty"`
	TaskBackgroundURL string                    `json:"taskBackgroundUrl,omitempty"`
	PreviewURL        string                    `json:"previewUrl,omitempty"`
	NameKey           string                    `json:"nameKey,omitempty"`
	DescriptionKey    string                    `json:"descriptionKey,omitempty"`
	Tokens            ThemePackTokens           `json:"tokens"`
	Recipes           ThemePackRecipes          `json:"recipes"`
	Background        *ThemePackBackground      `json:"background,omitempty"`
	TaskBackground    *ThemePackSceneBackground `json:"taskBackground,omitempty"`
	ContrastWarnings  []ThemeContrastWarning    `json:"contrastWarnings,omitempty"`
}

// ThemeContrastWarning surfaces WCAG contrast issues without blocking save.
type ThemeContrastWarning struct {
	Mode    string  `json:"mode"` // light|dark
	Pair    string  `json:"pair"` // e.g. fg/bg
	Ratio   float64 `json:"ratio"`
	Minimum float64 `json:"minimum"`
	Suggest string  `json:"suggest,omitempty"`
}

// ThemeActiveView is what the frontend needs to apply a pack + scene styling.
type ThemeActiveView struct {
	ActiveThemeID string         `json:"activeThemeId,omitempty"`
	Pack          *ThemePackView `json:"pack,omitempty"`
	SafeMode      bool           `json:"safeMode"`
}

// ThemeExperienceView is the unified appearance state for the redesigned
// settings overview + theme gallery. One call supplies everything the UI needs
// without inferring which style is actually effective.
type ThemeExperienceView struct {
	ThemeMode      string         `json:"themeMode"`               // auto|light|dark
	BaseStyle      string         `json:"baseStyle"`               // graphite|aurora|…
	EffectiveStyle string         `json:"effectiveStyle"`          // pack.baseStyle when pack active, else baseStyle
	ActiveThemeID  string         `json:"activeThemeId,omitempty"` // official/user only; never a base id
	ActivePack     *ThemePackView `json:"activePack,omitempty"`
	SafeMode       bool           `json:"safeMode"`
}

// ThemeSaveInput is the editor payload for creating/updating a user theme.
type ThemeSaveInput struct {
	ID             string                    `json:"id"`
	Name           string                    `json:"name"`
	Author         string                    `json:"author,omitempty"`
	Description    string                    `json:"description,omitempty"`
	License        string                    `json:"license,omitempty"`
	BaseStyle      string                    `json:"baseStyle"`
	Tokens         ThemePackTokens           `json:"tokens"`
	Recipes        ThemePackRecipes          `json:"recipes"`
	Background     *ThemePackBackground      `json:"background,omitempty"`
	TaskBackground *ThemePackSceneBackground `json:"taskBackground,omitempty"`
	// BackgroundDataURL is an optional data:image/... payload used when the
	// editor picked a new local image. Empty keeps the existing image.
	BackgroundDataURL     string `json:"backgroundDataUrl,omitempty"`
	TaskBackgroundDataURL string `json:"taskBackgroundDataUrl,omitempty"`
	// ClearBackground removes any existing background image.
	ClearBackground     bool `json:"clearBackground,omitempty"`
	ClearTaskBackground bool `json:"clearTaskBackground,omitempty"`
	// Replace allows overwriting an existing user theme with the same ID.
	Replace bool `json:"replace,omitempty"`
	// Activate enables the theme after a successful save.
	Activate bool `json:"activate,omitempty"`
}

// ThemeImportResult is returned after a ZIP import attempt.
// When NeedsReplace is true, the package was staged server-side and ConfirmImportThemePack
// (or ImportThemePack with replace=true) will publish without re-opening a file dialog.
// Absolute host paths are never exposed to the frontend.
type ThemeImportResult struct {
	Pack         ThemePackView `json:"pack"`
	Replaced     bool          `json:"replaced"`
	NeedsReplace bool          `json:"needsReplace,omitempty"`
	PendingID    string        `json:"pendingId,omitempty"`
}

func defaultThemePackRecipes() ThemePackRecipes {
	return ThemePackRecipes{Density: "comfortable", Corners: "soft"}
}

func themePackFloat64(value float64) *float64 {
	return &value
}

func defaultThemePackBackground() ThemePackBackground {
	return ThemePackBackground{
		FocusX:          0.5,
		FocusY:          0.5,
		SafeArea:        "center",
		HomeOpacity:     1,
		TaskOpacity:     0.28,
		OverlayStrength: 0.62,
		PaneOpacity:     themePackFloat64(0.72),
	}
}

func defaultThemePackTaskBackground() ThemePackSceneBackground {
	return ThemePackSceneBackground{
		FocusX:          0.5,
		FocusY:          0.5,
		SafeArea:        "center",
		Opacity:         0.28,
		OverlayStrength: 0.62,
		PaneOpacity:     themePackFloat64(0.80),
	}
}

func parseThemePackManifest(data []byte) (*ThemePackManifest, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("theme manifest is empty")
	}
	if len(data) > themePackMaxManifest {
		return nil, fmt.Errorf("theme manifest exceeds %d bytes", themePackMaxManifest)
	}
	// Reject top-level keys outside the versioned allow-list.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("theme manifest JSON: %w", err)
	}
	allowed := map[string]struct{}{
		"schemaVersion":  {},
		"id":             {},
		"name":           {},
		"author":         {},
		"description":    {},
		"license":        {},
		"baseStyle":      {},
		"tokens":         {},
		"recipes":        {},
		"background":     {},
		"taskBackground": {},
	}
	for k := range raw {
		if _, ok := allowed[k]; !ok {
			return nil, fmt.Errorf("theme manifest unknown field %q", k)
		}
	}
	var m ThemePackManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("theme manifest JSON: %w", err)
	}
	if err := validateThemePackManifest(&m); err != nil {
		return nil, err
	}
	return &m, nil
}

func validateThemePackManifest(m *ThemePackManifest) error {
	if m == nil {
		return fmt.Errorf("theme manifest is nil")
	}
	if m.SchemaVersion < themePackMinSchemaVersion || m.SchemaVersion > themePackSchemaVersion {
		return fmt.Errorf("unsupported theme schemaVersion %d (supported %d-%d)", m.SchemaVersion, themePackMinSchemaVersion, themePackSchemaVersion)
	}
	if m.SchemaVersion < 2 && m.TaskBackground != nil {
		return fmt.Errorf("taskBackground requires theme schemaVersion 2")
	}
	id := strings.TrimSpace(m.ID)
	if !themePackIDRe.MatchString(id) {
		return fmt.Errorf("invalid theme id %q (use lowercase letters, digits, hyphens)", m.ID)
	}
	m.ID = id
	name := strings.TrimSpace(m.Name)
	if name == "" || len(name) > themePackMaxNameLen {
		return fmt.Errorf("theme name must be 1–%d characters", themePackMaxNameLen)
	}
	if containsControl(name) {
		return fmt.Errorf("theme name contains control characters")
	}
	m.Name = name
	m.Author = clampThemeText(m.Author)
	m.Description = clampThemeText(m.Description)
	m.License = clampThemeText(m.License)

	base := strings.ToLower(strings.TrimSpace(m.BaseStyle))
	if _, ok := themePackBaseStyles[base]; !ok {
		return fmt.Errorf("invalid baseStyle %q", m.BaseStyle)
	}
	m.BaseStyle = base

	if err := validateThemeTokenMap(m.Tokens.Light, "tokens.light"); err != nil {
		return err
	}
	if err := validateThemeTokenMap(m.Tokens.Dark, "tokens.dark"); err != nil {
		return err
	}

	recipes := m.Recipes
	if recipes.Density == "" {
		recipes.Density = "comfortable"
	}
	if recipes.Corners == "" {
		recipes.Corners = "soft"
	}
	switch recipes.Density {
	case "compact", "comfortable":
	default:
		return fmt.Errorf("invalid density %q (use compact|comfortable)", recipes.Density)
	}
	switch recipes.Corners {
	case "square", "soft", "round":
	default:
		return fmt.Errorf("invalid corners %q (use square|soft|round)", recipes.Corners)
	}
	m.Recipes = recipes

	if m.Background != nil {
		bg, err := normalizeThemeBackground(m.Background)
		if err != nil {
			return err
		}
		m.Background = bg
	}
	if m.TaskBackground != nil {
		bg, err := normalizeThemeSceneBackground(m.TaskBackground)
		if err != nil {
			return err
		}
		m.TaskBackground = bg
	}
	if m.Background != nil && m.TaskBackground != nil && strings.EqualFold(m.Background.Image, m.TaskBackground.Image) {
		return fmt.Errorf("background and taskBackground must use different image names")
	}
	return nil
}

func validateThemeTokenMap(tokens map[string]string, path string) error {
	if tokens == nil {
		return nil
	}
	for k, v := range tokens {
		if _, ok := themePackTokenKeys[k]; !ok {
			return fmt.Errorf("%s: unknown token %q", path, k)
		}
		color := strings.TrimSpace(v)
		if !themePackColorRe.MatchString(color) {
			return fmt.Errorf("%s.%s: color must be #RRGGBB or #RRGGBBAA, got %q", path, k, v)
		}
		// Reject CSS functions / gradients / url() even if somehow encoded.
		lower := strings.ToLower(color)
		if strings.Contains(lower, "url(") || strings.Contains(lower, "gradient") || strings.Contains(lower, "expression") {
			return fmt.Errorf("%s.%s: disallowed color value", path, k)
		}
		tokens[k] = strings.ToLower(color)
	}
	return nil
}

func normalizeThemeBackground(in *ThemePackBackground) (*ThemePackBackground, error) {
	if in == nil {
		return nil, nil
	}
	out := defaultThemePackBackground()
	if in.Image != "" {
		raw := strings.TrimSpace(in.Image)
		raw = strings.ReplaceAll(raw, "\\", "/")
		// Reject any path form — only a bare file name is allowed in the manifest.
		if raw == "" || strings.Contains(raw, "/") || strings.Contains(raw, "..") || filepath.Base(raw) != raw {
			return nil, fmt.Errorf("background.image must be a plain file name")
		}
		if !themePackImageRe.MatchString(raw) {
			return nil, fmt.Errorf("background.image must be a local png/jpeg/webp file name")
		}
		out.Image = raw
	}
	out.FocusX = clamp01(in.FocusX, 0.5)
	out.FocusY = clamp01(in.FocusY, 0.5)
	safe := strings.ToLower(strings.TrimSpace(in.SafeArea))
	if safe == "" {
		safe = "center"
	}
	switch safe {
	case "left", "right", "center":
		out.SafeArea = safe
	default:
		return nil, fmt.Errorf("background.safeArea must be left|right|center")
	}
	// Home may be full strength; task opacity is capped for readability.
	out.HomeOpacity = clampFloat(in.HomeOpacity, 0, 1, 1)
	out.TaskOpacity = clampFloat(in.TaskOpacity, 0, 1, 0.28)
	out.OverlayStrength = clampFloat(in.OverlayStrength, 0, 1, 0.62)
	if in.PaneOpacity != nil {
		out.PaneOpacity = themePackFloat64(clampFloat(*in.PaneOpacity, 0, 1, 0.50))
	}
	// Empty image means token-only pack — drop background block.
	if out.Image == "" {
		return nil, nil
	}
	return &out, nil
}

func normalizeThemeSceneBackground(in *ThemePackSceneBackground) (*ThemePackSceneBackground, error) {
	if in == nil {
		return nil, nil
	}
	out := defaultThemePackTaskBackground()
	if in.Image != "" {
		raw := strings.TrimSpace(in.Image)
		raw = strings.ReplaceAll(raw, "\\", "/")
		if raw == "" || strings.Contains(raw, "/") || strings.Contains(raw, "..") || filepath.Base(raw) != raw {
			return nil, fmt.Errorf("taskBackground.image must be a plain file name")
		}
		if !themePackImageRe.MatchString(raw) {
			return nil, fmt.Errorf("taskBackground.image must be a local png/jpeg/webp file name")
		}
		out.Image = raw
	}
	out.FocusX = clamp01(in.FocusX, 0.5)
	out.FocusY = clamp01(in.FocusY, 0.5)
	safe := strings.ToLower(strings.TrimSpace(in.SafeArea))
	if safe == "" {
		safe = "center"
	}
	switch safe {
	case "left", "right", "center":
		out.SafeArea = safe
	default:
		return nil, fmt.Errorf("taskBackground.safeArea must be left|right|center")
	}
	out.Opacity = clampFloat(in.Opacity, 0, 1, 0.28)
	out.OverlayStrength = clampFloat(in.OverlayStrength, 0, 1, 0.62)
	if in.PaneOpacity != nil {
		out.PaneOpacity = themePackFloat64(clampFloat(*in.PaneOpacity, 0, 1, 0.68))
	}
	if out.Image == "" {
		return nil, nil
	}
	return &out, nil
}

func clampThemeText(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > themePackMaxTextLen {
		s = s[:themePackMaxTextLen]
	}
	if containsControl(s) {
		// Strip controls rather than reject optional fields.
		var b strings.Builder
		for _, r := range s {
			if unicode.IsControl(r) && r != '\n' && r != '\t' {
				continue
			}
			b.WriteRune(r)
		}
		s = strings.TrimSpace(b.String())
	}
	return s
}

func containsControl(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return true
		}
	}
	return false
}

func clamp01(v, def float64) float64 {
	return clampFloat(v, 0, 1, def)
}

func clampFloat(v, min, max, def float64) float64 {
	if v != v { // NaN
		return def
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func isBuiltinThemeID(id string) bool {
	_, ok := themePackBaseStyles[id]
	return ok
}

func builtinThemePacks() []ThemePackManifest {
	// Built-in packs mirror the six style directions with empty token overrides.
	order := []string{"graphite", "aurora", "slate", "carbon", "nocturne", "amber"}
	names := map[string]string{
		"graphite": "Graphite",
		"aurora":   "Aurora",
		"slate":    "Slate",
		"carbon":   "Carbon",
		"nocturne": "Nocturne",
		"amber":    "Amber",
	}
	out := make([]ThemePackManifest, 0, len(order))
	for _, id := range order {
		out = append(out, ThemePackManifest{
			SchemaVersion: themePackSchemaVersion,
			ID:            id,
			Name:          names[id],
			Author:        "Reasonix",
			Description:   "Built-in visual direction",
			License:       "Apache-2.0",
			BaseStyle:     id,
			Tokens:        ThemePackTokens{},
			Recipes:       defaultThemePackRecipes(),
		})
	}
	return out
}

func manifestToView(m *ThemePackManifest, kind string, active bool, backgroundURL, previewURL string, taskBackgroundURLs ...string) ThemePackView {
	taskBackgroundURL := ""
	if len(taskBackgroundURLs) > 0 {
		taskBackgroundURL = taskBackgroundURLs[0]
	}
	v := ThemePackView{
		ID:                m.ID,
		Name:              m.Name,
		Author:            m.Author,
		Description:       m.Description,
		License:           m.License,
		BaseStyle:         m.BaseStyle,
		Builtin:           kind != themeKindUser,
		Kind:              kind,
		Active:            active,
		HasBackground:     (m.Background != nil && m.Background.Image != "") || (m.TaskBackground != nil && m.TaskBackground.Image != ""),
		BackgroundURL:     backgroundURL,
		TaskBackgroundURL: taskBackgroundURL,
		PreviewURL:        previewURL,
		Tokens: ThemePackTokens{
			Light: copyStringMap(m.Tokens.Light),
			Dark:  copyStringMap(m.Tokens.Dark),
		},
		Recipes: m.Recipes,
	}
	if kind == themeKindOfficial {
		v.NameKey = "settings.themes.official." + m.ID + ".name"
		v.DescriptionKey = "settings.themes.official." + m.ID + ".description"
	}
	if m.Background != nil {
		bg := *m.Background
		v.Background = &bg
	}
	if m.TaskBackground != nil {
		bg := *m.TaskBackground
		v.TaskBackground = &bg
	}
	v.ContrastWarnings = computeContrastWarnings(m)
	return v
}

func copyStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func themePackCSSVars(tokens map[string]string) map[string]string {
	if len(tokens) == 0 {
		return nil
	}
	out := make(map[string]string, len(tokens)*2)
	for k, v := range tokens {
		css, ok := themePackTokenKeys[k]
		if !ok {
			continue
		}
		out[css] = v
		// Keep dual aliases used across stylesheets in sync.
		switch k {
		case "fg":
			out["--text"] = v
		case "fgDim":
			out["--text-2"] = v
		case "fgFaint":
			out["--text-3"] = v
		case "panel":
			out["--bg-elev"] = v
			out["--surface"] = v
		case "bg":
			out["--stage"] = v
		case "bgSoft":
			out["--bg-soft"] = v
			out["--surface-3"] = v
		case "accent":
			// Soft accent is derived client-side; keep strong close to accent.
			out["--accent-strong"] = v
			out["--control-primary-bg"] = v
		}
	}
	return out
}

func recipeCSSVars(r ThemePackRecipes) map[string]string {
	out := map[string]string{}
	switch r.Density {
	case "compact":
		out["--theme-density-pad"] = "6px"
		out["--theme-density-gap"] = "6px"
		out["--theme-row-h"] = "28px"
	default:
		out["--theme-density-pad"] = "10px"
		out["--theme-density-gap"] = "10px"
		out["--theme-row-h"] = "34px"
	}
	switch r.Corners {
	case "square":
		out["--r-s"] = "0px"
		out["--r"] = "2px"
		out["--r-l"] = "4px"
		out["--radius"] = "2px"
	case "round":
		out["--r-s"] = "8px"
		out["--r"] = "14px"
		out["--r-l"] = "18px"
		out["--radius"] = "14px"
	default: // soft
		out["--r-s"] = "5px"
		out["--r"] = "8px"
		out["--r-l"] = "11px"
		out["--radius"] = "8px"
	}
	return out
}
