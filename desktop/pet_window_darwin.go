//go:build darwin

package main

/*
#cgo darwin LDFLAGS: -framework Cocoa -framework WebKit
#include <stdlib.h>
extern void createPetWindow(const char *homeDir, const char *html);
extern void petEvaluateJS(const char *script);
extern void petMoveBy(double dx, double dy);
extern void petSetPosition(int x, int y);
extern void petSetWindowScale(double scale);
extern void petCloseWindow(void);
*/
import "C"
import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
	"unsafe"
)

// ── Public API (called from app.go) ──────────────────────────────────────────

var petWindowActive atomic.Bool

// CreatePetWindow creates a transparent floating WKWebView. After the page
// loads, petOnWebViewReady() (ObjC navigation delegate) sends the default
// sprite, scale, pet list, and initial state. Thread-safe via atomic guard.
func CreatePetWindow(homeDir string, scale float64, posX, posY int) {
	if !petWindowActive.CompareAndSwap(false, true) {
		return // already created or being created
	}
	cHome := C.CString(homeDir)
	cHTML := C.CString(petPageHTML())
	C.createPetWindow(cHome, cHTML)
	C.free(unsafe.Pointer(cHome))
	C.free(unsafe.Pointer(cHTML))

	// Scale and position are restored in petOnWebViewReady after the window
	// exists (dispatch_async in createPetWindow hasn't run yet at this point).
}

// PetEvaluateJS evaluates JavaScript in the pet WebView.
func PetEvaluateJS(script string) {
	if script == "" {
		return
	}
	cScript := C.CString(script)
	C.petEvaluateJS(cScript)
	C.free(unsafe.Pointer(cScript))
}

// PetSetState updates the pet animation state, bubble text, and session count.
func PetSetState(state, bubble string, sessionCount int) {
	js := fmt.Sprintf("window.setHookState(%q);window.__sessions=%d", state, sessionCount)
	PetEvaluateJS(js)
	if bubble != "" {
		PetEvaluateJS(fmt.Sprintf("showBubble(%q,2500)", bubble))
	}
}

// PetMoveBy moves the pet window by deltas.
func PetMoveBy(dx, dy float64) {
	C.petMoveBy(C.double(dx), C.double(dy))
}

// PetSetScale resizes the pet window and persists to config.
func PetSetScale(scale float64) {
	if scale < 0.5 {
		scale = 0.5
	}
	if scale > 1.5 {
		scale = 1.5
	}
	C.petSetWindowScale(C.double(scale))
	PetSetScaleConfig(scale)
	PetEvaluateJS(fmt.Sprintf("setScale(%f)", scale))
}

// PetSetSlug switches the pet sprite and persists to config.
func PetSetSlug(slug string) {
	if slug == "" {
		slug = "default"
	}
	PetSetSlugConfig(slug)
	PetEvaluateJS(fmt.Sprintf("window.__petSlug=%q", slug))
	if slug == "default" {
		PetEvaluateJS("resetPetSprite()")
	} else {
		sendPetSprite(slug)
	}
}

// PetCloseWindow closes the native window and resets the atomic guard.
func PetCloseWindow() {
	C.petCloseWindow()
	petWindowActive.Store(false)
}

// petRefreshI18n re-sends the current i18n strings to the pet JS.
// Called when the desktop language changes in Settings.
func petRefreshI18n() {
	if !petWindowActive.Load() {
		return
	}
	i18n := petI18n()
	js := petI18nJSON(i18n)
	PetEvaluateJS(fmt.Sprintf("window.__petI18n=%s;if(typeof I!=='undefined')I=window.__petI18n", js))
}

// petPopBubble picks a random string from items and shows it as a bubble.
// Safe on empty/nil slice (no-op).
func petPopBubble(items []string) {
	if len(items) == 0 {
		return
	}
	msg := items[int(time.Now().UnixNano())%len(items)]
	PetEvaluateJS(fmt.Sprintf("showBubble(%q,3000)", msg))
}

// ── Internal helpers ─────────────────────────────────────────────────────────

