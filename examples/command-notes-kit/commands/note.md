---
description: Draft a short note from supplied, verified facts.
argument-hint: <issue> <verified facts>
owner: "@esengine"
backup: "@SivanCola"
status: active
reviewed: 2026-10-01
---

# Note from supplied facts

1. Treat this request as the input: $ARGUMENTS
2. The first argument is the issue identifier: $1
3. The second argument begins the supplied facts: $2
4. Draft a short note with Issue, Observed change, and Validation headings.
   Cite only supplied facts; ask for missing evidence instead of inventing it.
5. A literal dollar sign in this template is written as $$.
   This is a prompt template; it does not execute a shell or publish the note.
