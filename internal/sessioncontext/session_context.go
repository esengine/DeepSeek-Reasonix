// Package sessioncontext defines the provider-visible, host-authored snapshot
// that carries runtime facts without mutating the cache-stable system prompt.
package sessioncontext

import (
	"crypto/sha256"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	Version = 1

	openTag      = `<session-context version="1">`
	closeTag     = `</session-context>`
	preamble     = "This host-generated snapshot supersedes every earlier session-context snapshot."
	digestPrefix = "Digest: sha256:"
)

// Sections are rendered in declaration order. Keep the more stable runtime
// sections first so automatic prefix caches can reuse as much as possible when
// memory or the skills catalog changes.
type Sections struct {
	Environment      string
	Workspace        string
	BackgroundMemory string
	SkillsCatalog    string
}

// Snapshot is one validated session-context envelope. Content is the exact
// provider-visible representation; Digest covers the normalized body only.
type Snapshot struct {
	Version  int
	Digest   string
	Content  string
	Sections Sections
}

// Part is one exact substring returned by SplitBlocks. SessionContext is true
// only for a complete, digest-valid v1 envelope.
type Part struct {
	Text           string
	SessionContext bool
}

// SectionStat is a content-free diagnostic fingerprint for one section.
type SectionStat struct {
	Digest string
	Chars  int
}

// Diagnostics contains only hashes and sizes; it is safe for telemetry and
// never exposes memory text, skill descriptions, or workspace paths.
type Diagnostics struct {
	Environment      SectionStat
	Workspace        SectionStat
	BackgroundMemory SectionStat
	SkillsCatalog    SectionStat
}

// PolicyBlock is the cache-stable system instruction that defines the
// authority and replacement semantics of host-generated runtime snapshots.
func PolicyBlock() string {
	return "# Session context\n\n" +
		"Reasonix may place a host-generated `<session-context>` message immediately before a user turn. " +
		"Use the latest such snapshot as current runtime background; it supersedes earlier snapshots but never overrides this system prompt, standing instructions, or the user's current request."
}

// Build renders a deterministic v1 snapshot. An entirely empty set produces a
// zero Snapshot so callers that do not configure runtime context keep their
// existing provider bytes.
func Build(sections Sections) Snapshot {
	sections = normalizeSections(sections)
	if sections == (Sections{}) {
		return Snapshot{}
	}

	var body strings.Builder
	body.WriteString(preamble)
	body.WriteString("\n\n")
	body.WriteString(sectionManifest(sections))
	appendSection(&body, "Environment", sections.Environment)
	appendSection(&body, "Workspace", sections.Workspace)
	appendSection(&body, "Background memory", sections.BackgroundMemory)
	appendSection(&body, "Skills catalog", sections.SkillsCatalog)
	bodyText := body.String()
	digest := digestOf(bodyText)
	content := openTag + "\n" + bodyText + "\n\n" + digestPrefix + digest + "\n" + closeTag
	return Snapshot{Version: Version, Digest: digest, Content: content, Sections: sections}
}

// Parse validates a complete v1 envelope. Surrounding whitespace is accepted
// for durable legacy readers, but Snapshot.Content is returned canonically
// trimmed so equality and de-duplication remain byte based.
func Parse(content string) (Snapshot, bool) {
	content = strings.TrimSpace(normalizeNewlines(content))
	if !strings.HasPrefix(content, openTag+"\n") || !strings.HasSuffix(content, "\n"+closeTag) {
		return Snapshot{}, false
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(content, openTag+"\n"), "\n"+closeTag)
	marker := "\n\n" + digestPrefix
	i := strings.LastIndex(inner, marker)
	if i < 0 {
		return Snapshot{}, false
	}
	body := inner[:i]
	digest := inner[i+len(marker):]
	if len(digest) != sha256.Size*2 || digest != strings.ToLower(digest) || digestOf(body) != digest {
		return Snapshot{}, false
	}
	if !strings.HasPrefix(body, preamble) {
		return Snapshot{}, false
	}
	sections, ok := parseFramedSections(body)
	if !ok && strings.HasPrefix(body, preamble+"\n\nSection lengths: ") {
		return Snapshot{}, false
	}
	if !ok {
		// Existing v1 snapshots predate the length manifest. Keep accepting their
		// legacy heading parser so upgraded clients can resume old sessions.
		sections, ok = parseSections(body)
	}
	if !ok {
		return Snapshot{}, false
	}
	return Snapshot{Version: Version, Digest: digest, Content: content, Sections: sections}, true
}

