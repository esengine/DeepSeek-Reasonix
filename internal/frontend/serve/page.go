package serve

import (
	"fmt"
	"io/fs"
	"net/http"
	"os"
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
	for _, candidate := range []string{
		filepath.Join(filepath.Dir(exe), "frontend-next", "dist"),
		filepath.Join("..", "frontend-next", "dist"),
		filepath.Join("desktop", "frontend-next", "dist"),
	} {
		if hasIndex(candidate) {
			return os.DirFS(candidate), nil
		}
	}
	return nil, nil
}

func hasIndex(dir string) bool {
	st, err := os.Stat(filepath.Join(dir, "index.html"))
	return err == nil && !st.IsDir()
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
			files.ServeHTTP(w, r)
			return
		}
		kernel.ServeHTTP(w, r)
	})
}
