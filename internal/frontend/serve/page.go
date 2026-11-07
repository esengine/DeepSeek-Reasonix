package serve

import (
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// FindPage resolves the built page a host serves. An explicit directory holding
// none is a launch that would open on nothing, so it fails here rather than at
// the first paint. Nothing found is (nil, nil): the host serves the kernel
// alone, and says so in its own words.
func FindPage(dir string) (fs.FS, error) {
	if dir != "" {
		if !hasIndex(dir) {
			return nil, fmt.Errorf("no index.html under %s", dir)
		}
		return os.DirFS(dir), nil
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	if page := pageBeside(exe); page != nil {
		return page, nil
	}
	for _, candidate := range []string{
		filepath.Join("..", "frontend-next", "dist"),
		filepath.Join("desktop", "frontend-next", "dist"),
	} {
		if hasIndex(candidate) {
			return os.DirFS(candidate), nil
		}
	}
	return nil, nil
}

// pageBeside looks next to the executable as invoked, then next to where a link
// to it points: a package manager that links the binary onto the PATH leaves the
// page beside the real file.
func pageBeside(exe string) fs.FS {
	dirs := []string{filepath.Dir(exe)}
	if real, err := filepath.EvalSymlinks(exe); err == nil && filepath.Dir(real) != dirs[0] {
		dirs = append(dirs, filepath.Dir(real))
	}
	for _, dir := range dirs {
		if candidate := filepath.Join(dir, "frontend-next", "dist"); hasIndex(candidate) {
			return os.DirFS(candidate)
		}
	}
	return nil
}

func hasIndex(dir string) bool {
	st, err := os.Stat(filepath.Join(dir, "index.html"))
	return err == nil && !st.IsDir()
}

// pageContentTypes is the type of every kind of file the built page ships.
var pageContentTypes = map[string]string{
	".html": "text/html; charset=utf-8", ".js": "text/javascript; charset=utf-8", ".mjs": "text/javascript; charset=utf-8",
	".css": "text/css; charset=utf-8", ".json": "application/json", ".map": "application/json", ".svg": "image/svg+xml",
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp", ".ico": "image/x-icon",
	".woff": "font/woff", ".woff2": "font/woff2", ".ttf": "font/ttf", ".otf": "font/otf", ".wasm": "application/wasm", ".webmanifest": "application/manifest+json", ".txt": "text/plain; charset=utf-8",
}

// withPage serves the files the built page brought with it and leaves every
// other path to the kernel. Which paths are the page's is asked of the page
// itself, never of a list of kernel routes. A path naming no file is the
// page's own routing, so it reaches the kernel, whose root hands the shell
// back.
func withPage(kernel http.Handler, page fs.FS) http.Handler {
	if page == nil {
		return kernel
	}
	files := http.FileServer(http.FS(page))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.Trim(r.URL.Path, "/")
		if name == "" {
			kernel.ServeHTTP(w, r)
			return
		}
		if st, err := fs.Stat(page, name); err == nil && !st.IsDir() {
			// On Windows the library reads types from the registry, where another
			// program's entry can turn the SVG logo into a broken image.
			if kind, ok := pageContentTypes[strings.ToLower(path.Ext(name))]; ok {
				w.Header().Set("Content-Type", kind)
			}
			files.ServeHTTP(w, r)
			return
		}
		kernel.ServeHTTP(w, r)
	})
}
