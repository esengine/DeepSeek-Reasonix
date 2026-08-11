## Summary

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

## Documentation impact

Documentation-impact: TODO

For changes to user-visible CLI, Desktop, configuration, provider, permission,
or tool behavior, use one of:

- `Documentation-impact: updated - <what changed>` and update `docs/*.md`.
- `Documentation-impact: none - <why the embedded documentation remains correct>`.

## Cache impact

Cache-impact: TODO
Cache-guard: TODO
System-prompt-review: N/A

For cache-sensitive changes, fill these lines before requesting review:

- `Cache-impact`: `none`, `low`, `medium`, or `high`, plus the reason.
- `Cache-guard`: the focused guard test/command added or run, or why an existing guard covers the change.
- `System-prompt-review`: required reviewer/approval note when provider-visible system prompt, memory prefix, output style, or skill index behavior changes.
