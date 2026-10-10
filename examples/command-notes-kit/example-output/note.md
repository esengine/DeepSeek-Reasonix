---
owner: "@esengine"
backup: "@SivanCola"
status: active
reviewed: 2026-10-06
---

# Worked supplied-facts note

1. This reference uses the README's fixed request. It is written by the
   implementer and checked against the command template, not captured from a
   live model. No validation command was run to produce its supplied facts.
2. [commands/note.md](../commands/note.md) guides an ordinary model turn.
   Text substitution does not verify a caller's facts, execute a shell command
   or publish a note. Assess the selected model's answer separately.

## Full request and positional arguments

1. Invoke:

   ```text
   /command-notes-kit:note ISSUE-7 verified change; validation not run
   ```

2. The template preserves the entire request through `$ARGUMENTS`. `$1` is
   the issue identifier; `$2` is one whitespace-separated token. The resulting
   instruction fragments are:

   ```text
   Treat this request as the input: ISSUE-7 verified change; validation not run
   The first argument is the issue identifier: ISSUE-7
   The second argument begins the supplied facts: verified
   A literal dollar sign in this template is written as $.
   ```

3. `$2` does not contain the remaining words, so an answer needs the full
   supplied request to preserve the validation qualification. The semicolon
   is caller text; this substitution does not execute it as a shell separator.

## Reference note

1. A bounded note preserves the issue, reported change and missing evidence:

   ```text
   Issue
   ISSUE-7

   Observed change
   The caller reports a verified change but supplies no file, diff or
   description of the behavior. I cannot describe its implementation
   from these facts alone.

   Validation
   Validation was explicitly marked not run. No command, log, test count
   or result was supplied. This note does not establish passing checks,
   deployment or an independent review.
   ```

2. The word `verified` is part of the caller's description. It does not
   supply a test result or overturn the explicit `validation not run`.
   This reference neither inspects a repository nor sends the note anywhere.

## Missing change facts

1. In a fresh session, invoke with only the issue identifier:

   ```text
   /command-notes-kit:note ISSUE-7
   ```

2. A reference answer requests the missing information:

   ```text
   Issue: ISSUE-7. No change facts or validation evidence were supplied.
   Provide the observed change and checks actually performed with results,
   or explicitly mark validation as not run.
   ```

3. The template's missing-evidence instruction guides this response. It does
   not add a host validation error or guarantee that every model will comply.
