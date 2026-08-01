//go:build darwin

package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"reasonix/internal/config"
)

// petDataRoot returns the base directory for pet data (spritesheets, metadata).
// Uses REASONIX_HOME if set, otherwise falls back to ~/.reasonix.
func petDataRoot() string {
	if rh := config.ReasonixHomeDir(); rh != "" {
		return rh
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".reasonix")
}

func petConfigPath() string {
	return config.UserConfigPath()
}

func petLoadConfig() *config.Config {
	return config.LoadForEdit(petConfigPath())
}

func petSaveConfig(cfg *config.Config) {
	if dir := filepath.Dir(petConfigPath()); dir != "" {
		os.MkdirAll(dir, 0755)
	}
	_ = cfg.SaveTo(petConfigPath())
}

// PetInfo holds a pet's slug and display name.
type PetInfo struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// petScanPets returns all installed pet slugs by scanning standard directories.
func petScanPets() []PetInfo {
	home, _ := os.UserHomeDir()
	var dirs []string
	dirs = append(dirs, filepath.Join(petDataRoot(), "pets"))
	if home != "" {
		dirs = append(dirs,
			filepath.Join(home, ".codex", "pets"),
			filepath.Join(home, ".petdex", "pets"),
		)
	}
	seen := map[string]bool{"default": true}
	var pets []PetInfo
	pets = append(pets, PetInfo{Slug: "default", Name: "🐱 Default"})
	for _, base := range dirs {
		entries, err := os.ReadDir(base)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			slug := e.Name()
			if seen[slug] {
				continue
			}
			// Verify at least one spritesheet file exists
			exts := []string{".webp", ".png"}
			hasSprite := false
			for _, ext := range exts {
				if _, err := os.Stat(filepath.Join(base, slug, "spritesheet"+ext)); err == nil {
					hasSprite = true
					break
				}
			}
			if !hasSprite {
				continue
			}
			seen[slug] = true
			name := slug
			if meta, err := os.ReadFile(filepath.Join(base, slug, "metadata.json")); err == nil {
				var info struct {
					Name string `json:"name"`
				}
				if json.Unmarshal(meta, &info) == nil && info.Name != "" {
					name = info.Name
				}
			}
			pets = append(pets, PetInfo{Slug: slug, Name: name})
		}
	}
	return pets
}

func petSpritesheetPaths(slug string) []string {
	if slug == "" || slug == "default" {
		return nil
	}
	home, _ := os.UserHomeDir()
	var dirs []string
	dirs = append(dirs, filepath.Join(petDataRoot(), "pets", slug))
	if home != "" {
		dirs = append(dirs,
			filepath.Join(home, ".codex", "pets", slug),
			filepath.Join(home, ".petdex", "pets", slug),
		)
	}
	var result []string
	for _, dir := range dirs {
		for _, ext := range []string{".webp", ".png"} {
			p := filepath.Join(dir, "spritesheet"+ext)
			if _, err := os.Stat(p); err == nil {
				result = append(result, p)
			}
		}
	}
	return result
}

func PetEnabled() bool {
	cfg := petLoadConfig()
	return cfg.DesktopPetEnabled()
}

func PetToggle() (result bool) {
	defer func() {
		if r := recover(); r != nil {
			result = false
		}
	}()
	cfg := petLoadConfig()
	enabled := cfg.DesktopPetEnabled()
	newVal := !enabled
	cfg.Desktop.PetEnabled = &newVal
	petSaveConfig(cfg)
	if newVal {
		CreatePetWindow(config.MemoryUserDir(), cfg.DesktopPetScale(), cfg.Desktop.PetPosX, cfg.Desktop.PetPosY)
	} else {
		PetCloseWindow()
	}
	return newVal
}

func PetSetScaleConfig(scale float64) {
	cfg := petLoadConfig()
	cfg.Desktop.PetScale = scale
	petSaveConfig(cfg)
}

func PetSetSlugConfig(slug string) {
	cfg := petLoadConfig()
	cfg.Desktop.PetSlug = slug
	petSaveConfig(cfg)
}

func PetSavePosConfig(x, y int) {
	cfg := petLoadConfig()
	cfg.Desktop.PetPosX = x
	cfg.Desktop.PetPosY = y
	petSaveConfig(cfg)
}

func (a *App) AppPetToggle() bool  { return PetToggle() }
func (a *App) AppPetEnabled() bool { return PetEnabled() }

func petIsDisabled() bool { return !PetEnabled() }
