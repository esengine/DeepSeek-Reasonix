// Package theme reads installed theme packs. A pack is data — named colours
// for a light and a dark scheme — with no code, stylesheet, or script in it,
// so anything that can write a JSON file can author one and no frontend has to
// trust what it loads.
package theme

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"reasonix/internal/contract/config"
)

// The packs that ship: manifest, background, and preview for each.
//
//go:embed builtin
var builtin embed.FS

const (
	manifestName = "theme.json"
	dirName      = "themes"
	// schemaVersion is the only shape this reader understands. A pack that
	// declares a newer one is skipped rather than half-read: a theme is
	// cosmetic, and guessing at unknown fields is worse than staying default.
	schemaVersion = 1
)

// Pack is one installed theme. Tokens are keyed by scheme ("light"/"dark")
// and then by the pack's own colour names; a frontend maps those onto its own
// variables, because only it knows what each surface in its layout is called.
type Pack struct {
	ID          string                       `json:"id"`
	Name        string                       `json:"name"`
	Author      string                       `json:"author,omitempty"`
	Description string                       `json:"description,omitempty"`
	Tokens      map[string]map[string]string `json:"tokens"`
	Recipes     *Recipes                     `json:"recipes,omitempty"`
	Background  *Background                  `json:"background,omitempty"`
	Sky         *Sky                         `json:"sky,omitempty"`
	HasPreview  bool                         `json:"hasPreview,omitempty"`
	// Warnings names the tokens that were dropped and why. The pack still
	// loads: one bad value must not cost the author every good one.
	Warnings []string `json:"warnings,omitempty"`
}

// Recipes is what a pack says about shape and rhythm rather than colour: two
// tiers the schema publishes and no token carries. A value outside them is
// dropped and reported, the way a token that fails its grammar is.
type Recipes struct {
	Density string `json:"density,omitempty"`
	Corners string `json:"corners,omitempty"`
}

// Background is how a pack's image wants to be placed, not just that it has
// one. Focus is the point that survives cropping — a portrait centred at 50%
// loses its subject on a wide window. The two opacities are the same image at
// rest and at work: a picture that is right behind an idle home screen is in
// the way of a transcript being read.
type Background struct {
	// Image is set when the pack ships one; the bytes come from Asset.
	Image bool `json:"image"`
	// FocusX/FocusY are 0..1 within the image.
	FocusX float64 `json:"focusX"`
	FocusY float64 `json:"focusY"`
	// SafeArea names the side the content sits on, so the image's own subject
	// can be kept away from it: left | center | right.
	SafeArea string `json:"safeArea,omitempty"`
	// HomeOpacity applies when nothing is running, TaskOpacity while a turn is.
	HomeOpacity     float64 `json:"homeOpacity"`
	TaskOpacity     float64 `json:"taskOpacity"`
	OverlayStrength float64 `json:"overlayStrength"`
}

// Sky is a live backdrop rather than a picture: drifting cloud, a sun, and a
// few light shafts, drawn by the window. Its colours ride here rather than in
// Tokens because a pack without a sky has no use for them, and a token every
// pack must ignore reads as broken. Values are checked the same way tokens are.
type Sky struct {
	// Ray tints the sun and the shafts; Cloud and CloudLit are the two cloud
	// tones, the second used for the third of puffs that catch the light.
	Ray      string  `json:"ray,omitempty"`
	Cloud    string  `json:"cloud,omitempty"`
	CloudLit string  `json:"cloudLit,omitempty"`
	RayAlpha float64 `json:"rayAlpha"`
	// CloudAlpha is the whole cloud layer's weight, kept low: it sits under a
	// transcript and must never compete with text.
	CloudAlpha float64 `json:"cloudAlpha"`
}

type manifest struct {
	SchemaVersion int                          `json:"schemaVersion"`
	ID            string                       `json:"id"`
	Name          string                       `json:"name"`
	Author        string                       `json:"author"`
	Description   string                       `json:"description"`
	Tokens        map[string]map[string]string `json:"tokens"`
	Recipes       *manifestRecipes             `json:"recipes"`
	Background    *manifestBackground          `json:"background"`
	Sky           *manifestSky                 `json:"sky"`
}

type manifestRecipes struct {
	Density string `json:"density"`
	Corners string `json:"corners"`
}

type manifestSky struct {
	Ray        string   `json:"ray"`
	Cloud      string   `json:"cloud"`
	CloudLit   string   `json:"cloudLit"`
	RayAlpha   *float64 `json:"rayAlpha"`
	CloudAlpha *float64 `json:"cloudAlpha"`
}

