package skill

import (
	"fmt"
	"strings"
)

// IndexMaxChars caps the pinned skills-index block so it can't bloat the
// cache-stable system-prompt prefix; bodies never enter the prefix.
const IndexMaxChars = 4000

const missingDescPlaceholder = `(no description — frontmatter is missing a "description:" line; tell the user to add one)`

// indexHeader introduces the skills block in the system prompt: the invocation
// policy (mandatory for inline, judgment-based for subagent) and how to call one.
const indexHeader = "# Skills — playbooks you can invoke\n\n" +
	"One-liner index. Before non-trivial work, scan it: if an untagged (inline) skill is even plausibly relevant to the task, invoke it before continuing instead of pre-judging — loading one imperfect inline skill is cheap. Skills tagged `[🧬 subagent]` are the heavy path; reach for them only when the task genuinely needs context-heavy work, not on weak relevance. Each entry is a built-in or a user-authored playbook. Call `run_skill({ name: \"<skill-name>\", arguments: \"<task>\" })` — `name` is JUST the identifier (e.g. `\"explore\"`), NOT the `[🧬 subagent]` tag that follows it. Prefer the dedicated top-level tool when one exists for a built-in subagent skill. Entries tagged `[🧬 subagent]` spawn an isolated subagent — its tool calls and reasoning never enter your context, only its final answer does; use them for context-heavy work (deep exploration, multi-step research) where you only need the conclusion. Untagged skills are inlined: the body becomes a tool result you read and act on directly. The user can also invoke a skill via `/<name>`."

const readOnlyIndexHeader = "# Skills — read-only playbooks you can invoke\n\n" +
	"One-liner index for the narrow read-only skill surface. Call `read_only_skill({ name: \"<skill-name>\", arguments: \"<task>\" })` — `name` is JUST the identifier, NOT the `[🧬 subagent]` tag. Inline skills are loaded into context. Skills tagged `[🧬 subagent]` run in an isolated ephemeral read-only subagent with only read-only research tools and safe foreground bash; no writes, installers, memory mutation, continuation/fork, background jobs, or further delegation are available."

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
	if len(skills) == 0 {
		return ""
	}
	lines := make([]string, 0, len(skills))
	for _, sk := range skills {
		lines = append(lines, indexLine(sk))
	}
	joined := strings.Join(lines, "\n")
	if r := []rune(joined); len(r) > IndexMaxChars {
		joined = string(r[:IndexMaxChars]) + fmt.Sprintf("\n… (truncated %d chars)", len(r)-IndexMaxChars)
	}
	return header + "\n\n```\n" + joined + "\n```"
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

// clipRunes truncates s to at most max runes (ellipsis included), never
// splitting a multi-byte rune.
func clipRunes(s string, max int) string {
	if max < 1 {
		max = 1
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max-1 < 1 {
		return string(r[:1])
	}
	return string(r[:max-1]) + "…"
}
