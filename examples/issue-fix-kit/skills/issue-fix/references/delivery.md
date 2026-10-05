---
owner: @esengine
backup: @SivanCola
status: active
reviewed: 2026-10-01
---

# Issue delivery format

1. **Problem:** state the trigger and expected behavior. Cite the user's issue
   or the local input that establishes the requirement.
2. **Cause and change:** identify the affected file or symbol, the evidence for
   the cause, and the resulting behavior. Separate confirmed facts from doubts.
3. **Validation:** list each relevant command, its observed result, and what it
   covers. A command suggested but not executed is **not run**, not passed.
4. **Limits:** state unresolved scope, environmental blockers, and unverified
   platforms. Distinguish unrelated existing failures from this issue.
5. **PR draft:** supply a title and body describing the final diff for a reader
   who has not seen the conversation. Include the useful validation evidence.

The handoff is a draft until the user authorizes publication. Do not invent an
issue number, commit, PR URL, check result, or maintainer approval.
