package hook

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	fileencoding "reasonix/internal/fileutil/encoding"
	"reasonix/internal/pluginpkg"
)

// SourceStatus describes one hooks settings file for diagnostics.
type SourceStatus struct {
	Scope      Scope
	Path       string
	Status     string // ok | missing | malformed | empty
	HookCount  int
	ParseError string
}

// Entry is one configured hook with diagnostic annotations.
type Entry struct {
	Event       Event
	Match       string
	Command     string
	ContextFile string
	Description string
	Timeout     int
	Scope       Scope
	Source      string
	Issues      []string // stable codes attached at collect time (optional)
}

// Inspection is a read-only hook configuration snapshot. It does not execute
// hooks.
type Inspection struct {
	// TrustedProject is retained in diagnostics for backward compatibility.
	// A non-empty project root is always trusted now.
	TrustedProject bool
	Sources        []SourceStatus
	Entries        []Entry
	// ProjectDefines is true when project settings declare hooks.
	ProjectDefines bool
}

// Inspect loads hook configuration for diagnostics. Unlike Load, it reports
// malformed files and empty/missing command entries that Load would silently
// skip.
func Inspect(opts LoadOptions) Inspection {
	out := Inspection{
		TrustedProject: opts.ProjectRoot != "",
		ProjectDefines: opts.ProjectRoot != "" && ProjectDefinesHooks(opts.ProjectRoot),
	}

	if opts.ProjectRoot != "" {
		p := ProjectSettingsPath(opts.ProjectRoot)
		st := inspectSettingsFile(p, ScopeProject)
		out.Sources = append(out.Sources, st)
		if s := readSettingsRaw(p); s != nil {
			appendInspectEntries(&out, s, ScopeProject, p)
		}
	}

	// Plugin hooks (enabled packages only — same as Load).
	appendPluginInspect(&out, reasonixHome(opts.HomeDir), opts.ProjectRoot)

	g := GlobalSettingsPath(opts.HomeDir)
	st := inspectSettingsFile(g, ScopeGlobal)
	if st.Status == "missing" {
		if legacy := legacyGlobalSettingsPath(opts.HomeDir); legacy != "" {
			if pathExists(legacy) {
				g = legacy
				st = inspectSettingsFile(g, ScopeGlobal)
			}
		}
	}
	out.Sources = append(out.Sources, st)
	if s := readSettingsRaw(g); s != nil {
		appendInspectEntries(&out, s, ScopeGlobal, g)
	}

	return out
}

func inspectSettingsFile(path string, scope Scope) SourceStatus {
	st := SourceStatus{Scope: scope, Path: path}
	if strings.TrimSpace(path) == "" {
		st.Status = "missing"
		return st
	}
	b, err := fileencoding.ReadFileUTF8(path)
	if err != nil {
		if os.IsNotExist(err) {
			st.Status = "missing"
			return st
		}
		st.Status = "unreadable"
		st.ParseError = err.Error()
		return st
	}
	var s Settings
	if err := json.Unmarshal(b, &s); err != nil {
		st.Status = "malformed"
		st.ParseError = err.Error()
		return st
	}
	count := 0
	for _, hooks := range s.Hooks {
		count += len(hooks)
	}
	st.HookCount = count
	if count == 0 {
		st.Status = "empty"
	} else {
		st.Status = "ok"
	}
	return st
}

func readSettingsRaw(path string) *Settings {
	return readSettings(path)
}

func appendInspectEntries(out *Inspection, s *Settings, scope Scope, source string) {
	if s == nil || s.Hooks == nil {
		return
	}
	// Include every map key so misspelled event names remain diagnosable
	// (hook.unknown_event). Known events first (stable product order), then
	// remaining keys sorted alphabetically.
	seen := map[Event]bool{}
	for _, event := range Events {
		if hooks, ok := s.Hooks[event]; ok {
			seen[event] = true
			for _, cfg := range hooks {
				out.Entries = append(out.Entries, Entry{
					Event:       event,
					Match:       cfg.Match,
					Command:     cfg.Command,
					ContextFile: cfg.ContextFile,
					Description: cfg.Description,
					Timeout:     cfg.Timeout,
					Scope:       scope,
					Source:      source,
				})
			}
		}
	}
	var unknown []Event
	for event := range s.Hooks {
		if !seen[event] {
			unknown = append(unknown, event)
		}
	}
	sort.Slice(unknown, func(i, j int) bool { return unknown[i] < unknown[j] })
	for _, event := range unknown {
		for _, cfg := range s.Hooks[event] {
			out.Entries = append(out.Entries, Entry{
				Event:       event,
				Match:       cfg.Match,
				Command:     cfg.Command,
				ContextFile: cfg.ContextFile,
				Description: cfg.Description,
				Timeout:     cfg.Timeout,
				Scope:       scope,
				Source:      source,
			})
		}
	}
}

func appendPluginInspect(out *Inspection, reasonixHomeDir, projectRoot string) {
	if strings.TrimSpace(reasonixHomeDir) == "" {
		return
	}
	installed, _ := pluginpkg.LoadInstalled(reasonixHomeDir)
	for _, item := range installed {
		pkg := item.Package
		src := filepath.Join(pkg.Root, pluginpkg.ManifestPath(pkg.ManifestKind))
		count := 0
		events := make([]string, 0, len(pkg.Manifest.Hooks))
		for event := range pkg.Manifest.Hooks {
			events = append(events, event)
		}
		sort.Strings(events)
		for _, eventName := range events {
			event := Event(eventName)
			// Keep unknown event names so diagnostics can report them.
			for _, h := range pkg.Manifest.Hooks[eventName] {
				count++
				command := expandPluginRoot(h.Command, pkg.Root)
				if command != "" && !h.ShellCommand && !filepath.IsAbs(command) {
					command = filepath.Join(pkg.Root, filepath.FromSlash(command))
				}
				contextFile := expandPluginRoot(h.ContextFile, pkg.Root)
				if contextFile != "" {
					contextFile = filepath.FromSlash(contextFile)
					if !filepath.IsAbs(contextFile) {
						contextFile = filepath.Join(pkg.Root, contextFile)
					} else {
						contextFile = filepath.Clean(contextFile)
					}
				}
				out.Entries = append(out.Entries, Entry{
					Event:       event,
					Match:       h.Match,
					Command:     command,
					ContextFile: contextFile,
					Description: h.Description,
					Timeout:     h.Timeout,
					Scope:       ScopePlugin,
					Source:      src,
				})
			}
		}
		status := "ok"
		if count == 0 {
			status = "empty"
		}
		out.Sources = append(out.Sources, SourceStatus{
			Scope:     ScopePlugin,
			Path:      src,
			Status:    status,
			HookCount: count,
		})
		_ = projectRoot
	}
}

// ValidateMatcher returns an error string when match is an invalid anchored regex.
// Empty and "*" are valid (match all).
func ValidateMatcher(match string) string {
	m := strings.TrimSpace(match)
	if m == "" || m == "*" {
		return ""
	}
	if _, err := regexp.Compile("^(?:" + m + ")$"); err != nil {
		return fmt.Sprintf("invalid matcher regex: %v", err)
	}
	return ""
}

// UsesToolMatcher reports whether an event evaluates HookConfig.Match.
// Non-tool events ignore matchers entirely, including malformed ones.
func UsesToolMatcher(event Event) bool {
	return event == PreToolUse || event == PostToolUse || event == PostToolUseFailure || event == PermissionRequest
}

// IsKnownEvent reports whether event is one of the 11 supported hook events.
func IsKnownEvent(event string) bool {
	return validEvent(Event(event))
}
