package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"reasonix/internal/config"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// themeMu serializes theme library mutations (import/save/delete/activate).
var themeMu sync.Mutex

// stagedThemeImport holds a ZIP extract awaiting replace confirmation.
// Host paths stay on the Go side — the frontend only sees pendingId.
type stagedThemeImport struct {
	id      string
	staging string
	pack    ThemePackView
}

var (
	pendingThemeMu    sync.Mutex
	pendingThemeStage *stagedThemeImport
)

func clearPendingThemeImport() {
	pendingThemeMu.Lock()
	defer pendingThemeMu.Unlock()
	if pendingThemeStage != nil && pendingThemeStage.staging != "" {
		_ = os.RemoveAll(pendingThemeStage.staging)
	}
	pendingThemeStage = nil
}

func setPendingThemeImport(id, staging string, pack ThemePackView) string {
	pendingThemeMu.Lock()
	if pendingThemeStage != nil && pendingThemeStage.staging != "" && pendingThemeStage.staging != staging {
		_ = os.RemoveAll(pendingThemeStage.staging)
	}
	pendingID := "pending-" + id + "-" + randomThemeSuffix()
	pendingThemeStage = &stagedThemeImport{id: id, staging: staging, pack: pack}
	pendingThemeMu.Unlock()
	return pendingID
}

func takePendingThemeImport() *stagedThemeImport {
	pendingThemeMu.Lock()
	defer pendingThemeMu.Unlock()
	p := pendingThemeStage
	pendingThemeStage = nil
	return p
}

// ListThemePacks returns base directions, official themes and user themes.
// Base packs are never "active" as theme packs; their "active" flag means
// "this is the configured base style and no pack is applied".
func (a *App) ListThemePacks() ([]ThemePackView, error) {
	themeMu.Lock()
	defer themeMu.Unlock()

	st := a.migrateThemeDesktopStateLocked()
	activeID := resolveActiveThemeID(st)
	baseStyle := a.desktopBaseStyleLocked()

	safe := a.themeSafeMode()
	var out []ThemePackView
	for _, m := range builtinThemePacks() {
		cp := m
		// Base "active" = no pack applied and this is the configured base style.
		baseActive := !safe && activeID == "" && baseStyle == m.ID
		out = append(out, manifestToView(&cp, themeKindBase, baseActive, "", ""))
	}
	if safe {
		return out, nil
	}
	for _, ot := range officialThemes() {
		m := ot.manifest
		bgURL := officialAssetURL(m.ID, m.Background.Image)
		pvURL := officialAssetURL(m.ID, officialPreviewName)
		out = append(out, manifestToView(&m, themeKindOfficial, activeID == m.ID, bgURL, pvURL))
	}
	ids, err := listUserThemeIDs()
	if err != nil {
		return out, err
	}
	for _, id := range ids {
		m, err := loadUserThemeManifest(id)
		if err != nil {
			continue
		}
		bgURL := ""
		if m.Background != nil && m.Background.Image != "" {
			bgURL = themeBackgroundURL(id, m.Background.Image)
		}
		taskURL := ""
		if m.TaskBackground != nil && m.TaskBackground.Image != "" {
			taskURL = themeBackgroundURL(id, m.TaskBackground.Image)
		}
		out = append(out, manifestToView(m, themeKindUser, activeID == id, bgURL, "", taskURL))
	}
	return out, nil
}

// GetActiveThemePack returns the currently enabled pack (nil pack when none / safe mode).
// Safe mode suppresses pack application but does not delete the stored id.
func (a *App) GetActiveThemePack() (ThemeActiveView, error) {
	themeMu.Lock()
	defer themeMu.Unlock()

	view := ThemeActiveView{SafeMode: a.themeSafeMode()}
	st := a.migrateThemeDesktopStateLocked()
	if view.SafeMode {
		// Do not clear ActiveThemeID — safe mode only blocks loading.
		return view, nil
	}
	activeID := resolveActiveThemeID(st)
	if st.ActiveThemeID != "" && activeID == "" {
		// Broken or migrated-away pointer: clear so the next launch is clean.
		st.ActiveThemeID = ""
		_ = saveThemeDesktopState(st)
		return view, nil
	}
	if activeID == "" {
		return view, nil
	}
	view.ActiveThemeID = activeID
	pack, err := a.loadThemeViewLocked(activeID, true)
	if err != nil {
		st.ActiveThemeID = ""
		_ = saveThemeDesktopState(st)
		view.ActiveThemeID = ""
		return view, nil
	}
	view.Pack = &pack
	return view, nil
}