// sendPetSprite loads the spritesheet for a slug and sends it to JS as
// a base64 data URL via setPetSprite().
func sendPetSprite(slug string) {
	paths := petSpritesheetPaths(slug)
	if len(paths) == 0 {
		i18n := petI18n()
		PetEvaluateJS(fmt.Sprintf("showBubble(%q,3000)", fmt.Sprintf(i18n.SpriteNotFound, slug)))
		return
	}
	data, err := os.ReadFile(paths[0])
	if err != nil {
		return
	}
	mime := "image/webp"
	if strings.HasSuffix(paths[0], ".png") {
		mime = "image/png"
	}
	b64 := base64.StdEncoding.EncodeToString(data)
	dataURL := fmt.Sprintf("data:%s;base64,%s", mime, b64)
	PetEvaluateJS(fmt.Sprintf("setPetSprite(%q)", dataURL))
}

// ── CGO exports (called from ObjC) ──────────────────────────────────────────

//export petOnJSEval
func petOnJSEval(result *C.char) {
	_ = C.GoString(result)
}

//export petOnSavePos
func petOnSavePos(x, y C.int) {
	PetSavePosConfig(int(x), int(y))
}

//export petOnSetScale
func petOnSetScale(scale C.double) {
	PetSetScale(float64(scale))
}

//export petOnSetSlug
func petOnSetSlug(slug *C.char) {
	PetSetSlug(C.GoString(slug))
}

//export petOnGetPets
func petOnGetPets() {
	// Refresh i18n in case language changed in Settings.
	i18n := petI18n()
	js := petI18nJSON(i18n)
	PetEvaluateJS(fmt.Sprintf("window.__petI18n=%s;if(typeof I!=='undefined')I=window.__petI18n", js))
	pets := petScanPets()
	b, _ := json.Marshal(pets)
	PetEvaluateJS(fmt.Sprintf("window.__PET_PETS=%s", string(b)))
}

// petOnWebViewReady is called from the WKNavigationDelegate after the pet
// HTML finishes loading. This is the only safe point to push initial state.
//
//export petOnWebViewReady
func petOnWebViewReady() {
	cfg := petLoadConfig()
	i18n := petI18n()
	// Send i18n strings to JS
	PetEvaluateJS(fmt.Sprintf("window.__petI18n=%s;I=window.__petI18n", petI18nJSON(i18n)))
	if len(defaultSpritesheet) > 0 {
		mime := "image/webp"
		b64 := base64.StdEncoding.EncodeToString(defaultSpritesheet)
		dataURL := fmt.Sprintf("data:%s;base64,%s", mime, b64)
		PetEvaluateJS(fmt.Sprintf("setDefaultSprite(%q)", dataURL))
	}
	scale := cfg.DesktopPetScale()
	if scale < 0.5 || scale > 1.5 {
		scale = 1.0
	}
	if scale != 1.0 {
		C.petSetWindowScale(C.double(scale))
		PetEvaluateJS(fmt.Sprintf("setScale(%f)", scale))
	}
	if pX, pY := cfg.Desktop.PetPosX, cfg.Desktop.PetPosY; pX != 0 || pY != 0 {
		C.petSetPosition(C.int(pX), C.int(pY))
	}
	pets := petScanPets()
	b, _ := json.Marshal(pets)
	PetEvaluateJS(fmt.Sprintf("window.__PET_PETS=%s", string(b)))
	slug := cfg.DesktopPetSlug()
	if slug != "" && slug != "default" {
		sendPetSprite(slug)
		PetEvaluateJS(fmt.Sprintf("window.__petSlug=%q", slug))
	}
	PetSetState("idle", i18n.Hello, 0)
}