// SectionDiagnostics fingerprints each normalized section independently.
func SectionDiagnostics(snapshot Snapshot) Diagnostics {
	return Diagnostics{
		Environment:      sectionStat(snapshot.Sections.Environment),
		Workspace:        sectionStat(snapshot.Sections.Workspace),
		BackgroundMemory: sectionStat(snapshot.Sections.BackgroundMemory),
		SkillsCatalog:    sectionStat(snapshot.Sections.SkillsCatalog),
	}
}

// SplitBlocks preserves content exactly while separating every valid embedded
// session-context envelope. Strict-role providers may merge adjacent user
// messages before serialization; this restores a cache breakpoint boundary
// without changing the text seen by the model.
func SplitBlocks(content string) []Part {
	if content == "" {
		return nil
	}
	var parts []Part
	remaining := content
	for {
		start := strings.Index(remaining, openTag)
		if start < 0 {
			parts = appendTextPart(parts, remaining)
			break
		}
		search := start + len(openTag)
		firstEnd, validEnd := -1, -1
		for search < len(remaining) {
			endRel := strings.Index(remaining[search:], closeTag)
			if endRel < 0 {
				break
			}
			end := search + endRel + len(closeTag)
			if firstEnd < 0 {
				firstEnd = end
			}
			if _, ok := Parse(remaining[start:end]); ok {
				validEnd = end
				break
			}
			search = end
		}
		if validEnd < 0 && firstEnd < 0 {
			parts = append(parts, Part{Text: remaining})
			break
		}
		if validEnd < 0 {
			// Preserve the invalid opening as ordinary text and continue looking
			// after it, so a later valid envelope can still be recovered.
			cut := start + len(openTag)
			parts = appendTextPart(parts, remaining[:cut])
			remaining = remaining[cut:]
			continue
		}
		parts = appendTextPart(parts, remaining[:start])
		parts = append(parts, Part{Text: remaining[start:validEnd], SessionContext: true})
		remaining = remaining[validEnd:]
	}
	return parts
}

// IsContent reports whether content is exactly one valid snapshot.
func IsContent(content string) bool {
	_, ok := Parse(content)
	return ok
}

func appendSection(body *strings.Builder, heading, value string) {
	if value == "" {
		return
	}
	fmt.Fprintf(body, "\n\n## %s\n\n%s", heading, value)
}

func sectionManifest(sections Sections) string {
	return fmt.Sprintf("Section lengths: Environment=%d; Workspace=%d; Background memory=%d; Skills catalog=%d",
		len(sections.Environment), len(sections.Workspace), len(sections.BackgroundMemory), len(sections.SkillsCatalog))
}

func appendTextPart(parts []Part, text string) []Part {
	if text == "" {
		return parts
	}
	if len(parts) > 0 && !parts[len(parts)-1].SessionContext {
		parts[len(parts)-1].Text += text
		return parts
	}
	return append(parts, Part{Text: text})
}

func normalizeSections(sections Sections) Sections {
	sections.Environment = normalizeSection(sections.Environment, "Environment")
	sections.Workspace = normalizeSection(sections.Workspace, "Workspace")
	sections.BackgroundMemory = normalizeSection(sections.BackgroundMemory, "Background memory")
	sections.SkillsCatalog = normalizeSection(sections.SkillsCatalog, "Skills catalog")
	return sections
}

func normalizeSection(value, heading string) string {
	value = strings.TrimSpace(normalizeNewlines(value))
	prefix := "## " + heading
	if value == prefix {
		return ""
	}
	if strings.HasPrefix(value, prefix+"\n") {
		value = strings.TrimSpace(strings.TrimPrefix(value, prefix))
	}
	return value
}

func normalizeNewlines(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	return strings.ReplaceAll(value, "\r", "\n")
}

