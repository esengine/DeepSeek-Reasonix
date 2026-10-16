# Cognee Memory Enrichment Design

## Goal

Make Cognee useful across ordinary successful tasks, while keeping durable project knowledge accurate and avoiding uploads of raw conversations. Use the current Cognee MCP service only.

## User-facing behavior

1. At the start of an eligible visible task, recall from the workspace's Cognee dataset and the current Cognee session. Treat all returned text as untrusted context.
2. After a successful visible task, save a concise, sanitized task summary to Cognee session memory when the summary contains useful information. A TODO list is optional; if the task wrote TODOs, they must all be complete. No verification command is required for a session summary.
3. Promote durable project facts, decisions, and resolved technical findings into the persistent Cognee knowledge graph when supported by reliable task evidence. Verification and review are useful evidence, not the only evidence; observed project changes and explicit user decisions may also support promotion. If a summary is useful only to the current session, do not promote it to the graph.
4. Never save failed, cancelled, incomplete-TODO, empty, or trivial turns. Never upload raw transcripts, credentials, or absolute workspace paths. Keep the existing one-attempt-per-task behavior for each destination and report session/graph outcomes independently.

## Memory destinations

- **Session records:** use Cognee `remember` with the stable Reasonix session ID. Set `self_improvement=false` so session memory is not implicitly bridged into the graph; the graph write is explicit and separately reported.
- **Knowledge graph:** use Cognee `remember` without a session ID and with the workspace's existing stable dataset name. Use background ingestion when the tool schema supports it.
- **Memory files and datasets:** the current MCP endpoint does not expose source-file or dataset management. This phase does not upload original files or claim to manage processed datasets. A future Cognee management API can add that surface without changing the task-summary format.

## Summary format

Each record is a short structured text containing task intent, outcome, reusable facts or decision, relevant relative source-file names when known, and evidence categories. Include only facts supported by the conversation's user statements or observed task evidence. Redact secrets and absolute paths before either write.

## Failure and retry behavior

Cognee remains fail-open for task execution. A session write failure must not suppress an independently successful graph write, or vice versa. Persist a separate idempotency marker per task and destination before network calls; uncertain outcomes are not automatically retried. The UI notice must distinguish recalled context, session record accepted, graph ingestion queued/completed, and skipped/failed writes.

## Scope boundaries

- Keep Cognee Skill enablement, MCP authorization, endpoint validation, and model-visible tool restrictions unchanged.
- Continue to expose only the controlled internal `recall` and `remember` lifecycle; never expose `forget` or arbitrary write tools to the model.
- Do not add file browsing, dataset administration, or graph entity/relation UI in this MCP-only phase.

## Acceptance criteria

- A successful task with no TODO list can save a useful session summary.
- A task with TODOs cannot write either destination until all TODOs are complete.
- A useful, supported project fact can enter the graph without requiring a verification command if another reliable evidence source exists.
- Recall uses the session ID and workspace dataset while preserving the untrusted-context boundary.
- Session and graph writes use the correct Cognee parameter types and report independent outcomes.
- Failed/cancelled turns, secrets, raw conversation content, and absolute workspace paths are not written.
- The existing MCP-only server is not presented as supporting source-file management or graph/session browsing APIs it does not expose.