type manifestBackground struct {
	Image           string   `json:"image"`
	FocusX          *float64 `json:"focusX"`
	FocusY          *float64 `json:"focusY"`
	SafeArea        string   `json:"safeArea"`
	HomeOpacity     *float64 `json:"homeOpacity"`
	TaskOpacity     *float64 `json:"taskOpacity"`
	OverlayStrength *float64 `json:"overlayStrength"`
}

// Dir is where user-installed packs live: one directory per pack, each with a
// theme.json. It is under the memory root so a pack an agent writes lands
// beside the rest of what the user's agent has authored.
func Dir() string {
	return filepath.Join(config.MemoryUserDir(), dirName)
}

// List returns every readable pack, shipped and installed, sorted by id. A
// missing directory is not an error — it means nothing is installed. An
// unreadable or malformed pack is skipped so one bad file cannot hide the
// rest. An installed pack shadows a shipped one of the same id: the user's
// copy of a palette is the one they meant.
func List() []Pack {
	byID := map[string]Pack{}
	for _, pack := range listBuiltin() {
		byID[pack.ID] = pack
	}
	if entries, err := os.ReadDir(Dir()); err == nil {
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			pack, err := load(filepath.Join(Dir(), e.Name(), manifestName), e.Name())
			if err != nil {
				continue
			}
			byID[pack.ID] = pack
		}
	}
	for _, p := range pluginPacks() {
		if pack, err := load(filepath.Join(p.dir, manifestName), p.id); err == nil {
			byID[pack.ID] = pack
		}
	}
	out := make([]Pack, 0, len(byID))
	for _, pack := range byID {
		out = append(out, pack)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func listBuiltin() []Pack {
	entries, err := fs.ReadDir(builtin, "builtin")
	if err != nil {
		return nil
	}
	var out []Pack
	for _, e := range entries {
		id := strings.TrimSuffix(e.Name(), ".json")
		raw, err := builtin.ReadFile("builtin/" + e.Name())
		if err != nil {
			continue
		}
		pack, err := decode(raw, id)
		if err != nil {
			continue
		}
		out = append(out, pack)
	}
	return out
}

// Load returns one pack by id.
func Load(id string) (Pack, error) {
	id = strings.TrimSpace(id)
	if err := validID(id); err != nil {
		return Pack{}, err
	}
	if isPluginID(id) {
		dir, ok := pluginDir(id)
		if !ok {
			return Pack{}, fmt.Errorf("theme: no enabled plugin contributes %q", id)
		}
		return load(filepath.Join(dir, manifestName), id)
	}
	if pack, err := load(filepath.Join(Dir(), id, manifestName), id); err == nil {
		return pack, nil
	}
	raw, err := builtin.ReadFile("builtin/" + id + ".json")
	if err != nil {
		return Pack{}, fmt.Errorf("theme: no pack %q", id)
	}
	return decode(raw, id)
}

func load(path, dirID string) (Pack, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Pack{}, err
	}
	return decode(raw, dirID)
}

func decode(raw []byte, dirID string) (Pack, error) {
	pack, m, err := decodeManifest(raw, dirID)
	if err != nil {
		return Pack{}, err
	}
	pack.Background = backgroundOf(m.Background, hasAsset(dirID, assetBackground))
	pack.Sky = skyOf(m.Sky)
	pack.HasPreview = hasAsset(dirID, assetPreview)
	return pack, nil
}

func decodeManifest(raw []byte, dirID string) (Pack, manifest, error) {
	id := dirID
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return Pack{}, manifest{}, fmt.Errorf("theme %s: %w", dirID, err)
	}
	if m.SchemaVersion != schemaVersion {
		return Pack{}, manifest{}, fmt.Errorf("theme %s: unsupported schemaVersion %d", dirID, m.SchemaVersion)
	}
	// The directory name is the address the user activates by, so it wins over
	// whatever the manifest claims its id is.
	name := strings.TrimSpace(m.Name)
	if name == "" {
		name = id
	}
	tokens := map[string]map[string]string{}
	var warnings []string
	for _, scheme := range []string{"light", "dark"} {
		if len(m.Tokens[scheme]) == 0 {
			return Pack{}, manifest{}, fmt.Errorf("theme %s: no %s tokens", id, scheme)
		}
		copied := make(map[string]string, len(m.Tokens[scheme]))
		for k, v := range m.Tokens[scheme] {
			if validToken(k, v) {
				copied[k] = v
				continue
			}
			// A dropped token is reported rather than silently ignored: the
			// author is the only one who can fix it, and a pack that half
			// applies looks like the app is broken rather than the pack.
			warnings = append(warnings, dropReason(scheme, k, v))
		}
		if len(copied) == 0 {
			return Pack{}, manifest{}, fmt.Errorf("theme %s: no usable %s tokens", id, scheme)
		}
		tokens[scheme] = copied
	}
	recipes := recipesOf(m.Recipes, &warnings)
	// Map iteration order would otherwise make the same pack report its
	// problems in a different order on every read.
	slices.Sort(warnings)
	pack := Pack{ID: id, Name: name, Author: strings.TrimSpace(m.Author), Description: strings.TrimSpace(m.Description), Tokens: tokens, Recipes: recipes, Warnings: warnings}
	return pack, m, nil
}

