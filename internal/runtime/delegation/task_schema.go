package delegation

import "encoding/json"

func (t *TaskTool) Name() string { return "task" }

func (t *TaskTool) Description() string {
	return "Spawn a sub-agent for a focused sub-task. Optional profile selects a runAs=subagent Skill whose body becomes the full system prompt (no implicit concise default). Optional write_paths declare non-overlapping write targets so background writers may run in parallel; give file paths when the targets are known, since a directory overlaps every path beneath it and serializes writers. Omitting write_paths on a writer reserves nothing up front and holds only the path it is writing (the default, optimistic mode); in strict mode ([agent] write_lease and subagent_claims set to strict) it claims the whole workspace and serializes writers. The sub-agent runs in its own session with a filtered tool list (defaults to every parent tool, then applies the subagent boundary: " + subagentToolBoundarySummary + "). Only its final answer is returned. A sub-agent is not a new session: you dispatch it, the person never creates one, its answer comes back into this conversation, and a sub-agent dispatching another is bounded by max_subagent_depth."
}

func (t *TaskTool) Schema() json.RawMessage {
	return t.withIsolation(json.RawMessage(`{
"type":"object",
"properties":{
  "prompt":{"type":"string","description":"What the sub-agent should accomplish. Be specific about the deliverable — the sub-agent does not see this conversation."},
  "description":{"type":"string","description":"Short label for the sub-task (3-7 words). Surfaced in the dispatch line so the user sees what's running."},
  "context_summary":{"type":"string","description":"Optional selected background and prior decisions, up to 16384 UTF-8 bytes. Sent in the opening task, never the system prompt. It grants no permissions."},
  "context_refs":{"type":"array","items":{"type":"string"},"maxItems":64,"description":"Optional completed sa_... result references from this conversation. The child receives bounded previews and a scoped reader for their full text."},
  "profile":{"type":"string","description":"Optional runAs=subagent profile name. Resolved at runtime from the Skill store; explicit names may invoke invocation=manual profiles. The profile body becomes the full system prompt."},
  "write_paths":{"type":"array","items":{"type":"string"},"description":"Optional workspace-relative or absolute file/directory paths this writer may modify. Prefer file paths when the targets are known: a directory overlaps every path beneath it and serializes writers. Globs and workspace escapes are rejected. Writers without write_paths claim the whole workspace (serializing against every other writer claim) unless write_lease is optimistic. Non-overlapping paths allow parallel writers up to max_parallel_writers. In fleet, multiple whole-workspace claims fail preflight before any task starts."},
  "tools":{"type":"array","items":{"type":"string"},"description":"Optional tool whitelist. When profile sets allowed-tools, this list is intersected (call args cannot expand profile permissions). ` + subagentToolBoundarySummary + `"},
  "max_steps":{"type":"integer","description":"Optional cap on tool-call rounds. Defaults to half the parent's cap (min 5).","minimum":1},
  "run_in_background":{"type":"boolean","description":"Run the sub-agent asynchronously: returns a job id immediately and keeps working across turns. Collect its final answer with wait, and you'll be notified when it finishes. Use for long, independent sub-tasks you don't need to block on right now."},
  "model":{"type":"string","description":"Optional model override for the sub-agent (a configured provider/model name). Precedence: persistent profile config, this argument, profile frontmatter, global subagent default, parent model."},
  "effort":{"type":"string","description":"Optional reasoning effort for the sub-agent (e.g. high, max). Same precedence as model."},
  "continue_from":{"type":"string","description":"Continue a prior compatible subagent transcript in the current conversation context. Pass only the 'sa_...' value from the prior result's 'Subagent reference: ...' line. If the ref belongs to an ancestor conversation, the framework continues a current-conversation copy."},
  "task_id":{"type":"string","description":"Send this call's prompt as guidance to a sub-agent that is still running, instead of starting a new one. Pass the 'sa_...' value from its 'Subagent reference: ...' line. The sub-agent reads it at its next tool call, framed as coming from you and not from the user; it changes nothing about what the sub-agent is allowed to do. If that sub-agent has already finished, the call continues its transcript exactly as continue_from does."}
},
"required":["prompt"]
}`))
}