// GetThemeExperience returns the unified appearance state for overview + gallery.
func (a *App) GetThemeExperience() (ThemeExperienceView, error) {
	themeMu.Lock()
	defer themeMu.Unlock()

	safe := a.themeSafeMode()
	// Migrate first so a v1 base-style activeThemeId lands in desktop.theme_style
	// before we read appearance.
	st := a.migrateThemeDesktopStateLocked()
	themeMode, baseStyle := a.desktopAppearanceLocked()
	view := ThemeExperienceView{
		ThemeMode:      themeMode,
		BaseStyle:      baseStyle,
		EffectiveStyle: baseStyle,
		SafeMode:       safe,
	}
	if safe {
		return view, nil
	}
	activeID := resolveActiveThemeID(st)
	if st.ActiveThemeID != "" && activeID == "" {
		st.ActiveThemeID = ""
		_ = saveThemeDesktopState(st)
		return view, nil
	}
	if activeID == "" {
		return view, nil
	}
	pack, err := a.loadThemeViewLocked(activeID, true)
	if err != nil {
		st.ActiveThemeID = ""
		_ = saveThemeDesktopState(st)
		return view, nil
	}
	view.ActiveThemeID = activeID
	view.ActivePack = &pack
	if pack.BaseStyle != "" {
		view.EffectiveStyle = pack.BaseStyle
	}
	return view, nil
}

func (a *App) loadThemeViewLocked(id string, active bool) (ThemePackView, error) {
	if isBuiltinThemeID(id) {
		m := findBuiltinManifest(id)
		if m == nil {
			return ThemePackView{}, fmt.Errorf("unknown built-in theme %q", id)
		}
		return manifestToView(m, themeKindBase, active, "", ""), nil
	}
	if ot := findOfficialTheme(id); ot != nil {
		m := ot.manifest
		bgURL := officialAssetURL(m.ID, m.Background.Image)
		pvURL := officialAssetURL(m.ID, officialPreviewName)
		return manifestToView(&m, themeKindOfficial, active, bgURL, pvURL), nil
	}
	m, err := loadUserThemeManifest(id)
	if err != nil {
		return ThemePackView{}, err
	}
	bgURL := ""
	if m.Background != nil && m.Background.Image != "" {
		bgURL = themeBackgroundURL(id, m.Background.Image)
	}
	taskURL := ""
	if m.TaskBackground != nil && m.TaskBackground.Image != "" {
		taskURL = themeBackgroundURL(id, m.TaskBackground.Image)
	}
	return manifestToView(m, themeKindUser, active, bgURL, "", taskURL), nil
}

// ActivateThemePack enables an official or user theme. Empty id clears the pack
// (same as DisableThemePack). Base style ids are rejected — use ActivateBaseStyle.
func (a *App) ActivateThemePack(id string) error {
	themeMu.Lock()
	defer themeMu.Unlock()

	id = strings.TrimSpace(id)
	if a.themeSafeMode() && id != "" {
		return fmt.Errorf("safe mode does not load external themes")
	}
	st := a.migrateThemeDesktopStateLocked()
	if id == "" {
		st.ActiveThemeID = ""
		return saveThemeDesktopState(st)
	}
	if isBuiltinThemeID(id) {
		return fmt.Errorf("base style %q is not a theme pack; use ActivateBaseStyle", id)
	}
	if findOfficialTheme(id) != nil {
		st.ActiveThemeID = id
		return saveThemeDesktopState(st)
	}
	if _, err := loadUserThemeManifest(id); err != nil {
		return fmt.Errorf("theme %q is missing or invalid", id)
	}
	st.ActiveThemeID = id
	return saveThemeDesktopState(st)
}

