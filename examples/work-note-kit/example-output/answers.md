---
owner: "@esengine"
backup: "@SivanCola"
status: active
reviewed: 2026-10-06
---

# Worked prompt answers

1. These references use the README's fixed arguments. They are written by the
   implementer and checked against the template instructions; no live model
   produced them and no check was executed to create the reported facts.
2. The template substitutes caller text into an ordinary user turn. Its
   instructions guide the answer; they do not verify supplied facts or enforce
   truthfulness. Assess an actual model's response separately.

## Supplied handoff facts

1. Invoke:

   ```text
   /work-note-kit:handoff lint-passed typecheck-passed integration-not-run
   ```

2. [handoff.md](../prompts/handoff.md) inserts all three facts through
   `$ARGUMENTS`. A reference answer keeps reported passes separate from
   the missing task outcome and unrun integration check:

   ```text
   Outcome
   No task outcome was supplied.

   Check evidence
   The caller reports lint-passed and typecheck-passed.
   No logs, exact commands, versions or check scope were supplied.

   Remaining work
   Integration was marked integration-not-run and remains unverified.
   Confirm the task outcome and provide the required integration evidence.
   These facts do not establish deployment or an independent review.
   ```

3. This answer reports the caller's observations. It does not claim that the
   template ran lint or typecheck, inspected logs, or completed an integration.

## Check summary label

1. Invoke:

   ```text
   /work-note-kit:checks:summarize build unit-passed network-not-run
   ```

2. [summarize.md](../prompts/checks/summarize.md) uses `$1` for the label and
   `$ARGUMENTS` for the complete observations, including that label. Its
   substitution yields these lines; they are not an executed check report:

   ```text
   Label: build
   Observations: build unit-passed network-not-run
   A literal dollar sign is $.
   ```

3. A reference summary distinguishes the label from the supplied results:

   ```text
   Label: build
   Supplied observations: build unit-passed network-not-run

   The caller reports a passing unit check and a network check that was not run.
   The word build is the label; no separate build result was supplied.
   No command, test count, platform or logs were provided for the unit check.
   Network behavior, deployment and production readiness remain unverified.
   ```

## Missing handoff facts

1. Invoke without arguments:

   ```text
   /work-note-kit:handoff
   ```

2. The empty `$ARGUMENTS` leaves no caller facts. A reference answer requests
   the missing evidence instead of inventing a completed task:

   ```text
   No task facts were supplied, so I cannot report an outcome or check results.
   Provide the task outcome, relevant changes, checks actually performed
   with their results, and any remaining work.
   ```

3. Empty input still reaches the model. This is the answer to assess against
   the template's missing-facts guidance, not a host-side validation refusal.
