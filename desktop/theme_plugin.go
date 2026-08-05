package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/pluginpkg"
)

// Plugin themes are .reasonix-theme packs contributed by ENABLED installed
// plugins (Manifest v1 contributes.themes globs). They are read-only: they are
// never copied into the user theme library, never staged, and never mutated —
// every read goes straight to the ZIP inside the plugin root. The external id
// is plugin:<pluginName>:<themeID>; the pack manifest's own id continues to
// obey themePackIDRe and the plugin: prefix is added/stripped only at this
// seam.

const (
	themeKindPlugin     = "plugin"
	pluginThemeIDPrefix = "plugin:"
)

// isPluginThemeID reports whether an external theme id carries the plugin
// prefix. It is intentionally prefix-only (cheap, allocation-free) so the
// fallback contract can recognize plugin pointers even when the remainder is
// malformed; parsePluginThemeID does the strict validation.
func isPluginThemeID(id string) bool {
	return strings.HasPrefix(strings.TrimSpace(id), pluginThemeIDPrefix)
}

// parsePluginThemeID splits plugin:<pluginName>:<themeID>. Plugin names cannot
// contain ":" (pluginpkg.validName) and theme ids cannot either
// (themePackIDRe), so a single Cut on the first colon is unambiguous.
func parsePluginThemeID(id string) (pluginName, themeID string, ok bool) {
	rest, found := strings.CutPrefix(strings.TrimSpace(id), pluginThemeIDPrefix)
	if !found {
		return "", "", false
	}
	pluginName, themeID, found = strings.Cut(rest, ":")
	if !found {
		return "", "", false
	}
	if !pluginpkg.IsValidName(pluginName) || !themePackIDRe.MatchString(themeID) {
		return "", "", false
	}
	return pluginName, themeID, true
}

// pluginTheme is one validated theme pack discovered inside an enabled plugin.
type pluginTheme struct {
	id         string // plugin:<pluginName>:<themeID>
	pluginName string // installed plugin name (for view badging)
	themeID    string // the pack manifest's own id
	path       string // absolute path of the .reasonix-theme ZIP
	manifest   *ThemePackManifest
	digests    map[string]string // lowercase scene image name -> content digest
	warnings   []string          // non-fatal discovery issues of the same plugin
}

// discoverPluginThemes resolves the contributes.themes globs of every ENABLED
// installed plugin and validates each matched pack with the same schema v2
// validator and ZIP container rules the user-theme import path uses. Invalid
// files are skipped and reported through the returned warnings — never fatal.
// Disabled plugins are skipped by pluginpkg.LoadInstalled; uninstalling a
// plugin simply makes its themes disappear from the result.
func discoverPluginThemes() ([]pluginTheme, []string) {
	pkgs, warnings := pluginpkg.LoadInstalled(config.ReasonixHomeDir())
	var out []pluginTheme
	seen := map[string]bool{}
	for i := range pkgs {
		pluginName := pkgs[i].Installed.Name
		pluginWarningsStart := len(warnings)
		// Parse-level theme issues (missing paths, unmatched globs) computed by
		// pluginpkg ride along too; other capability warnings stay with the
		// plugin views that already show them.
		for _, w := range pkgs[i].Warnings {
			if strings.HasPrefix(w, "theme") {
				warnings = append(warnings, fmt.Sprintf("plugin %s: %s", pluginName, w))
			}
		}
		var mine []pluginTheme
		for _, ref := range pkgs[i].Package.Inventory().Themes {
			m, images, err := loadPluginThemeZip(ref.Path)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("plugin %s: theme %s skipped: %v", pluginName, filepath.Base(ref.Path), err))
				continue
			}
			id := pluginThemeIDPrefix + pluginName + ":" + m.ID
			if seen[id] {
				warnings = append(warnings, fmt.Sprintf("plugin %s: theme %s skipped: duplicate theme id %q", pluginName, filepath.Base(ref.Path), m.ID))
				continue
			}
			seen[id] = true
			digests := make(map[string]string, len(images))
			for key, data := range images {
				digests[key] = themeDataDigest(data)
			}
			mine = append(mine, pluginTheme{
				id:         id,
				pluginName: pluginName,
				themeID:    m.ID,
				path:       ref.Path,
				manifest:   m,
				digests:    digests,
			})
		}
		// Surface the plugin's skipped-file warnings on each of its surviving
		// views (the same per-item pattern PluginView.Warnings uses).
		for j := range mine {
			mine[j].warnings = append([]string(nil), warnings[pluginWarningsStart:]...)
		}
		out = append(out, mine...)
	}
	return out, warnings
}

// findPluginTheme resolves one enabled plugin's contributed theme, or nil when
// the plugin is missing, disabled, uninstalled, or the file became invalid.
func findPluginTheme(pluginName, themeID string) *pluginTheme {
	themes, _ := discoverPluginThemes()
	for i := range themes {
		if themes[i].pluginName == pluginName && themes[i].themeID == themeID {
			return &themes[i]
		}
	}
	return nil
}