// ActivateBaseStyle writes the base color direction and clears any active pack.
// Theme mode (auto/light/dark), fonts and zoom are preserved.
func (a *App) ActivateBaseStyle(style string) error {
	themeMu.Lock()
	defer themeMu.Unlock()

	style = strings.TrimSpace(strings.ToLower(style))
	if !isBuiltinThemeID(style) {
		return fmt.Errorf("unknown base style %q", style)
	}
	themeMode, _ := a.desktopAppearanceLocked()
	if err := a.SetDesktopAppearance(themeMode, style); err != nil {
		return err
	}
	st := a.migrateThemeDesktopStateLocked()
	st.ActiveThemeID = ""
	return saveThemeDesktopState(st)
}

// DisableThemePack clears the active pack and restores the configured base style.
// Theme mode, fonts and zoom are preserved.
func (a *App) DisableThemePack() error {
	themeMu.Lock()
	defer themeMu.Unlock()
	st := a.migrateThemeDesktopStateLocked()
	st.ActiveThemeID = ""
	return saveThemeDesktopState(st)
}

// RestoreGraphiteAppearance disables any pack and sets base style to Graphite.
// Theme mode, fonts and zoom are preserved.
func (a *App) RestoreGraphiteAppearance() error {
	themeMu.Lock()
	defer themeMu.Unlock()
	themeMode, _ := a.desktopAppearanceLocked()
	if err := a.SetDesktopAppearance(themeMode, "graphite"); err != nil {
		return err
	}
	st := a.migrateThemeDesktopStateLocked()
	st.ActiveThemeID = ""
	return saveThemeDesktopState(st)
}

// ResetThemePack is a compatibility wrapper for older frontends.
// Prefer DisableThemePack or RestoreGraphiteAppearance.
func (a *App) ResetThemePack() error {
	return a.DisableThemePack()
}

// migrateThemeDesktopStateLocked upgrades v1 state and clears invalid ids.
// Caller must hold themeMu. Side effect: may write desktop.theme_style when a
// v1 base-style activeThemeId is migrated.
func (a *App) migrateThemeDesktopStateLocked() ThemeDesktopState {
	st := loadThemeDesktopState()
	changed := false
	id := strings.TrimSpace(st.ActiveThemeID)

	// v1 stored base styles as activeThemeId — move them to desktop.theme_style.
	if isBuiltinThemeID(id) {
		themeMode, _ := a.desktopAppearanceLocked()
		_ = a.SetDesktopAppearance(themeMode, id)
		st.ActiveThemeID = ""
		changed = true
	} else if id != "" && resolveActiveThemeID(st) == "" {
		// Missing / corrupt official or user pack — clear pointer only.
		st.ActiveThemeID = ""
		changed = true
	}
	if st.SchemaVersion != themeStateSchemaVer {
		st.SchemaVersion = themeStateSchemaVer
		changed = true
	}
	if changed {
		_ = saveThemeDesktopState(st)
		st = loadThemeDesktopState()
	}
	return st
}

func (a *App) desktopAppearanceLocked() (themeMode, baseStyle string) {
	// Read-only snapshot of user desktop prefs. applyConfigOnly serializes
	// writers; a concurrent save may race, which is acceptable for UI display.
	cfg := config.LoadForEdit(config.UserConfigPath())
	themeMode = cfg.DesktopTheme()
	if themeMode == "" {
		themeMode = "auto"
	}
	baseStyle = cfg.DesktopThemeStyle()
	// Frontend maps legacy aliases; for API consumers normalize known bases.
	if !isBuiltinThemeID(baseStyle) {
		switch baseStyle {
		case "ember":
			baseStyle = "carbon"
		case "midnight", "porcelain":
			baseStyle = "nocturne"
		case "sandstone", "linen":
			baseStyle = "amber"
		case "glacier":
			baseStyle = "slate"
		default:
			baseStyle = "graphite"
		}
	}
	return themeMode, baseStyle
}

func (a *App) desktopBaseStyleLocked() string {
	_, style := a.desktopAppearanceLocked()
	return style
}