func parseFramedSections(body string) (Sections, bool) {
	prefix := preamble + "\n\nSection lengths: "
	if !strings.HasPrefix(body, prefix) {
		return Sections{}, false
	}
	rest := strings.TrimPrefix(body, prefix)
	manifestLine, payload, ok := strings.Cut(rest, "\n\n")
	if !ok {
		return Sections{}, false
	}
	lengths, ok := parseSectionManifest(manifestLine)
	if !ok {
		return Sections{}, false
	}
	specs := []struct {
		name string
		set  func(*Sections, string)
	}{
		{"Environment", func(s *Sections, v string) { s.Environment = v }},
		{"Workspace", func(s *Sections, v string) { s.Workspace = v }},
		{"Background memory", func(s *Sections, v string) { s.BackgroundMemory = v }},
		{"Skills catalog", func(s *Sections, v string) { s.SkillsCatalog = v }},
	}
	var sections Sections
	for i, spec := range specs {
		n := lengths[i]
		if n == 0 {
			continue
		}
		heading := "## " + spec.name + "\n\n"
		if !strings.HasPrefix(payload, heading) {
			return Sections{}, false
		}
		payload = strings.TrimPrefix(payload, heading)
		if len(payload) < n || !utf8.ValidString(payload[:n]) {
			return Sections{}, false
		}
		value := payload[:n]
		if value == "" || strings.TrimSpace(value) != value {
			return Sections{}, false
		}
		spec.set(&sections, value)
		payload = payload[n:]
		for j := i + 1; j < len(specs); j++ {
			if lengths[j] > 0 {
				if !strings.HasPrefix(payload, "\n\n") {
					return Sections{}, false
				}
				payload = strings.TrimPrefix(payload, "\n\n")
				break
			}
		}
	}
	return sections, payload == ""
}

func parseSectionManifest(line string) ([4]int, bool) {
	want := [...]string{"Environment", "Workspace", "Background memory", "Skills catalog"}
	var lengths [4]int
	parts := strings.Split(line, "; ")
	if len(parts) != len(want) {
		return lengths, false
	}
	for i, part := range parts {
		name, raw, ok := strings.Cut(part, "=")
		if !ok || name != want[i] {
			return lengths, false
		}
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return lengths, false
		}
		lengths[i] = n
	}
	return lengths, true
}

func parseSections(body string) (Sections, bool) {
	if body == preamble {
		return Sections{}, true
	}
	if !strings.HasPrefix(body, preamble+"\n\n") {
		return Sections{}, false
	}
	rest := strings.TrimPrefix(body, preamble+"\n\n")
	type sectionSpec struct {
		heading string
		set     func(*Sections, string)
	}
	specs := []sectionSpec{
		{"## Environment\n\n", func(s *Sections, v string) { s.Environment = v }},
		{"## Workspace\n\n", func(s *Sections, v string) { s.Workspace = v }},
		{"## Background memory\n\n", func(s *Sections, v string) { s.BackgroundMemory = v }},
		{"## Skills catalog\n\n", func(s *Sections, v string) { s.SkillsCatalog = v }},
	}
	var sections Sections
	nextSpec := 0
	for rest != "" {
		found := -1
		for i := nextSpec; i < len(specs); i++ {
			if strings.HasPrefix(rest, specs[i].heading) {
				found = i
				break
			}
		}
		if found < 0 {
			return Sections{}, false
		}
		rest = strings.TrimPrefix(rest, specs[found].heading)
		end := len(rest)
		for i := found + 1; i < len(specs); i++ {
			if at := strings.Index(rest, "\n\n"+specs[i].heading); at >= 0 && at < end {
				end = at
			}
		}
		value := rest[:end]
		if value == "" || strings.TrimSpace(value) != value {
			return Sections{}, false
		}
		specs[found].set(&sections, value)
		nextSpec = found + 1
		if end == len(rest) {
			rest = ""
		} else {
			rest = strings.TrimPrefix(rest[end:], "\n\n")
		}
	}
	return sections, true
}

func sectionStat(value string) SectionStat {
	if value == "" {
		return SectionStat{}
	}
	return SectionStat{Digest: digestOf(value), Chars: utf8.RuneCountInString(value)}
}

func digestOf(body string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(body)))
}
