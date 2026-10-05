package skill

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"reasonix/internal/base/textutil"
)

// IndexMaxChars caps the skills listing so a large catalog cannot dominate the
// turn it rides on; past it the listing only names skills and the capability search describes them.
const IndexMaxChars = 4000

const missingDescPlaceholder = `(no description — frontmatter is missing a "description:" line; tell the user to add one)`

// indexHeader introduces the skills block: the invocation policy (mandatory for
// inline, judgment-based for subagent) and how to call one.
const indexHeader = "# Skills — playbooks you can invoke\n\n" +
	"One-liner index. Before non-trivial work, scan it: if an untagged (inline) skill is even plausibly relevant to the task, invoke it before continuing instead of pre-judging — loading one imperfect inline skill is cheap. Skills tagged `[🧬 subagent]` are the heavy path; reach for them only when the task genuinely needs context-heavy work, not on weak relevance. Each entry is a built-in or a user-authored playbook. Call `run_skill({ name: \"<skill-name>\", arguments: \"<task>\" })` — `name` is JUST the identifier (e.g. `\"explore\"`), NOT the `[🧬 subagent]` tag that follows it. Prefer the dedicated top-level tool when one exists for a built-in subagent skill. Entries tagged `[🧬 subagent]` spawn an isolated subagent — its tool calls and reasoning never enter your context, only its final answer does; use them for context-heavy work (deep exploration, multi-step research) where you only need the conclusion. Untagged skills are inlined: the body becomes a tool result you read and act on directly. The user can also invoke a skill via `/<name>`."

const readOnlyIndexHeader = "# Skills — read-only playbooks you can invoke\n\n" +
	"One-liner index for the narrow read-only skill surface. Call `read_only_skill({ name: \"<skill-name>\", arguments: \"<task>\" })` — `name` is JUST the identifier, NOT the `[🧬 subagent]` tag. Inline skills are loaded into context. Skills tagged `[🧬 subagent]` run in an isolated ephemeral read-only subagent with only read-only research tools and safe foreground bash; no writes, installers, memory mutation, continuation/fork, background jobs, or writer-capable delegation are available. Read-only nested delegation may be available until max_subagent_depth is reached."

// IndexBlock renders the skills listing the controller owes a turn. Only names +
// descriptions (+ a subagent tag) are listed; bodies load on demand via run_skill.
func IndexBlock(skills []Skill) string {
	return indexBlockWithHeader(indexHeader, skills)
}

// ReadOnlyIndexBlock renders the same listing with read_only_skill-specific
// invocation guidance for token-economy plan-mode connections.
func ReadOnlyIndexBlock(skills []Skill) string {
	return indexBlockWithHeader(readOnlyIndexHeader, skills)
}

func indexBlockWithHeader(header string, skills []Skill) string {
	listed := modelListed(skills)
	if len(listed) == 0 {
		return ""
	}
	lines := make([]string, len(listed))
	for i, sk := range listed {
		lines[i] = indexLine(sk)
	}
	if joined := strings.Join(lines, "\n"); runeLen(joined) <= IndexMaxChars {
		return header + "\n\n```\n" + joined + "\n```"
	}
	return header + "\n\n" + fmt.Sprintf(namesOnlyNote, len(listed)) + "\n\n```\n" + namesWithin(listed) + "\n```"
}

// namesOnlyNote replaces descriptions when they would not fit: every skill stays
// reachable by name, and the capability search answers what a name leaves out.
const namesOnlyNote = "This project has %d skills, more than this listing can describe, so it names them without descriptions. " +
	"To learn what a skill is for, search by the task: `use_capability({ action: \"search\", query: \"<what you need>\" })` — skills come back as `skill:<name>` with their descriptions."

// modelListed drops manual-invocation skills (e.g. user-authored subagent
// profiles): they stay invocable by name (/<name>, run_skill) but never enter
// what the model scans for candidates to call on its own initiative.
func modelListed(skills []Skill) []Skill {
	out := make([]Skill, 0, len(skills))
	for _, sk := range skills {
		if sk.Invocation != "manual" && !sk.DisableModelInvocation {
			out = append(out, sk)
		}
	}
	return out
}

// namesWithin lists names whole-line until IndexMaxChars and counts the rest,
// so a cut never leaves half an identifier for the model to call.
func namesWithin(skills []Skill) string {
	lines := make([]string, 0, len(skills))
	used := 0
	for i, sk := range skills {
		line := "- " + sk.Name + subagentTag(sk)
		if i > 0 && used+1+runeLen(line) > IndexMaxChars {
			lines = append(lines, fmt.Sprintf("… %d more not named here; use_capability search reaches every skill", len(skills)-i))
			break
		}
		lines = append(lines, line)
		used += 1 + runeLen(line)
	}
	return strings.Join(lines, "\n")
}

func runeLen(s string) int { return utf8.RuneCountInString(s) }

func subagentTag(sk Skill) string {
	if sk.RunAs == RunSubagent {
		return " [🧬 subagent]"
	}
	return ""
}

// indexLine renders one skill as "- name [tag] — description", clipped to a
// stable width. The subagent tag goes after the name so a model copying the line
// into run_skill's `name` arg still yields a clean identifier.
func indexLine(sk Skill) string {
	desc := strings.TrimSpace(strings.ReplaceAll(sk.Description, "\n", " "))
	if desc == "" {
		desc = missingDescPlaceholder
	}
	tag := subagentTag(sk)
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
