package secrets

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
)

// Inverted so the zero value protects: a caller that never wires the config is
// confined, and only an explicit opt-out opens the files.
var disableCredentialProtection atomic.Bool

// SetProtectCredentialFiles enables or disables the always-on credential-file
// read denial ([secrets] protect_credential_files, default true).
func SetProtectCredentialFiles(enabled bool) { disableCredentialProtection.Store(!enabled) }

// ProtectCredentialFiles reports whether credential files are denied to readers.
func ProtectCredentialFiles() bool { return !disableCredentialProtection.Load() }

// The ~/.ssh entries carrying no private key material. Judged by exception
// because keys take user-chosen names (work_github): "id_*" would miss them.
var sshPublicNames = map[string]bool{
	"config":          true,
	"known_hosts":     true,
	"known_hosts.old": true,
	"authorized_keys": true,
	"environment":     true,
	"rc":              true,
}

// CredentialReadPath reports whether abs names a credential file: one holding
// secret bytes and no settings. Files mixing both (~/.npmrc, gh hosts.yml) are
// absent because denying them breaks the tool that owns them.
func CredentialReadPath(abs string) bool {
	if !ProtectCredentialFiles() {
		return false
	}
	clean := filepath.Clean(abs)
	for _, home := range homeCandidates() {
		if withinSSHDir(home, clean) {
			name := strings.ToLower(filepath.Base(clean))
			return !sshPublicNames[name] && !strings.HasSuffix(name, ".pub")
		}
		for _, p := range credentialExactPaths(home) {
			if pathsEqual(clean, p) {
				return true
			}
		}
	}
	return false
}

// Both spellings of home, resolved and raw. Matching only the resolved one
// fails open for a caller that did not run its path through EvalSymlinks, and a
// deny check that fails open is worse than none: it reads as protection.
func homeCandidates() []string {
	resolved, raw := credentialHome(), rawHome()
	switch {
	case resolved == "":
		return nil
	case raw == "" || raw == resolved:
		return []string{resolved}
	default:
		return []string{resolved, raw}
	}
}

func rawHome() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

// CredentialFilePaths enumerates the credential files that exist now, for OS
// sandbox deny lists that take paths rather than predicates. A snapshot: a key
// written later is denied to bash only from the next build, though
// CredentialReadPath still hides it from the read tools.
func CredentialFilePaths() []string {
	if !ProtectCredentialFiles() {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	var out []string
	sshDir := filepath.Join(home, ".ssh")
	if entries, err := os.ReadDir(sshDir); err == nil {
		for _, e := range entries {
			if !e.Type().IsRegular() {
				continue
			}
			name := strings.ToLower(e.Name())
			if sshPublicNames[name] || strings.HasSuffix(name, ".pub") {
				continue
			}
			out = append(out, filepath.Join(sshDir, e.Name()))
		}
	}
	for _, p := range credentialExactPaths(home) {
		if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() {
			out = append(out, p)
		}
	}
	return out
}

func credentialExactPaths(home string) []string {
	return []string{
		filepath.Join(home, ".aws", "credentials"),
		filepath.Join(home, ".pypirc"),
		filepath.Join(home, ".netrc"),
		filepath.Join(home, "_netrc"),
		filepath.Join(home, ".git-credentials"),
		filepath.Join(home, ".config", "gcloud", "application_default_credentials.json"),
		filepath.Join(home, ".config", "gcloud", "credentials.db"),
		filepath.Join(home, ".config", "gcloud", "access_tokens.db"),
	}
}

// Cached by raw home value, so the common path costs no syscall and a changed
// HOME still re-resolves.
var resolvedHome atomic.Pointer[[2]string]

func credentialHome() string {
	raw := rawHome()
	if raw == "" {
		return ""
	}
	if cached := resolvedHome.Load(); cached != nil && cached[0] == raw {
		return cached[1]
	}
	real := raw
	if resolved, err := filepath.EvalSymlinks(raw); err == nil {
		real = resolved
	}
	resolvedHome.Store(&[2]string{raw, real})
	return real
}

func withinSSHDir(home, clean string) bool {
	dir := filepath.Join(home, ".ssh")
	if foldPaths {
		dir, clean = strings.ToLower(dir), strings.ToLower(clean)
	}
	rel, err := filepath.Rel(dir, clean)
	if err != nil || rel == "." {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Case-insensitive filesystems: a deny-side comparison that respected case
// there would be bypassable by spelling.
var foldPaths = runtime.GOOS == "windows" || runtime.GOOS == "darwin"

func pathsEqual(a, b string) bool {
	if foldPaths {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// ForbiddenReadPaths is every path this package denies readers, in the form an
// OS sandbox takes. broad adds the directories [secrets] protect_sensitive_files
// covers; that denylist's name-pattern half (.env, *.pem) cannot come along,
// because only an anchored path can be masked or denied.
func ForbiddenReadPaths(broad bool) []string {
	out := CredentialFilePaths()
	if broad {
		out = append(out, SensitiveHomeDirs()...)
	}
	return out
}

// SensitiveHomeDirs are the home directories protect_sensitive_files hides.
func SensitiveHomeDirs() []string {
	home := credentialHome()
	if home == "" {
		return nil
	}
	return []string{filepath.Join(home, ".ssh")}
}

// SensitiveFileName reports whether a file's base name marks it as holding
// secrets wherever it sits: dotenv files and credential stores, private key
// and certificate containers, and the default SSH identity files.
func SensitiveFileName(name string) bool {
	name = strings.ToLower(name)
	switch name {
	case ".env", ".git-credentials", ".netrc", "id_rsa", "id_dsa", "id_ecdsa", "id_ed25519":
		return true
	}
	if rest, ok := strings.CutPrefix(name, ".env."); ok {
		return rest != "example" && rest != "sample" && rest != "template"
	}
	for _, ext := range []string{".pem", ".key", ".p12", ".pfx"} {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	return false
}
