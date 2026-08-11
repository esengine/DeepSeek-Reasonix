package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"sort"
	"sync"

	"embed"
)

// Official themes are read-only, MIT-licensed Reasonix assets embedded into the
// desktop binary. They reuse the Theme Pack V1 validator and the same
// content-addressed asset route as user themes, but read from embed.FS instead
// of the user library and are served with immutable caching.
//
// Fail-closed contract: an invalid official entry is dropped from the registry
// (never listed, never served) without affecting startup; the build-time test
// in theme_official_test.go fails long before that can ship.

//go:embed themes/official
var officialThemesFS embed.FS

const (
	officialThemeDirName     = "themes/official"
	officialPreviewName      = "preview.webp"
	officialBackgroundWidth  = 2560
	officialBackgroundHeight = 1440
	officialPreviewWidth     = 480
	officialPreviewHeight    = 270
	officialMaxBackground    = 2359296  // 2.25 MiB per background
	officialMaxPreview       = 122880   // 120 KiB per thumbnail
	officialMaxTotalBytes    = 18 << 20 // 18 MiB across all backgrounds
	officialExpectedCount    = 8        // release gate: all eight themes
	themeKindBase            = "base"
	themeKindOfficial        = "official"
	themeKindUser            = "user"
)

// Fixed gallery / list order (not alphabetical).
var officialThemeOrderFixed = []string{
	"official-rose-dawn",
	"official-fortune-forge",
	"official-crimson-horizon",
	"official-sage-breeze",
	"official-spark-notebook",
	"official-violet-starlight",
	"official-cyan-stage",
	"official-noir-gold",
}

type officialTheme struct {
	manifest      ThemePackManifest
	bgDigest      string
	previewDigest string
	bgSize        int64
}

var (
	officialOnce     sync.Once
	officialRegistry map[string]*officialTheme
	officialOrder    []string
	officialLoadErr  error
)

// loadOfficialRegistry parses and validates every embedded official theme once.
// Invalid entries are skipped (fail-closed); the first error is remembered for
// diagnostics and tests.
func loadOfficialRegistry() {
	officialRegistry = map[string]*officialTheme{}
	officialOrder = nil
	entries, err := officialThemesFS.ReadDir(officialThemeDirName)
	if err != nil {
		officialLoadErr = fmt.Errorf("read official themes: %w", err)
		return
	}
	var total int64
	var firstErr error
	remember := func(err error) {
		if firstErr == nil {
			firstErr = err
		}
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id := e.Name()
		ot, err := loadOfficialTheme(id)
		if err != nil {
			remember(fmt.Errorf("official theme %q: %w", id, err))
			continue
		}
		if _, dup := officialRegistry[ot.manifest.ID]; dup {
			remember(fmt.Errorf("official theme id %q is duplicated", ot.manifest.ID))
			continue
		}
		officialRegistry[ot.manifest.ID] = ot
		officialOrder = append(officialOrder, ot.manifest.ID)
		total += ot.bgSize
	}
	if total > officialMaxTotalBytes {
		remember(fmt.Errorf("official backgrounds total %d bytes exceeds %d", total, officialMaxTotalBytes))
	}
	// Prefer the explicit product order; append any unexpected extras last.
	ordered := make([]string, 0, len(officialRegistry))
	seen := map[string]bool{}
	for _, id := range officialThemeOrderFixed {
		if _, ok := officialRegistry[id]; ok {
			ordered = append(ordered, id)
			seen[id] = true
		}
	}
	var extras []string
	for id := range officialRegistry {
		if !seen[id] {
			extras = append(extras, id)
		}
	}
	sort.Strings(extras)
	officialOrder = append(ordered, extras...)
	officialLoadErr = firstErr
}

func loadOfficialTheme(dirID string) (*officialTheme, error) {
	if !themePackIDRe.MatchString(dirID) {
		return nil, fmt.Errorf("invalid directory name")
	}
	base := officialThemeDirName + "/" + dirID
	raw, err := officialThemesFS.ReadFile(base + "/" + themePackManifestName)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	m, err := parseThemePackManifest(raw)
	if err != nil {
		return nil, err
	}
	if m.ID != dirID {
		return nil, fmt.Errorf("manifest id %q does not match directory %q", m.ID, dirID)
	}
	if isBuiltinThemeID(m.ID) {
		return nil, fmt.Errorf("official theme id %q collides with a base style", m.ID)
	}
	if m.Background == nil || m.Background.Image == "" {
		return nil, fmt.Errorf("official themes require a background image")
	}
	if m.Background.Image != "background.webp" {
		return nil, fmt.Errorf("official background must be background.webp, got %q", m.Background.Image)
	}

	bg, err := officialThemesFS.ReadFile(base + "/" + m.Background.Image)
	if err != nil {
		return nil, fmt.Errorf("read background: %w", err)
	}
	if err := validateOfficialImage(bg, m.Background.Image, officialBackgroundWidth, officialBackgroundHeight, officialMaxBackground); err != nil {
		return nil, fmt.Errorf("background: %w", err)
	}
	preview, err := officialThemesFS.ReadFile(base + "/" + officialPreviewName)
	if err != nil {
		return nil, fmt.Errorf("read preview: %w", err)
	}
	if err := validateOfficialImage(preview, officialPreviewName, officialPreviewWidth, officialPreviewHeight, officialMaxPreview); err != nil {
		return nil, fmt.Errorf("preview: %w", err)
	}
	return &officialTheme{
		manifest:      *m,
		bgDigest:      themeBytesDigest(bg),
		previewDigest: themeBytesDigest(preview),
		bgSize:        int64(len(bg)),
	}, nil
}

