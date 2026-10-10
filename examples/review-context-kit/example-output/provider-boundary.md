---
owner: "@esengine"
backup: "@SivanCola"
status: active
reviewed: 2026-10-06
---

# Worked context-delivery comparison

1. This reference is written by the implementer and checked against
   [review.txt](../context/review.txt) and the existing
   [host effect test](../../../internal/assembly/boot/effect_review_context_example_test.go).
   It describes host delivery; no live model produced an answer for this document.
2. The test uses real approved-plan installation and `boot.Build` with a
   recording provider. Its assertions establish the received request contents,
   not the quality or completeness of a model's review.

## Complete reference block

1. The host wraps the trimmed `context/review.txt` body in this block. This
   is a source-checked block, not a full provider request or a model response:

   ```text
   <hook-context event="SessionStart">
   Review reference from review-context-kit

   When reviewing a change, connect each finding to a concrete source location and
   the behavior it affects. Separate observed behavior from an untested hypothesis.
   List the checks actually run and their results. State what remains unverified.
   Do not present this reference, successful installation, or a plausible explanation
   as evidence that a change works. Follow the user's requested review scope.
   </hook-context>
   ```

2. The reference goes into the latest user message. The original raw user
   input remains unchanged. The test also checks that the reference stays out
   of the stable system prefix and that the provider tool schema stays unchanged.

## Three input turns

1. The count below means complete copies of the block above in the newest
   user message of each recorded provider request. It excludes earlier messages,
   other context and anything the provider may return as an answer.

   | Turn | Original raw input | Session transition | Enabled package: complete blocks |
   | --- | --- | --- | --- |
   | 1 | `Review this change` | First input to the controller's session | 1 |
   | 2 | `Report the checks actually run` | Same session | 0 |
   | 3 | `Review in the fresh session` | `NewSession` before this input | 1 |

2. The first turn's message may remain in the second request's conversation
   history under normal context management. Its presence there does not add
   another block to the second turn's newest user message.
3. These are assertions exercised by the recording-provider test. A manual
   model run needs its own request and tool evidence; the model repeating the
   reference's wording cannot establish where the host sent it.

## Package lifecycle comparison

1. Each row below uses a newly built controller. A preview leaves the package
   uninstalled; disable retains its managed copy; removal deletes that copy.

   | Package state | First turn | Same-session next turn | First turn after `NewSession` |
   | --- | --- | --- | --- |
   | Absent | 0 | 0 | 0 |
   | Previewed, not applied | 0 | 0 | 0 |
   | Copied and enabled, temporary source removed | 1 | 0 | 1 |
   | Disabled | 0 | 0 | 0 |
   | Re-enabled | 1 | 0 | 1 |
   | Removed | 0 | 0 | 0 |

2. Disabling or removing the package affects newly built controllers. It
   does not erase reference text already sent in an existing session's history.
3. These delivery counts do not establish a reviewed change, passing checks,
   an independent reviewer verdict or production readiness. Those claims need
   their own source locations, observed behavior and actual verification results.
