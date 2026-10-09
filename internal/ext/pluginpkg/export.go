// export.go — an installed package, packed for someone else's machine.
package pluginpkg

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	fileencoding "reasonix/internal/base/fileutil/encoding"
	"reasonix/internal/base/secrets"
)

// ExportSizeLimit caps the packed bytes. A plugin root is arbitrary user
// content — a vendored node_modules or a checked-in model file turns an export
// into a memory incident — so the archive is refused rather than truncated.
const ExportSizeLimit = 64 << 20

// exportSkipDirs never travel with a package. .git in particular carries
// remote URLs with embedded tokens, which is exactly what an export must not
// hand to a stranger.
var exportSkipDirs = map[string]bool{".git": true, ".hg": true, ".svn": true}

var envVarRef = regexp.MustCompile(`^\$\{([A-Za-z_][A-Za-z0-9_]*)\}$`)

// Export packs the package rooted at root into a zip whose entries hang off a
// single <name>/ directory, and reports the environment variables whoever
// installs it has to supply. Every literal value in its MCP and runtime
// configuration is replaced by an ${ENV_VAR} reference on the way out.
func Export(name, root string) ([]byte, []string, error) {
	if !IsValidName(name) {
		return nil, nil, fmt.Errorf("invalid plugin name %q", name)
	}
	// A linked install is a symlink to wherever the author keeps it; walking
	// the link itself would pack nothing at all.
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, nil, err
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	required := map[string]bool{}
	var total int64

	walkErr := filepath.WalkDir(resolved, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(resolved, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if exportSkipDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		// Only regular files: a symlink out of the package would either
		// dangle on the other machine or smuggle a path off this one.
		if !d.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if credentialBearing(rel) {
			stripped, names, err := StripCredentials(data)
			if err != nil {
				return fmt.Errorf("%s: %w", rel, err)
			}
			data = stripped
			for _, n := range names {
				required[n] = true
			}
		}
		total += int64(len(data))
		if total > ExportSizeLimit {
			return fmt.Errorf("plugin %q exceeds the %d MB export limit", name, ExportSizeLimit>>20)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		header := &zip.FileHeader{Name: path.Join(name, rel), Method: zip.Deflate}
		header.SetMode(info.Mode().Perm())
		w, err := zw.CreateHeader(header)
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	})
	if walkErr != nil {
		return nil, nil, walkErr
	}
	if err := zw.Close(); err != nil {
		return nil, nil, err
	}
	out := slices.Sorted(maps.Keys(required))
	return buf.Bytes(), out, nil
}

// credentialBearing reports whether a file inside a package can carry MCP or
// runtime configuration. Every other file travels byte-for-byte: rewriting a
// skill's own JSON fixture because it happens to have an "env" key would
// corrupt the package to protect nothing.
func credentialBearing(rel string) bool {
	switch rel {
	case NativeManifest, CodexManifest, ClaudeManifest, claudeMCPPath, claudeSettingsPath:
		return true
	}
	return false
}

// StripCredentials rewrites every literal value under an "env" or "headers"
// object into an ${ENV_VAR} reference and reports the variable names the result
// now depends on. Telling secrets from ordinary values is deliberately not
// attempted: an export leaves this machine, so a heuristic that misses one
// leaks it, while a false positive costs one variable to fill in.
func StripCredentials(raw []byte) ([]byte, []string, error) {
	var doc any
	if err := json.Unmarshal(fileencoding.DecodeToUTF8(raw), &doc); err != nil {
		return nil, nil, err
	}
	required := map[string]bool{}
	stripNode(doc, "", required)
	// Re-serialized, because the rewrite has to reach the file: field order
	// follows Go's map ordering rather than the author's.
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	names := slices.Sorted(maps.Keys(required))
	return append(out, '\n'), names, nil
}

// stripNode walks the document carrying the name of the server it is inside,
// so two servers that both authenticate do not collapse onto one variable.
func stripNode(node any, scope string, required map[string]bool) {
	obj, ok := node.(map[string]any)
	if !ok {
		if list, ok := node.([]any); ok {
			for _, item := range list {
				stripNode(item, scope, required)
			}
		}
		return
	}
	for key, value := range obj {
		switch {
		case strings.EqualFold(key, "command"):
			if command, ok := value.(string); ok {
				obj[key] = secrets.RedactConfigValue("", command)
			}
		case strings.EqualFold(key, "url"):
			if endpoint, ok := value.(string); ok {
				obj[key] = secrets.RedactEndpoint(endpoint)
			}
		case strings.EqualFold(key, "args"):
			if args, ok := value.([]any); ok {
				text := make([]string, len(args))
				for i, arg := range args {
					text[i], _ = arg.(string)
				}
				for i, arg := range secrets.RedactArgs(text) {
					if _, ok := args[i].(string); ok {
						args[i] = arg
					}
				}
			}
		case strings.EqualFold(key, "mcpServers"):
			servers, ok := value.(map[string]any)
			if !ok {
				continue
			}
			for serverName, server := range servers {
				stripNode(server, serverName, required)
			}
		case strings.EqualFold(key, "env"), strings.EqualFold(key, "headers"):
			fields, ok := value.(map[string]any)
			if !ok {
				continue
			}
			for field, fieldValue := range fields {
				text, ok := fieldValue.(string)
				if !ok {
					continue
				}
				name := exportVarName(scope, field)
				if ref := envVarRef.FindStringSubmatch(strings.TrimSpace(text)); ref != nil {
					required[ref[1]] = true
					continue
				}
				fields[field] = "${" + name + "}"
				required[name] = true
			}
		default:
			stripNode(value, scope, required)
		}
	}
}

func exportVarName(scope, field string) string {
	name := envVarName(scope + "_" + field)
	if strings.TrimSpace(scope) == "" {
		name = envVarName(field)
	}
	return name
}

func envVarName(key string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(key)) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	if name := strings.Trim(b.String(), "_"); name != "" {
		if name[0] >= '0' && name[0] <= '9' {
			return "_" + name
		}
		return name
	}
	return "SECRET"
}
