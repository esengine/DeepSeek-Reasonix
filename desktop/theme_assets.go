package main

import (
	"bytes"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/config"
)

const themeAssetURLPrefix = "/__reasonix_theme_asset/"

// themeAssetMiddleware serves theme background images through a content-addressed
// read-only route. The frontend only receives temporary URLs — never absolute paths.
//
// URL shape: /__reasonix_theme_asset/{themeID}/{digest}/{filename}
// On every request the server re-validates theme id, filename, MIME, file identity
// (digest), and directory containment.
func (a *App) themeAssetMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, themeAssetURLPrefix) {
				next.ServeHTTP(w, r)
				return
			}
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			// Safe mode never serves external theme assets.
			if a.themeSafeMode() {
				http.NotFound(w, r)
				return
			}
			rest := strings.TrimPrefix(r.URL.Path, themeAssetURLPrefix)
			parts := strings.Split(rest, "/")
			if len(parts) != 3 {
				http.NotFound(w, r)
				return
			}
			themeID, digest, filename := parts[0], parts[1], parts[2]
			if !themePackIDRe.MatchString(themeID) {
				http.NotFound(w, r)
				return
			}
			if !themePackImageRe.MatchString(filename) {
				http.NotFound(w, r)
				return
			}
			if len(digest) < 8 || len(digest) > 64 {
				http.NotFound(w, r)
				return
			}
			// Official themes serve embedded assets with immutable caching;
			// everything else falls through to the user library path below.
			if isOfficialThemeID(themeID) {
				serveOfficialThemeAsset(w, r, themeID, digest, filename)
				return
			}
			if isBuiltinThemeID(themeID) {
				http.NotFound(w, r)
				return
			}
			abs, err := resolveThemeImageAbs(themeID, filename)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			// Re-validate file identity against the digest embedded in the URL.
			got, err := themeFileDigest(abs)
			if err != nil || !strings.EqualFold(got, digest) {
				http.NotFound(w, r)
				return
			}
			if err := validateThemeImageFile(abs); err != nil {
				http.NotFound(w, r)
				return
			}
			f, err := os.Open(abs)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			defer f.Close()
			info, err := f.Stat()
			if err != nil {
				http.NotFound(w, r)
				return
			}
			mimeType := themeImageMIMEFromName(filename)
			w.Header().Set("Content-Type", mimeType)
			w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": filename}))
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Cache-Control", "private, max-age=3600")
			http.ServeContent(w, r, filename, info.ModTime(), f)
		})
	}
}

// serveOfficialThemeAsset serves embedded official backgrounds and previews.
// The URL digest is re-verified against the embedded bytes on every request;
// only the manifest-declared background and the fixed preview name resolve.
func serveOfficialThemeAsset(w http.ResponseWriter, r *http.Request, themeID, digest, filename string) {
	ot := findOfficialTheme(themeID)
	if ot == nil {
		http.NotFound(w, r)
		return
	}
	want := ""
	switch filename {
	case ot.manifest.Background.Image:
		want = ot.bgDigest
	case officialPreviewName:
		want = ot.previewDigest
	default:
		http.NotFound(w, r)
		return
	}
	if !strings.EqualFold(want, digest) {
		http.NotFound(w, r)
		return
	}
	data, name, err := readOfficialAsset(themeID, filename)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// MIME re-verification: sniff must agree with the declared extension.
	head := data
	if len(head) > 512 {
		head = head[:512]
	}
	if sniffThemeImageMIME(head, name) != themeImageMIMEFromName(name) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", themeImageMIMEFromName(name))
	w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": name}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}

func (a *App) themeSafeMode() bool {
	// DesktopStartupSettings exposes SafeMode; keep a local helper so asset
	// middleware and list/load paths share the same gate.
	return config.SafeModeRequested()
}

func themeBackgroundURL(themeID, imageName string) string {
	if imageName == "" || isBuiltinThemeID(themeID) {
		return ""
	}
	abs, err := resolveThemeImageAbs(themeID, imageName)
	if err != nil {
		return ""
	}
	digest, err := themeFileDigest(abs)
	if err != nil {
		return ""
	}
	return themeAssetURLPrefix + themeID + "/" + digest + "/" + filepath.Base(imageName)
}