// recipesOf keeps the tiers the schema publishes and reports anything else. A
// recipe is not interpolated anywhere, but a value the frontend cannot spell
// leaves the pack reading as one that declared nothing.
func recipesOf(m *manifestRecipes, warnings *[]string) *Recipes {
	if m == nil {
		return nil
	}
	out := Recipes{
		Density: recipeTier("density", m.Density, densityTiers, warnings),
		Corners: recipeTier("corners", m.Corners, cornerTiers, warnings),
	}
	if out.Density == "" && out.Corners == "" {
		return nil
	}
	return &out
}

// The schema's enumerations, read here rather than trusted: the schema reaches
// only a pack somebody validated against it, and a manifest is hand-written.
var (
	densityTiers = []string{"compact", "comfortable"}
	cornerTiers  = []string{"square", "soft", "round"}
)

func recipeTier(field, value string, tiers []string, warnings *[]string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if slices.Contains(tiers, value) {
		return value
	}
	*warnings = append(*warnings, fmt.Sprintf("recipes.%s: %q is not one of %s", field, value, strings.Join(tiers, "|")))
	return ""
}

// colourOr passes a value through the same check a token colour gets: every
// one of these is interpolated into a stylesheet, so none of them may be
// trusted because it arrived in a different section of the manifest.
func colourOr(v, fallback string) string {
	v = strings.TrimSpace(v)
	if v != "" && isColour(v) {
		return v
	}
	return fallback
}

// skyOf drops a colour the validator refuses rather than the whole sky: an
// author who mistyped one tint still gets the layer, in the default tone.
func skyOf(m *manifestSky) *Sky {
	if m == nil {
		return nil
	}
	out := &Sky{RayAlpha: 0.85, CloudAlpha: 0.4}
	out.Ray = colourOr(m.Ray, "")
	out.Cloud = colourOr(m.Cloud, "")
	out.CloudLit = colourOr(m.CloudLit, "")
	if m.RayAlpha != nil {
		out.RayAlpha = clamp01(*m.RayAlpha)
	}
	if m.CloudAlpha != nil {
		out.CloudAlpha = clamp01(*m.CloudAlpha)
	}
	return out
}

// backgroundOf keeps the placement even when the image is missing: a pack that
// declares a focus and two opacities has said how it wants to be shown, and a
// user dropping their own background.webp next to the manifest should get that
// placement rather than a centred default.
func backgroundOf(m *manifestBackground, image bool) *Background {
	if m == nil && !image {
		return nil
	}
	out := &Background{Image: image, FocusX: 0.5, FocusY: 0.5, HomeOpacity: 1, TaskOpacity: 0.2, OverlayStrength: 0.65}
	if m == nil {
		return out
	}
	if m.FocusX != nil {
		out.FocusX = clamp01(*m.FocusX)
	}
	if m.FocusY != nil {
		out.FocusY = clamp01(*m.FocusY)
	}
	switch m.SafeArea {
	case "left", "center", "right":
		out.SafeArea = m.SafeArea
	}
	if m.HomeOpacity != nil {
		out.HomeOpacity = clamp01(*m.HomeOpacity)
	}
	if m.TaskOpacity != nil {
		out.TaskOpacity = clamp01(*m.TaskOpacity)
	}
	if m.OverlayStrength != nil {
		out.OverlayStrength = clamp01(*m.OverlayStrength)
	}
	return out
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// isColour keeps a token only when it is a plain hex colour. The value is
// interpolated straight into a stylesheet, so anything that could carry a
// url(), an expression, or a closing brace is dropped rather than escaped.
func isColour(v string) bool {
	v = strings.TrimSpace(v)
	if len(v) != 4 && len(v) != 7 && len(v) != 9 {
		return false
	}
	if v[0] != '#' {
		return false
	}
	for _, r := range v[1:] {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return false
		}
	}
	return true
}
