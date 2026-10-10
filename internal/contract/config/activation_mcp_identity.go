package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"reasonix/internal/base/proc"
)

// ProjectDeclarationDigest covers everything a repository controls about one
// declared server's launch: the declaration as written, the project .env
// values it expands, and the content of workspace files its command or
// arguments name directly. Interpreters found on PATH and files those load in
// turn are the user's or out of reach, and are not covered.
func ProjectDeclarationDigest(entry PluginEntry, root string) string {
	in := declarationInputsOf(entry, root)
	payload, _ := json.Marshal(struct {
		Declaration   json.RawMessage
		DotEnv, Files map[string]string
	}{declarationText(entry), in.DotEnv, in.Files})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

// ErrProjectDeclarationChanged is a repository-declared server whose
// declaration, or a workspace input it draws on, changed after its spec was
// built, so starting it would run something nobody decided on.
var ErrProjectDeclarationChanged = errors.New("project MCP declaration changed since it was enabled")

// DeclaredLaunchCheck pins entry's declaration as it reads now and returns the
// check a launcher runs before starting it; nil for a server the user declared.
func DeclaredLaunchCheck(entry PluginEntry, root string) func() error {
	if repoDeclared, _, _ := mcpIdentity(entry); !repoDeclared {
		return nil
	}
	pinned := ProjectDeclarationDigest(entry, root)
	return func() error {
		if ProjectDeclarationDigest(entry, root) != pinned {
			return fmt.Errorf("%w: %q", ErrProjectDeclarationChanged, entry.Name)
		}
		return nil
	}
}

// declarationText is the launch part of the declaration as the file spells it.
func declarationText(entry PluginEntry) json.RawMessage {
	text, _ := json.Marshal(struct {
		Type, Command, URL string
		Args               []string
		Env, Headers       map[string]string
	}{
		strings.ToLower(strings.TrimSpace(entry.Type)), entry.Command, entry.URL,
		nonEmptyStrings(entry.Args), nonEmptyStringMap(entry.Env), nonEmptyStringMap(entry.Headers),
	})
	return text
}

type declarationInputs struct {
	DotEnv, Files map[string]string
}

func declarationInputsOf(entry PluginEntry, root string) declarationInputs {
	in := declarationInputs{DotEnv: map[string]string{}, Files: map[string]string{}}
	record := func(name string) (string, bool) {
		v, ok := entry.expansionEnv[name]
		in.DotEnv[name] = v
		return v, ok
	}
	fields := append([]string{entry.Command, entry.URL}, entry.Args...)
	for _, v := range entry.Env {
		fields = append(fields, v)
	}
	for _, v := range entry.Headers {
		fields = append(fields, v)
	}
	for _, f := range fields {
		expandVarsWithLookup(f, record)
	}
	ws := WorkspaceRootValue(root)
	expanded := entry.ExpandedPluginForRoot(root)
	words := append(launchCandidates(expanded), expanded.Args...)
	for _, path := range workspaceFilesNamed(ws, ws, words) {
		key := path
		if rel, err := filepath.Rel(ws, path); err == nil {
			key = filepath.ToSlash(rel)
		}
		in.Files[key] = fileContentDigest(path)
	}
	return in
}

// commandNamesWindows selects the launcher's Windows name probing; a seam so
// every OS can test it.
var commandNamesWindows = runtime.GOOS == "windows"

// launchCandidates names every file the launcher could start for e's command:
// each spelling it tries, and for a bare name each directory of the
// declaration's own PATH. The user's PATH is theirs and stays out.
func launchCandidates(e PluginEntry) []string {
	// The host's PATHEXT (os/exec probes with it) and every declared spelling of
	// PATH/PATHEXT count, in key order: Windows env keys ignore case, so which
	// one the launcher ends up with is not this digest's to guess.
	names := proc.CommandNames(e.Command, os.Getenv("PATHEXT"), commandNamesWindows)
	var declaredPaths []string
	for _, k := range slices.Sorted(maps.Keys(e.Env)) {
		v := e.Env[k]
		switch {
		case strings.EqualFold(k, "PATHEXT"):
			for _, n := range proc.CommandNames(e.Command, v, commandNamesWindows) {
				if !slices.Contains(names, n) {
					names = append(names, n)
				}
			}
		case k == "PATH" || commandNamesWindows && strings.EqualFold(k, "PATH"):
			declaredPaths = append(declaredPaths, filepath.SplitList(v)...)
		}
	}
	if strings.ContainsAny(e.Command, `/\`) {
		return names
	}
	out := []string{e.Command}
	for _, dir := range declaredPaths {
		if dir == "" || !filepath.IsAbs(dir) {
			continue
		}
		for _, name := range names {
			out = append(out, filepath.Join(dir, name))
		}
	}
	return out
}

// fileContentDigest hashes path's content; a missing or unreadable file gets a
// marker of its own, so creating or restoring it reads as a change.
func fileContentDigest(path string) string {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "missing"
		}
		return "unreadable"
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "unreadable"
	}
	return hex.EncodeToString(h.Sum(nil))
}

func nonEmptyStrings(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	return s
}

func nonEmptyStringMap(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	return m
}
