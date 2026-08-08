package evolve

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"reasonix/internal/secrets"
)

var emailPattern = regexp.MustCompile(`(?i)\b[a-z0-9.!#$%&'*+/=?^_` + "`" + `{|}~-]+@[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+\b`)

// Validate checks a proposal is safe and complete enough to apply.
// Already-applied proposals pass with nil so Apply can no-op.
func Validate(p Proposal) error {
	if strings.TrimSpace(p.ID) == "" {
		return fmt.Errorf("proposal id is required")
	}
	status := NormalizeStatus(string(p.Status))
	if status == StatusApplied {
		return nil
	}
	if status == StatusDiscarded {
		return fmt.Errorf("proposal %s is discarded", p.ID)
	}
	tier := NormalizeTier(string(p.Tier))
	if tier != TierL0 && tier != TierL1 {
		return fmt.Errorf("unsupported tier %q", p.Tier)
	}
	if strings.TrimSpace(p.Title) == "" {
		return fmt.Errorf("title is required")
	}
	if strings.TrimSpace(p.Why) == "" {
		return fmt.Errorf("why is required")
	}
	if strings.TrimSpace(p.HowToApply) == "" {
		return fmt.Errorf("how_to_apply is required")
	}
	if err := validateEvidence(p.Evidence); err != nil {
		return err
	}
	if sensitive(proposalText(p)) {
		return fmt.Errorf("proposal may contain sensitive information")
	}
	switch tier {
	case TierL0:
		return validateL0(p)
	case TierL1:
		return validateL1(p)
	default:
		return fmt.Errorf("unsupported tier %q", tier)
	}
}

func validateEvidence(ev []Evidence) error {
	if len(ev) == 0 {
		return fmt.Errorf("at least one evidence entry is required")
	}
	for i, e := range ev {
		if strings.TrimSpace(e.SessionPath) == "" {
			return fmt.Errorf("evidence[%d].session_path is required", i)
		}
		if e.MessageIndex < 0 {
			return fmt.Errorf("evidence[%d].message_index must be >= 0", i)
		}
	}
	return nil
}

func validateL0(p Proposal) error {
	kind := strings.ToLower(strings.TrimSpace(string(p.Target.Kind)))
	if kind != "" && kind != string(TargetMemory) {
		return fmt.Errorf("L0 target kind must be memory, got %q", p.Target.Kind)
	}
	scope := strings.ToLower(strings.TrimSpace(p.Target.MemoryScope))
	if scope == "global" {
		return fmt.Errorf("global memory scope requires an explicit non-evolve path")
	}
	mt := strings.ToLower(strings.TrimSpace(p.Target.MemoryType))
	if mt != "" && mt != "feedback" && mt != "project" {
		return fmt.Errorf("L0 memory_type must be feedback or project, got %q", p.Target.MemoryType)
	}
	if strings.TrimSpace(p.Body) == "" && (strings.TrimSpace(p.Why) == "" || strings.TrimSpace(p.HowToApply) == "") {
		return fmt.Errorf("L0 body or why/how_to_apply is required")
	}
	return nil
}

func validateL1(p Proposal) error {
	kind := strings.ToLower(strings.TrimSpace(string(p.Target.Kind)))
	if kind != "" && kind != string(TargetAgentsMD) {
		return fmt.Errorf("L1 target kind must be agents_md, got %q", p.Target.Kind)
	}
	body := strings.TrimSpace(p.Body)
	if body == "" {
		// Allow composing body from title + how_to_apply at apply time, but still
		// budget the composed form.
		body = composeL1Bullet(p.Title, p.HowToApply)
	}
	if lineCount(body) > MaxL1BodyLines {
		return fmt.Errorf("L1 body exceeds %d lines", MaxL1BodyLines)
	}
	if len([]rune(body)) > 2000 {
		return fmt.Errorf("L1 body is too large")
	}
	return nil
}

func proposalText(p Proposal) string {
	parts := []string{p.Title, p.Why, p.HowToApply, p.Body, p.Target.Description, p.Target.MemoryTitle}
	for _, e := range p.Evidence {
		parts = append(parts, e.Quote, e.SessionPath)
	}
	return strings.Join(parts, "\n")
}

func sensitive(text string) bool {
	if secrets.Redact(text) != text {
		return true
	}
	if emailPattern.MatchString(text) {
		return true
	}
	upper := strings.ToUpper(text)
	return strings.Contains(upper, "BEGIN PRIVATE KEY") || strings.Contains(upper, "BEGIN OPENSSH PRIVATE KEY")
}

func lineCount(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	n := 1
	for _, r := range s {
		if r == '\n' {
			n++
		}
	}
	return n
}

func composeL1Bullet(title, how string) string {
	title = oneLine(title)
	how = oneLine(how)
	if title == "" {
		return "- " + how
	}
	if how == "" {
		return "- **" + title + "**"
	}
	return "- **" + title + ":** " + how
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.Join(strings.Fields(s), " ")
}

// slugify turns a title into a short kebab-case memory name.
func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 48 {
		out = out[:48]
		out = strings.Trim(out, "-")
	}
	if out == "" {
		return "evolve-lesson"
	}
	return out
}