// petdexInstall downloads a pet from the Petdex API and installs it
// to $REASONIX_HOME/pets/<slug>/spritesheet.webp without using npx.
func petdexInstall(name string) {
	i18n := petI18n()
	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Get("https://petdex.dev/api/manifest")
	if err != nil {
		PetEvaluateJS(fmt.Sprintf("showBubble(%q,3000)", i18n.InstallNetworkErr))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		PetEvaluateJS(fmt.Sprintf("showBubble(%q,3000)", fmt.Sprintf(i18n.InstallServerErr, resp.StatusCode)))
		return
	}
	body, _ := io.ReadAll(resp.Body)

	var manifest struct {
		Pets []struct {
			Slug           string `json:"slug"`
			DisplayName    string `json:"displayName"`
			SpritesheetURL string `json:"spritesheetUrl"`
		} `json:"pets"`
	}
	if err := json.Unmarshal(body, &manifest); err != nil {
		PetEvaluateJS(fmt.Sprintf("showBubble(%q,3000)", i18n.InstallParseErr))
		return
	}

	var slug, url string
	if name == "__random__" {
		if len(manifest.Pets) > 0 {
			idx := int(time.Now().UnixMilli() % int64(len(manifest.Pets)))
			if idx < 0 {
				idx = 0
			}
			p := manifest.Pets[idx]
			slug = p.Slug
			url = p.SpritesheetURL
		}
	} else {
		for _, p := range manifest.Pets {
			if p.Slug == name || p.DisplayName == name {
				slug = p.Slug
				url = p.SpritesheetURL
				break
			}
		}
	}
	if slug == "" {
		PetEvaluateJS(fmt.Sprintf("showBubble(%q,3000)", fmt.Sprintf(i18n.InstallNotFound, name)))
		return
	}
	if slug == "" || strings.Contains(slug, "..") || strings.ContainsAny(slug, `/\`) {
		PetEvaluateJS(fmt.Sprintf("showBubble(%q,3000)", fmt.Sprintf(i18n.InstallInvalidSlug, slug)))
		return
	}
	if url == "" {
		PetEvaluateJS(fmt.Sprintf("showBubble(%q,3000)", i18n.InstallNoSprite))
		return
	}

	imgResp, err := httpClient.Get(url)
	if err != nil {
		PetEvaluateJS(fmt.Sprintf("showBubble(%q,3000)", i18n.InstallDlFailed))
		return
	}
	defer imgResp.Body.Close()
	if imgResp.StatusCode != 200 {
		PetEvaluateJS(fmt.Sprintf("showBubble(%q,3000)", i18n.InstallDlFailed))
		return
	}
	imgData, _ := io.ReadAll(imgResp.Body)

	rh := petDataRoot()
	ext := ".webp"
	if strings.HasSuffix(url, ".png") {
		ext = ".png"
	}
	petDir := filepath.Join(rh, "pets", slug)
	os.MkdirAll(petDir, 0755)
	os.WriteFile(filepath.Join(petDir, "spritesheet"+ext), imgData, 0644)
	// Save display name as metadata (if available from manifest)
	var petName = slug
	for _, p := range manifest.Pets {
		if p.Slug == slug && p.DisplayName != "" {
			petName = p.DisplayName
			break
		}
	}
	if meta, err := json.Marshal(map[string]string{"name": petName}); err == nil {
		os.WriteFile(filepath.Join(petDir, "metadata.json"), meta, 0644)
	}

	PetEvaluateJS(fmt.Sprintf("showBubble(%q,3000)", fmt.Sprintf(i18n.InstallSuccess, slug)))
	pets := petScanPets()
	b, _ := json.Marshal(pets)
	PetEvaluateJS(fmt.Sprintf("window.__PET_PETS=%s", string(b)))
	PetSetSlug(slug)
}

//export petOnInstallPet
func petOnInstallPet(name *C.char) {
	petName := C.GoString(name)
	if petName == "" {
		return
	}
	// Show bubble before goroutine (main thread, guaranteed to execute)
	PetEvaluateJS(fmt.Sprintf("showBubble(%q,5000)", petI18n().InstallDownloading))
	go petdexInstall(petName)
}

//export petOnDeletePet
func petOnDeletePet(slug *C.char) {
	petName := C.GoString(slug)
	if petName == "" || petName == "default" {
		return
	}
	if strings.Contains(petName, "..") || strings.ContainsAny(petName, `/\`) {
		return
	}
	paths := petSpritesheetPaths(petName)
	// Remove spritesheet file(s) and the entire pet directory
	for _, p := range paths {
		os.Remove(p)
		os.RemoveAll(filepath.Dir(p))
	}
	// Fallback: even if no spritesheet found in standard paths, try removing
	// the directory from the pet data root directly.
	deleteDir := filepath.Join(petDataRoot(), "pets", petName)
	os.RemoveAll(deleteDir)
	// If the deleted pet was current, reset to default (this also refreshes JS).
	cfg := petLoadConfig()
	if cfg.DesktopPetSlug() == petName {
		PetSetSlug("default")
	}
	// Refresh pet list in JS (PetSetSlug may have already done this, but
	// we need to ensure the list is fresh even if the deleted pet wasn't current).
	pets := petScanPets()
	b, _ := json.Marshal(pets)
	PetEvaluateJS(fmt.Sprintf("window.__PET_PETS=%s", string(b)))
	i18n := petI18n()
	PetEvaluateJS(fmt.Sprintf("showBubble(%q,3000)", fmt.Sprintf(i18n.DeleteDone, petName)))
}