// SaveThemePack creates or updates a user theme from the editor payload.
func (a *App) SaveThemePack(input ThemeSaveInput) (ThemePackView, error) {
	themeMu.Lock()
	defer themeMu.Unlock()

	if a.themeSafeMode() {
		return ThemePackView{}, fmt.Errorf("safe mode cannot save themes")
	}
	m := &ThemePackManifest{
		SchemaVersion:  themePackSchemaVersion,
		ID:             strings.TrimSpace(input.ID),
		Name:           input.Name,
		Author:         input.Author,
		Description:    input.Description,
		License:        input.License,
		BaseStyle:      input.BaseStyle,
		Tokens:         input.Tokens,
		Recipes:        input.Recipes,
		Background:     input.Background,
		TaskBackground: input.TaskBackground,
	}
	// Preserve editor tuning while validation runs before the data URL has been
	// decoded into its final file name. The placeholder is replaced below.
	if !input.ClearBackground && strings.TrimSpace(input.BackgroundDataURL) != "" && m.Background != nil && m.Background.Image == "" {
		m.Background.Image = "background.webp"
	}
	if !input.ClearTaskBackground && strings.TrimSpace(input.TaskBackgroundDataURL) != "" && m.TaskBackground != nil && m.TaskBackground.Image == "" {
		m.TaskBackground.Image = "background-task.webp"
	}
	if err := validateThemePackManifest(m); err != nil {
		return ThemePackView{}, err
	}
	if isReservedThemeID(m.ID) {
		return ThemePackView{}, fmt.Errorf("built-in theme ids are reserved")
	}

	var imageBytes []byte
	keepExistingImage := false

	if input.ClearBackground {
		m.Background = nil
	} else if strings.TrimSpace(input.BackgroundDataURL) != "" {
		name, data, err := decodeDataURLImage(input.BackgroundDataURL)
		if err != nil {
			return ThemePackView{}, err
		}
		imageBytes = data
		if m.Background == nil {
			bg := defaultThemePackBackground()
			m.Background = &bg
		}
		m.Background.Image = name
		// Re-validate after image assignment.
		bg, err := normalizeThemeBackground(m.Background)
		if err != nil {
			return ThemePackView{}, err
		}
		m.Background = bg
	} else if m.Background != nil && m.Background.Image != "" {
		// Keep existing image from library when editing.
		if userThemeExists(m.ID) {
			keepExistingImage = true
		} else {
			return ThemePackView{}, fmt.Errorf("background image data is required for new themes with a background")
		}
	}

	var taskImageBytes []byte
	keepExistingTaskImage := false
	if input.ClearTaskBackground {
		m.TaskBackground = nil
	} else if strings.TrimSpace(input.TaskBackgroundDataURL) != "" {
		name, data, err := decodeDataURLImage(input.TaskBackgroundDataURL)
		if err != nil {
			return ThemePackView{}, err
		}
		taskImageBytes = data
		if m.TaskBackground == nil {
			bg := defaultThemePackTaskBackground()
			m.TaskBackground = &bg
		}
		m.TaskBackground.Image = taskBackgroundImageName(name)
		bg, err := normalizeThemeSceneBackground(m.TaskBackground)
		if err != nil {
			return ThemePackView{}, err
		}
		m.TaskBackground = bg
	} else if m.TaskBackground != nil && m.TaskBackground.Image != "" {
		if userThemeExists(m.ID) {
			keepExistingTaskImage = true
		} else {
			return ThemePackView{}, fmt.Errorf("task background image data is required for new themes with a task background")
		}
	}

	var staging string
	var err error
	var homeSource themeStagingImage
	if keepExistingImage {
		existing, err := resolveThemeImageAbs(m.ID, m.Background.Image)
		if err != nil {
			return ThemePackView{}, err
		}
		homeSource.path = existing
	} else {
		homeSource.bytes = imageBytes
	}
	var taskSource themeStagingImage
	if keepExistingTaskImage {
		existing, err := resolveThemeImageAbs(m.ID, m.TaskBackground.Image)
		if err != nil {
			return ThemePackView{}, err
		}
		taskSource.path = existing
	} else {
		taskSource.bytes = taskImageBytes
	}
	staging, err = writeThemeStaging(m, homeSource.path, homeSource.bytes, taskSource)
	if err != nil {
		return ThemePackView{}, err
	}
	defer os.RemoveAll(staging)

	// Honor Replace: create/import-style saves must not silently overwrite.
	// The editor passes Replace=true when editing an existing theme.
	exists := userThemeExists(m.ID)
	if exists && !input.Replace {
		return ThemePackView{}, fmt.Errorf("theme %q already exists; set replace to overwrite", m.ID)
	}
	if err := publishThemeDir(m.ID, staging, exists && input.Replace); err != nil {
		return ThemePackView{}, err
	}

	if input.Activate {
		st := loadThemeDesktopState()
		st.ActiveThemeID = m.ID
		if err := saveThemeDesktopState(st); err != nil {
			return ThemePackView{}, err
		}
	}
	return a.loadThemeViewLocked(m.ID, input.Activate)
}

