---
owner: "@esengine"
backup: "@SivanCola"
status: active
reviewed: 2026-10-06
---

# Prefix example: worked release-note draft

1. This is a reference answer for the fixed local exercise in
   [change-evidence.md](change-evidence.md). Codex checked and revised it against
   the installed release-note skill, complete format reference and evidence.
2. It is a draft for contributors to the prefix example. No publication,
   release version or native Reasonix live-model/human approval is asserted.
   Compare your own answer against the evidence before reading this draft.

## Fixes

- The prefix helper bounds the requested limit between zero and the current
  slice length. Negative limits return an empty slice, oversized limits return
  the available input, and a nil slice no longer panics. See
  [RN-01's patch and input table](change-evidence.md#rn-01-selected-change).
- Extra capacity does not expose values beyond the current length. Normal
  and zero limits keep their previous behavior. These are also covered by
  [RN-01](change-evidence.md#rn-01-selected-change).

## Validation

- In the observed macOS temporary fixture, `go test -count=1 -v ./...` passed
  the five existing cases and the added length-versus-capacity case. The
  existing cases checked that the input values were not changed. See
  [the complete observed scope](change-evidence.md#observed-checks).
- These fixture checks do not establish native Windows/Linux behavior,
  integration, deployment, live-model authoring quality or independent review.

## Needs confirmation

- The target release version and publication approval were not supplied.
- No public commit or PR link was supplied; RN-01 is the selected local
  evidence record. Add an actual selected change link when one exists.

## Review the draft

| Question | Evidence or expected treatment |
| --- | --- |
| Is every behavior claim selected? | Only RN-01 appears as a fix. |
| Did a title alone supply the claim? | The complete before/after function, patch and input table supply it. |
| Is the result a copy with independent storage? | No; the unselected idea is omitted and no allocation is claimed. |
| Are six fixture cases presented as a whole-product gate? | No; platform and fixture scope remain explicit. |
| Must a new feature or upgrade section appear? | No verified highlight or user migration is supplied, so those sections are omitted. |
| What if the observed-check record is removed? | Replace the validation result with “Validation not verified from the selected evidence.” |
| Is the example publication approved? | No; keep the missing version and publication approval in Needs confirmation. |