// loadPluginThemeZip opens a contributed theme pack read-only and returns the
// validated manifest plus the raw bytes of its declared scene images. The ZIP
// container rules are the shared ones from scanThemeZipEntries; nothing is
// extracted to disk.
func loadPluginThemeZip(path string) (*ThemePackManifest, map[string][]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, nil, fmt.Errorf("theme package must not be a symlink")
	}
	if !info.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("theme package must be a regular file")
	}
	if info.Size() > themePackMaxZipBytes {
		return nil, nil, fmt.Errorf("theme package exceeds %d bytes", themePackMaxZipBytes)
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	// Re-check size after open (TOCTOU).
	fi, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	if fi.Size() > themePackMaxZipBytes {
		return nil, nil, fmt.Errorf("theme package exceeds %d bytes", themePackMaxZipBytes)
	}

	zr, err := zip.NewReader(f, fi.Size())
	if err != nil {
		return nil, nil, fmt.Errorf("invalid theme ZIP: %w", err)
	}
	manifestEntry, imageEntries, err := scanThemeZipEntries(zr)
	if err != nil {
		return nil, nil, err
	}
	raw, err := readZipFileLimited(manifestEntry, themePackMaxManifest)
	if err != nil {
		return nil, nil, err
	}
	m, err := parseThemePackManifest(raw)
	if err != nil {
		return nil, nil, err
	}
	if err := checkThemeZipImages(m, imageEntries); err != nil {
		return nil, nil, err
	}
	images := make(map[string][]byte, len(imageEntries))
	for key, zf := range imageEntries {
		data, err := readZipFileLimited(zf, themePackMaxImageBytes)
		if err != nil {
			return nil, nil, err
		}
		images[key] = data
	}
	return m, images, nil
}

// pluginThemeView renders the frontend-safe view of a plugin theme. Kind is
// "plugin", the plugin name rides along for badging, and the pack is marked
// read-only (Builtin=false, no save/delete/rename paths accept the id).
func pluginThemeView(pt pluginTheme, active bool) ThemePackView {
	m := pt.manifest
	bgURL := ""
	if m.Background != nil {
		bgURL = pluginThemeBackgroundURL(pt, m.Background.Image)
	}
	taskURL := ""
	if m.TaskBackground != nil {
		taskURL = pluginThemeBackgroundURL(pt, m.TaskBackground.Image)
	}
	v := manifestToView(m, themeKindPlugin, active, bgURL, "", taskURL)
	// The external id carries the plugin: prefix; the manifest's own id stays
	// governed by themePackIDRe inside the pack.
	v.ID = pt.id
	v.PluginName = pt.pluginName
	v.Warnings = append([]string(nil), pt.warnings...)
	return v
}

// pluginThemeBackgroundURL builds the content-addressed asset URL for a scene
// image that stays inside the plugin ZIP. The digest was computed when the
// theme was discovered; the serve path re-verifies it on every request.
func pluginThemeBackgroundURL(pt pluginTheme, imageName string) string {
	if imageName == "" {
		return ""
	}
	digest := pt.digests[strings.ToLower(imageName)]
	if digest == "" {
		return ""
	}
	return themeAssetURLPrefix + pt.id + "/" + digest + "/" + filepath.Base(imageName)
}

// themeDataDigest is the in-memory counterpart of themeFileDigest: the same
// truncated SHA-256 content identity used by every theme asset URL.
func themeDataDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])[:16]
}

// servePluginThemeAsset serves a scene image straight out of the plugin ZIP.
// It mirrors serveOfficialThemeAsset: the theme must still resolve from an
// enabled plugin, the filename must be manifest-declared, the URL digest is
// re-verified against the current bytes, and the MIME sniff must agree with
// the declared extension.
func servePluginThemeAsset(w http.ResponseWriter, r *http.Request, pluginName, themeID, digest, filename string) {
	pt := findPluginTheme(pluginName, themeID)
	if pt == nil {
		http.NotFound(w, r)
		return
	}
	declared := pt.manifest.Background != nil && pt.manifest.Background.Image == filename ||
		pt.manifest.TaskBackground != nil && pt.manifest.TaskBackground.Image == filename
	if !declared {
		http.NotFound(w, r)
		return
	}
	_, images, err := loadPluginThemeZip(pt.path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	data := images[strings.ToLower(filename)]
	if len(data) == 0 {
		http.NotFound(w, r)
		return
	}
	// Re-validate file identity against the digest embedded in the URL.
	if !strings.EqualFold(themeDataDigest(data), digest) {
		http.NotFound(w, r)
		return
	}
	head := data
	if len(head) > 512 {
		head = head[:512]
	}
	if sniffThemeImageMIME(head, filename) != themeImageMIMEFromName(filename) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", themeImageMIMEFromName(filename))
	w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": filename}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeContent(w, r, filename, time.Time{}, bytes.NewReader(data))
}

// errPluginThemeReadOnly is the shared guard message for bound methods that
// must never mutate a plugin theme.
func errPluginThemeReadOnly(id, op string) error {
	return fmt.Errorf("plugin theme %q is read-only and cannot be %s; it is managed by its plugin", strings.TrimSpace(id), op)
}