// DeleteThemePack removes a user theme. Active theme falls back to none (Graphite path).
func (a *App) DeleteThemePack(id string) error {
	themeMu.Lock()
	defer themeMu.Unlock()

	id = strings.TrimSpace(id)
	if isReservedThemeID(id) {
		return fmt.Errorf("built-in themes cannot be deleted")
	}
	if err := deleteUserTheme(id); err != nil {
		return err
	}
	st := loadThemeDesktopState()
	if st.ActiveThemeID == id {
		st.ActiveThemeID = ""
		return saveThemeDesktopState(st)
	}
	return nil
}

// CopyThemePack duplicates a base, official or user theme into a new user theme id.
func (a *App) CopyThemePack(sourceID, newID, newName string) (ThemePackView, error) {
	themeMu.Lock()
	defer themeMu.Unlock()

	if a.themeSafeMode() {
		return ThemePackView{}, fmt.Errorf("safe mode cannot copy themes")
	}
	sourceID = strings.TrimSpace(sourceID)
	newID = strings.TrimSpace(newID)
	if !themePackIDRe.MatchString(newID) || isReservedThemeID(newID) {
		return ThemePackView{}, fmt.Errorf("invalid new theme id")
	}
	if userThemeExists(newID) {
		return ThemePackView{}, fmt.Errorf("theme %q already exists", newID)
	}

	var m *ThemePackManifest
	var imageBytes []byte
	var taskImageBytes []byte
	if isBuiltinThemeID(sourceID) {
		src := findBuiltinManifest(sourceID)
		if src == nil {
			return ThemePackView{}, fmt.Errorf("unknown source theme")
		}
		cp := *src
		m = &cp
	} else if ot := findOfficialTheme(sourceID); ot != nil {
		// Copying an official theme embeds a private copy of its background so the
		// duplicate becomes an ordinary editable user theme.
		cp := ot.manifest
		m = &cp
		data, _, err := readOfficialAsset(sourceID, cp.Background.Image)
		if err != nil {
			return ThemePackView{}, fmt.Errorf("read official background: %w", err)
		}
		imageBytes = data
	} else {
		src, err := loadUserThemeManifest(sourceID)
		if err != nil {
			return ThemePackView{}, err
		}
		m = src
		if m.Background != nil && m.Background.Image != "" {
			p, err := resolveThemeImageAbs(sourceID, m.Background.Image)
			if err != nil {
				return ThemePackView{}, err
			}
			imageBytes, err = os.ReadFile(p)
			if err != nil {
				return ThemePackView{}, err
			}
		}
		if m.TaskBackground != nil && m.TaskBackground.Image != "" {
			p, err := resolveThemeImageAbs(sourceID, m.TaskBackground.Image)
			if err != nil {
				return ThemePackView{}, err
			}
			taskImageBytes, err = os.ReadFile(p)
			if err != nil {
				return ThemePackView{}, err
			}
		}
	}
	m.ID = newID
	if strings.TrimSpace(newName) != "" {
		m.Name = strings.TrimSpace(newName)
	} else {
		m.Name = m.Name + " Copy"
	}
	if err := validateThemePackManifest(m); err != nil {
		return ThemePackView{}, err
	}
	staging, err := writeThemeStaging(m, "", imageBytes, themeStagingImage{bytes: taskImageBytes})
	if err != nil {
		return ThemePackView{}, err
	}
	defer os.RemoveAll(staging)
	if err := publishThemeDir(newID, staging, false); err != nil {
		return ThemePackView{}, err
	}
	return a.loadThemeViewLocked(newID, false)
}

