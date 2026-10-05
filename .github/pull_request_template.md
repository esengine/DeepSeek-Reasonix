<!--
Target branch: 2.x features and fixes go to `studio`. 1.x takes bug fixes,
provider/API compatibility, release/updater, security and platform stability
only, on `main-v2`.
-->

## Summary

-

## Cause

-

## Blast radius

-

## Neighbouring behaviours tested

-

## Why this layer

-

## Issues

<!--
If this resolves a report, put `Fixes #123` on its own line — GitHub only
auto-closes from a bare line, so `- Fixes #123` in a list does nothing and the
report stays open. If it only relates to one, use `Refs #123` instead: the
release workflow then asks that reporter to verify once the fix ships.
-->

## Verification

-

For a GSAP to WAAPI/CSS migration (or any cross-API replacement), document
the source-to-target contract here: easing syntax, time units, callbacks,
cancellation, reduced-motion behavior, and failure fallback. Verification must
assert that the target API was actually called; a mock that silently skips it
does not count.
