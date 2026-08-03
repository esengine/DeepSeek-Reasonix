# Repository Contributors

Reasonix recognizes contributions from merged commits and from source pull
requests that materially inform a maintainer integration, even when the source
implementation is not merged verbatim. GitHub-recognized co-contributor credit
is recorded with public `Co-authored-by` commit trailers; this file preserves
the contribution and integration outcome in human-readable form.

| Source pull request | Contributor | Recognized contribution | Integration outcome |
| --- | --- | --- | --- |
| [#7254](https://github.com/esengine/DeepSeek-Reasonix/pull/7254) | [@orz0219](https://github.com/orz0219) | Identified and covered DeepSeek V4 Flash tool-call responses that can report zero reasoning tokens while omitting replayable `reasoning_content`, exposing a misleading user warning. | [#7259](https://github.com/esengine/DeepSeek-Reasonix/pull/7259) used the reported behavior and test direction to define a bounded silent-recovery path. It did not adopt the proposed zero-token exemption because the wire format cannot always distinguish an explicit zero from an omitted field. Commit [`2a2d0e6`](https://github.com/esengine/DeepSeek-Reasonix/commit/2a2d0e674a1fb2f663276a739ae9a071d2296e09) records the contributor with a public co-author trailer. |

The complete automatically generated commit-contributor graph remains available
on [GitHub](https://github.com/esengine/DeepSeek-Reasonix/graphs/contributors?all=1).