// ImportThemePack opens a file dialog (or uses sourcePath in tests) and imports a ZIP.
// When replace is false and the id exists, the extract is kept as a pending import
// (NeedsReplace=true) so a subsequent ImportThemePack("", true) publishes without
// re-opening the file dialog. Host paths never leave the Go side.
func (a *App) ImportThemePack(sourcePath string, replace bool) (ThemeImportResult, error) {
	themeMu.Lock()
	defer themeMu.Unlock()

	if a.themeSafeMode() {
		return ThemeImportResult{}, fmt.Errorf("safe mode cannot import themes")
	}

	// Confirm a previously staged conflict without re-picking a file.
	path := strings.TrimSpace(sourcePath)
	if path == "" && replace {
		if pending := takePendingThemeImport(); pending != nil {
			defer os.RemoveAll(pending.staging)
			if err := publishThemeDir(pending.id, pending.staging, true); err != nil {
				return ThemeImportResult{}, err
			}
			pack, err := a.loadThemeViewLocked(pending.id, false)
			if err != nil {
				return ThemeImportResult{}, err
			}
			return ThemeImportResult{Pack: pack, Replaced: true}, nil
		}
		// Fall through to dialog/path if nothing was pending (e.g. tests pass path).
	}

	if path == "" {
		if a.ctx == nil {
			return ThemeImportResult{}, fmt.Errorf("no theme package selected")
		}
		picked, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
			Title: "Import Reasonix Theme",
			Filters: []runtime.FileFilter{
				{DisplayName: "Reasonix Theme (*.reasonix-theme)", Pattern: "*.reasonix-theme"},
				{DisplayName: "ZIP (*.zip)", Pattern: "*.zip"},
			},
		})
		if err != nil {
			return ThemeImportResult{}, err
		}
		path = picked
	}
	if path == "" {
		return ThemeImportResult{}, nil
	}

	m, staging, err := importThemePackZIP(path)
	if err != nil {
		return ThemeImportResult{}, err
	}

	exists := userThemeExists(m.ID)
	if exists && !replace {
		// Stage for confirmation — do not delete staging; pending owns it.
		pack := manifestToView(m, themeKindUser, false, "", "")
		pendingID := setPendingThemeImport(m.ID, staging, pack)
		return ThemeImportResult{
			Pack:         pack,
			NeedsReplace: true,
			PendingID:    pendingID,
		}, nil
	}
	defer os.RemoveAll(staging)
	clearPendingThemeImport()

	if err := publishThemeDir(m.ID, staging, replace || exists); err != nil {
		return ThemeImportResult{}, err
	}
	pack, err := a.loadThemeViewLocked(m.ID, false)
	if err != nil {
		return ThemeImportResult{}, err
	}
	return ThemeImportResult{Pack: pack, Replaced: exists && replace}, nil
}

// ExportThemePack writes the theme to a user-selected destination.
func (a *App) ExportThemePack(id, destPath string) (string, error) {
	themeMu.Lock()
	defer themeMu.Unlock()

	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("theme id is required")
	}
	path := strings.TrimSpace(destPath)
	if path == "" {
		if a.ctx == nil {
			return "", fmt.Errorf("no export path")
		}
		defaultName := id + themePackExt
		picked, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
			Title:           "Export Reasonix Theme",
			DefaultFilename: defaultName,
			Filters: []runtime.FileFilter{
				{DisplayName: "Reasonix Theme (*.reasonix-theme)", Pattern: "*.reasonix-theme"},
			},
		})
		if err != nil {
			return "", err
		}
		path = picked
	}
	if path == "" {
		return "", nil
	}
	if err := exportThemePackZIP(id, path); err != nil {
		return "", err
	}
	if !strings.HasSuffix(strings.ToLower(path), themePackExt) {
		path += themePackExt
	}
	return path, nil
}

// PickThemeBackground opens a native file dialog for a local background image.
// Returns a data URL for the editor preview — never exposes the absolute path.
func (a *App) PickThemeBackground() (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("file dialog unavailable")
	}
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Choose Theme Background",
		Filters: []runtime.FileFilter{
			{DisplayName: "Images (*.png;*.jpg;*.jpeg;*.webp)", Pattern: "*.png;*.jpg;*.jpeg;*.webp"},
		},
	})
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", nil
	}
	if err := validateThemeImageFile(path); err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if int64(len(data)) > themePackMaxImageBytes {
		return "", fmt.Errorf("background image exceeds %d bytes", themePackMaxImageBytes)
	}
	mime := themeImageMIMEFromName(filepath.Base(path))
	// Return as data URL so the frontend never needs the host path.
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}
