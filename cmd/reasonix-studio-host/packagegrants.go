package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/platform/packagegrant"
)

// stripPackageGrants works on the directory the application runs from and is
// never handed a directory directly: the grants it removes are the ones that
// stop that application's own sandboxed children from loading, and no other
// tree is this flag's to change. The report goes to out as one JSON line.
func stripPackageGrants(out, logs io.Writer, app string) int {
	if strings.TrimSpace(app) == "" {
		fmt.Fprintln(logs, "strip-package-grants: -studio-app names no application")
		return 2
	}
	exe, err := filepath.EvalSymlinks(app)
	if err != nil {
		fmt.Fprintf(logs, "strip-package-grants: %v\n", err)
		return 2
	}
	if info, err := os.Stat(exe); err != nil || !info.Mode().IsRegular() {
		fmt.Fprintf(logs, "strip-package-grants: %s is not an application executable\n", exe)
		return 2
	}
	report, err := packagegrant.Strip(filepath.Dir(exe))
	if err != nil {
		fmt.Fprintf(logs, "strip-package-grants: %v\n", err)
		return 1
	}
	for _, refused := range report.Refused {
		fmt.Fprintf(logs, "strip-package-grants: kept a grant at %s: %v\n", refused.Path, refused.Err)
	}
	for _, unread := range report.Unread {
		fmt.Fprintf(logs, "strip-package-grants: could not read %s: %v\n", unread.Path, unread.Err)
	}
	if err := json.NewEncoder(out).Encode(report); err != nil {
		return 1
	}
	return 0
}
