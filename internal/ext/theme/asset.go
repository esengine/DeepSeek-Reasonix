package theme

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Kind names the two images a pack may carry: the one drawn behind the window,
// and the thumbnail a picker shows before you commit to it.
type Kind string

const (
	assetBackground Kind = "background"
	assetPreview    Kind = "preview"
)

// KindOf maps a wire name onto a kind, rejecting anything else. Callers take
// this string from a URL, so an unknown one must not become a file path.
func KindOf(s string) (Kind, bool) {
	switch Kind(strings.TrimSpace(s)) {
	case assetBackground:
		return assetBackground, true
	case assetPreview:
		return assetPreview, true
	}
	return "", false
}

// extensions are the image types a pack may ship, in the order they are tried.
// The list is what bounds this: a pack names its image in the manifest, but
// what is actually served is only ever <kind>.<one of these> beside it.
var extensions = []string{".webp", ".png", ".jpg", ".jpeg"}

var contentTypes = map[string]string{
	".webp": "image/webp",
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
}

// maxAssetBytes bounds what one pack can make a frontend download. The shipped
// backgrounds are around 200KB; a pack an order of magnitude past that is
// either a mistake or an attempt to wedge the window.
const maxAssetBytes = 8 << 20

// Asset returns one pack's image bytes and its content type. An installed pack
// shadows a shipped one, matching List and Load.
func Asset(id string, kind Kind) ([]byte, string, error) {
	if _, ok := KindOf(string(kind)); !ok {
		return nil, "", fmt.Errorf("theme: unknown asset %q", kind)
	}
	if err := validID(id); err != nil {
		return nil, "", err
	}
	dir, local := assetDir(id)
	for _, ext := range extensions {
		if !local {
			break
		}
		path := filepath.Join(dir, string(kind)+ext)
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		if info.Size() > maxAssetBytes {
			return nil, "", fmt.Errorf("theme %s: %s is %d bytes", id, kind, info.Size())
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		return raw, contentTypes[ext], nil
	}
	for _, ext := range extensions {
		if isPluginID(id) {
			break
		}
		raw, err := builtin.ReadFile(builtinAssetName(id, kind, ext))
		if err != nil {
			continue
		}
		return raw, contentTypes[ext], nil
	}
	return nil, "", fmt.Errorf("theme %s: no %s image", id, kind)
}

func hasAsset(id string, kind Kind) bool {
	if validID(id) != nil {
		return false
	}
	dir, local := assetDir(id)
	for _, ext := range extensions {
		if !local {
			break
		}
		if info, err := os.Stat(filepath.Join(dir, string(kind)+ext)); err == nil && !info.IsDir() {
			return true
		}
		if isPluginID(id) {
			continue
		}
		if _, err := fs.Stat(builtin, builtinAssetName(id, kind, ext)); err == nil {
			return true
		}
	}
	return false
}

// Shipped assets are flat rather than one directory per pack, so the id has to
// carry no separator for this to stay inside builtin/ — validID is what says so.
func builtinAssetName(id string, kind Kind, ext string) string {
	return "builtin/" + id + "." + string(kind) + ext
}

func validID(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("theme: empty id")
	}
	if id != filepath.Base(id) || strings.ContainsAny(id, `/\`) {
		return fmt.Errorf("theme: %q is not a pack id", id)
	}
	return nil
}
