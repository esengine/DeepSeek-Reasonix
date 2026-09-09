package skill

import (
	"fmt"
	"strings"

	"reasonix/internal/textutil"
)

// IndexMaxChars caps the session-context skills catalog; bodies never enter it.
const IndexMaxChars = 4000

const missingDescPlaceholder = `(no description — frontmatter is missing a "description:" line; tell the user to add one)`

// indexHeader is the cache-stable invocation policy. The dynamic catalog is
// delivered independently in the latest host-generated session-context.
const indexHeader = "# Skills — playbooks you can invoke\n\n" +
	"The latest host-generated `<session-context>` contains the current one-line skills catalog. Before non-trivial work, scan it: if an untagged (inline) skill is even plausibly relevant to the task, invoke it before continuing instead of pre-judging — loading one imperfect inline skill is cheap. Skills tagged `[🧬 subagent]` are the heavy path; reach for them only when the task genuinely needs context-heavy work, not on weak relevance. Each entry is a built-in or a user-authored playbook. Call `run_skill({ name: \"<skill-name>\", arguments: \"<task>\" })` — `name` is JUST the identifier, NOT the `[🧬 subagent]` tag that follows it. Prefer the dedicated top-level tool when one exists for a built-in subagent skill. Entries tagged `[🧬 subagent]` spawn an isolated subagent — its tool calls and reasoning never enter your context, only its final answer does; use them for context-heavy work (deep exploration, multi-step research) where you only need the conclusion. Untagged skills are inlined: the body becomes a tool result you read and act on directly. The user can also invoke a skill via `/<name>`."

const readOnlyIndexHeader = "# Skills — read-only playbooks you can invoke\n\n" +
	"The latest host-generated `<session-context>` contains the current one-line catalog for this narrow read-only skill surface. Call `read_only_skill({ name: \"<skill-name>\", arguments: \"<task>\" })` — `name` is JUST the identifier, NOT the `[🧬 subagent]` tag. Inline skills are loaded into context. Skills tagged `[🧬 subagent]` run in an isolated ephemeral read-only subagent with only read-only research tools and safe foreground bash; no writes, installers, memory mutation, continuation/fork, background jobs, or writer-capable delegation are available. Read-only nested delegation may be available until max_subagent_depth is reached."

// InvocationPolicyBlock is the stable executor policy without catalog entries.
func InvocationPolicyBlock() string { return indexHeader }

// ReadOnlyInvocationPolicyBlock is the stable planner policy without catalog entries.
func ReadOnlyInvocationPolicyBlock() string { return readOnlyIndexHeader }

// CatalogBlock renders only dynamic names, descriptions, and run tags.
func CatalogBlock(skills []Skill) string { return catalogBlock(skills) }

// ReadOnlyCatalogBlock currently has the same entries as CatalogBlock; the
// planner-specific invocation semantics remain in ReadOnlyInvocationPolicyBlock.
func ReadOnlyCatalogBlock(skills []Skill) string { return catalogBlock(skills) }

// IndexBlock renders the system/tool-result skills listing without attaching it
// to a base prompt. Only names + descriptions (+ a subagent tag) are listed;
// bodies load on demand via run_skill.
func IndexBlock(skills []Skill) string {
	return indexBlockWithHeader(indexHeader, skills)
}

// ReadOnlyIndexBlock renders the same listing with read_only_skill-specific
// invocation guidance for token-economy plan-mode connections.
func ReadOnlyIndexBlock(skills []Skill) string {
	return indexBlockWithHeader(readOnlyIndexHeader, skills)
}

func indexBlockWithHeader(header string, skills []Skill) string {
	catalog := catalogBlock(skills)
	if catalog == "" {
		return ""
	}
	return header + "\n\n" + catalog
}

func catalogBlock(skills []Skill) string {
	if len(skills) == 0 {
		return ""
	}
	lines := make([]string, 0, len(skills))
	for _, sk := range skills {
		// Manual-invocation skills (e.g. user-authored subagent profiles) stay
		// invocable by name (/<name>, run_skill) but must never enter the
		// session-context catalog the model scans for candidates on its own
		// initiative.
		if sk.Invocation == "manual" {
			continue
		}
		lines = append(lines, indexLine(sk))
	}
	if len(lines) == 0 {
		return ""
	}
	joined := strings.Join(lines, "\n")
	if r := []rune(joined); len(r) > IndexMaxChars {
		joined = string(r[:IndexMaxChars]) + fmt.Sprintf("\n… (truncated %d chars)", len(r)-IndexMaxChars)
	}
	return "```\n" + joined + "\n```"
}

// ApplyIndex appends the skills index to basePrompt, or returns it unchanged
// when there are no skills. Only names + descriptions (+ a subagent tag) are
// listed; bodies load on demand via run_skill.
func ApplyIndex(basePrompt string, skills []Skill) string {
	block := IndexBlock(skills)
	if block == "" {
		return basePrompt
	}
	return basePrompt + "\n\n" + block
}

// indexLine renders one skill as "- name [tag] — description", clipped to a
// stable width. The subagent tag goes after the name so a model copying the line
// into run_skill's `name` arg still yields a clean identifier.
func indexLine(sk Skill) string {
	desc := strings.TrimSpace(strings.ReplaceAll(sk.Description, "\n", " "))
	if desc == "" {
		desc = missingDescPlaceholder
	}
	tag := ""
	if sk.RunAs == RunSubagent {
		tag = " [🧬 subagent]"
	}
	max := 130 - len([]rune(sk.Name)) - len([]rune(tag))
	clipped := clipRunes(desc, max)
	if clipped == "" {
		return "- " + sk.Name + tag
	}
	return "- " + sk.Name + tag + " — " + clipped
}

// clipRunes preserves the historical name but clips by grapheme clusters so
// combined emoji and other user-visible characters stay intact.
func clipRunes(s string, max int) string {
	return textutil.ClipGraphemes(s, max, "…")
}
