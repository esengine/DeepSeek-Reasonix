---
owner: "@esengine"
backup: "@SivanCola"
status: active
reviewed: 2026-10-02
description: Draft a handoff from supplied task facts without inventing results
argument-hint: <task facts...>
---

Draft a short work handoff using only the facts supplied by the caller.

Facts supplied by the caller:
$ARGUMENTS

1. Report the outcome, evidence of checks actually performed, and remaining
   work in separate sections. Preserve identifiers and qualifications.
2. Treat a check marked not run as unverified. A passing unit test does not
   establish an integration result, deployment, or production readiness.
3. If the input is empty or essential facts are absent, name the missing facts
   and ask for them instead of claiming completion.
4. Keep the answer in the caller's requested language. This template supplies
   guidance; it grants no permission to edit files, run checks, or contact others.
