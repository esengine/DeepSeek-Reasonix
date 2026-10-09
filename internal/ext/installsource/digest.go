package installsource

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
)

// digestPattern is the only spelling a content digest takes, on the wire and in
// a registry row; anything else is not a pin.
var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// IsContentDigest reports whether s is a well-formed content digest.
func IsContentDigest(s string) bool { return digestPattern.MatchString(s) }

// digestPart is one action's contribution: only what decides the bytes that
// land or the process that starts, never where on this machine they land, so a
// reviewer's plan and an installer's plan of the same content agree.
type digestPart struct {
	Kind    string          `json:"kind"`
	Name    string          `json:"name"`
	Source  string          `json:"source,omitempty"`
	Commit  string          `json:"commit,omitempty"`
	Content string          `json:"content,omitempty"`
	Entry   json.RawMessage `json:"entry,omitempty"`
}

// contentDigest fingerprints material captured by a plan. Copied plugins hash
// their snapshot; linked trees and uncaptured local skills remain unpinnable.
func contentDigest(actions []action) string {
	if len(actions) == 0 {
		return ""
	}
	parts := make([]digestPart, 0, len(actions))
	for _, a := range actions {
		part, ok := digestPartFor(a)
		if !ok {
			return ""
		}
		parts = append(parts, part)
	}
	sort.Slice(parts, func(i, j int) bool {
		if parts[i].Kind != parts[j].Kind {
			return parts[i].Kind < parts[j].Kind
		}
		return parts[i].Name < parts[j].Name
	})
	body, _ := json.Marshal(parts)
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func digestPartFor(a action) (digestPart, bool) {
	switch {
	case a.Kind == "skill" && a.Action == "copy_skill":
		if a.skill.IsDir || a.skill.Content == "" || !isURL(a.skill.SourcePath) {
			return digestPart{}, false
		}
		sum := sha256.Sum256([]byte(a.skill.Content))
		return digestPart{Kind: a.Kind, Name: a.Name, Content: hex.EncodeToString(sum[:])}, true
	case a.Kind == "plugin" && a.Action == "install_plugin_package":
		commit := strings.ToLower(strings.TrimSpace(a.Commit))
		if a.Mode == "link" {
			return digestPart{}, false
		}
		if a.treeDigest != "" {
			return digestPart{Kind: a.Kind, Name: a.Name, Source: a.Source, Commit: commit, Content: a.treeDigest}, true
		}
		return digestPart{}, false
	case a.Kind == "mcp" && a.Action == "install_mcp_server":
		if !isURL(a.Source) && !LooksLikePackage(a.Source) {
			return digestPart{}, false
		}
		e := a.entry
		entry, _ := json.Marshal(struct {
			Type, Command, URL, Concurrency, Load string
			Args                                  []string
			Env, Headers                          map[string]string
			StartupTimeout, CallTimeout           int
			ToolTimeouts                          map[string]int
			AllowMissingPKCE                      bool
			AutoStart                             *bool
		}{e.Type, e.Command, e.URL, e.Concurrency, e.Load, e.Args, e.Env, e.Headers,
			e.StartupTimeoutSeconds, e.CallTimeoutSeconds, e.ToolTimeoutSeconds,
			e.OAuthAllowMissingPKCEMetadata, e.AutoStart})
		return digestPart{Kind: a.Kind, Name: e.Name, Entry: entry}, true
	}
	return digestPart{}, false
}

// checkExpectedDigest refuses a plan whose material is not the pinned one. Both
// the preview and the apply of one Execute pass through it, and apply writes the
// material its own plan read, so the bytes checked are the bytes installed.
func checkExpectedDigest(expected string, actions []action) error {
	if expected == "" {
		return nil
	}
	if !IsContentDigest(expected) {
		return newErr(ErrNotPinnable, "expected digest %q is not sha256:<64 hex>", expected)
	}
	got := contentDigest(actions)
	if got == "" {
		return newErr(ErrNotPinnable, "this source resolves to material that cannot be pinned by content")
	}
	if got != expected {
		return newErr(ErrDigestMismatch, "source now resolves to %s, the pinned version is %s", got, expected)
	}
	return nil
}
