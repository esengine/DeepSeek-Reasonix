package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// loadDotEnv loads Reasonix's global .env for provider credentials. The
// workspace .env values returned by loadDotEnvForRoot are ignored here because
// loadDotEnv has no Config to carry a workspace-scoped expansion environment.
func loadDotEnv() {
	loadDotEnvForRoot(".")
}

// loadDotEnvForRoot returns workspace .env values for scoped plugin/MCP/proxy
// expansion, then loads Reasonix's global .env for provider credentials.
// Workspace .env values are deliberately not written into the process
// environment, so multiple desktop/ACP workspaces cannot leak tokens into each
// other and project files cannot redirect Reasonix's own config/credential
// paths.
func loadDotEnvForRoot(root string) map[string]string {
	projectEnv := loadProjectDotEnvForExpansion(root)
	loadCredentialStoreForRoot(root)
	return projectEnv
}

func loadProjectDotEnvForExpansion(root string) map[string]string {
	root = resolveRoot(root)
	path := ".env"
	if root != "." {
		path = filepath.Join(root, ".env")
	}
	if current := UserCredentialsPath(); current != "" && samePath(path, current) {
		return nil
	}
	return readDotEnvFileMap(path, func(key string) bool {
		return !isProjectDotEnvControlKey(key)
	})
}

func isProjectDotEnvControlKey(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return true
	}
	upper := strings.ToUpper(key)
	if strings.HasPrefix(upper, "REASONIX_") {
		return true
	}
	switch upper {
	case "HOME", "USERPROFILE", "APPDATA", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME":
		return true
	default:
		return false
	}
}

func legacyCredentialsPaths() []string {
	current := UserCredentialsPath()
	seen := map[string]bool{}
	var paths []string
	add := func(path string) {
		if path == "" {
			return
		}
		path = filepath.Clean(path)
		if current != "" && samePath(path, current) {
			return
		}
		if seen[path] {
			return
		}
		seen[path] = true
		paths = append(paths, path)
	}
	if dir := legacyOSSupportDir(); dir != "" {
		add(filepath.Join(dir, "credentials"))
	}
	if dir := userSupportDir(); dir != "" {
		add(filepath.Join(dir, "credentials"))
	}
	for _, cfg := range legacyXDGConfigPaths() {
		add(filepath.Join(filepath.Dir(cfg), "credentials"))
	}
	return paths
}

func loadDotEnvFileAs(path string, source CredentialSource) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.Trim(strings.TrimSpace(val), `"'`)
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); exists && source.Kind != CredentialSourceCredentials {
			recordExistingCredentialSource(key)
			continue
		}
		if err := os.Setenv(key, val); err == nil && source.Kind != "" {
			source.Path = path
			recordCredentialSource(key, val, source)
		}
	}
}

func readDotEnvFileMap(path string, allow func(string) bool) map[string]string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" || allow != nil && !allow(key) {
			continue
		}
		out[key] = strings.Trim(strings.TrimSpace(val), `"'`)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func envFileValue(path, wantKey string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key != wantKey {
			continue
		}
		val = strings.Trim(strings.TrimSpace(val), `"'`)
		return val, true
	}
	return "", false
}