func validateOfficialImage(data []byte, name string, wantW, wantH int, maxBytes int64) error {
	if int64(len(data)) > maxBytes {
		return fmt.Errorf("%s exceeds %d bytes", name, maxBytes)
	}
	head := data
	if len(head) > 512 {
		head = head[:512]
	}
	mime := sniffThemeImageMIME(head, name)
	if mime != "image/webp" {
		return fmt.Errorf("%s must be WebP", name)
	}
	cfg, format, err := decodeThemeImageConfig(bytesReader(data), mime)
	if err != nil {
		return fmt.Errorf("decode %s: %w", name, err)
	}
	if format != "webp" {
		return fmt.Errorf("%s must be WebP", name)
	}
	if cfg.Width != wantW || cfg.Height != wantH {
		return fmt.Errorf("%s must be %d×%d, got %d×%d", name, wantW, wantH, cfg.Width, cfg.Height)
	}
	return nil
}

func bytesReader(b []byte) io.Reader {
	return bytes.NewReader(b)
}

func themeBytesDigest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:16]
}

func officialThemes() []*officialTheme {
	officialOnce.Do(loadOfficialRegistry)
	out := make([]*officialTheme, 0, len(officialOrder))
	for _, id := range officialOrder {
		out = append(out, officialRegistry[id])
	}
	return out
}

func findOfficialTheme(id string) *officialTheme {
	officialOnce.Do(loadOfficialRegistry)
	return officialRegistry[id]
}

func isOfficialThemeID(id string) bool {
	return findOfficialTheme(id) != nil
}

// isReservedThemeID covers both the six base styles and the eight official
// themes: user saves, imports, copies, overwrites and deletes must refuse them.
func isReservedThemeID(id string) bool {
	return isBuiltinThemeID(id) || isOfficialThemeID(id)
}

// officialAssetURL builds the content-addressed URL for an embedded asset.
// Only the manifest-declared background and the fixed preview name are served.
func officialAssetURL(id, filename string) string {
	ot := findOfficialTheme(id)
	if ot == nil {
		return ""
	}
	switch filename {
	case ot.manifest.Background.Image:
		return themeAssetURLPrefix + id + "/" + ot.bgDigest + "/" + filename
	case officialPreviewName:
		return themeAssetURLPrefix + id + "/" + ot.previewDigest + "/" + filename
	default:
		return ""
	}
}

// readOfficialAsset returns embedded asset bytes after digest verification.
func readOfficialAsset(id, filename string) ([]byte, string, error) {
	ot := findOfficialTheme(id)
	if ot == nil {
		return nil, "", fmt.Errorf("unknown official theme")
	}
	var name, digest string
	switch filename {
	case ot.manifest.Background.Image:
		name, digest = ot.manifest.Background.Image, ot.bgDigest
	case officialPreviewName:
		name, digest = officialPreviewName, ot.previewDigest
	default:
		return nil, "", fmt.Errorf("asset not declared")
	}
	data, err := fs.ReadFile(officialThemesFS, officialThemeDirName+"/"+id+"/"+name)
	if err != nil {
		return nil, "", err
	}
	if got := themeBytesDigest(data); got != digest {
		return nil, "", fmt.Errorf("digest mismatch")
	}
	return data, name, nil
}

// validateOfficialThemes gates the build: every embedded entry must parse and
// pass the V1 validator plus image budgets, and the release set must be complete.
func validateOfficialThemes() error {
	officialOnce.Do(loadOfficialRegistry)
	if officialLoadErr != nil {
		return officialLoadErr
	}
	if len(officialOrder) != officialExpectedCount {
		return fmt.Errorf("expected %d official themes, found %d", officialExpectedCount, len(officialOrder))
	}
	return nil
}

// resetOfficialRegistryForTest clears the cached registry (tests only).
func resetOfficialRegistryForTest() {
	officialOnce = sync.Once{}
	officialRegistry = nil
	officialOrder = nil
	officialLoadErr = nil
}
