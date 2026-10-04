---
owner: "@esengine"
backup: "@SivanCola"
status: active
reviewed: 2026-10-02
description: Summarize supplied check evidence and its limits
argument-hint: <label> <observations...>
---

Summarize the supplied check evidence, keeping observation separate from inference.

Label: $1
Observations: $ARGUMENTS
A literal dollar sign is $$.

1. Name each reported check, its scope and observed result. Preserve failures
   and checks not run; do not infer success from the absence of a failure.
2. If the label or observations are missing, identify the missing input. Do
   not fabricate logs, test counts, platforms, or a reviewer verdict.
3. Explain what the evidence does not establish. Use the caller's language.
   This prompt does not execute a check or enforce an execution boundary.
