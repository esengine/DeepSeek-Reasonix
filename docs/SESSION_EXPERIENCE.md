# Session experience

The desktop Settings page uses one **Session experience** preference to control how work in progress is presented in a transcript.

## Modes

| Mode | While a turn is running | After the turn completes |
| --- | --- | --- |
| **Standard** | Work in progress is visible. | Completed work is collapsed by default; expand it from the message when needed. |
| **Deep** | The complete work process is shown in real time. | Completed work remains expanded; individual sections can still be collapsed manually. |

This preference applies to reasoning, tool calls, sub-task progress, work-process cards, approvals, validation, and the active turn. It changes presentation only. It does not change the selected model, reasoning strength, provider request, cost, context window, or saved transcript data.

Manual expand/collapse is a message-level reading action and is retained for the message row. It does not create another global setting.
Overrides are keyed by session and stable process-segment identity in a bounded in-memory cache. They survive React re-renders and transcript window recycling, but intentionally do not persist across application restarts.

Warnings, approvals, delivery states, extension cards, and other items that require user action remain outside the completed work-process fold, so Standard mode never makes an action unreachable.

## Configuration and compatibility

The canonical desktop configuration is:

```toml
[desktop]
session_experience = "standard"
```

The only valid values are `standard` and `deep`; missing or invalid values use `standard`. The Go Settings and startup snapshots are authoritative. The TypeScript field remains optional for one release so older backends and historical fixtures fall back to Standard; local storage is never allowed to override a backend snapshot.

Older setters and fields remain compatibility mirrors for one complete release cycle:

| Legacy write | Canonical result |
| --- | --- |
| `SetDisplayMode(*)` | Standard |
| `SetReasoningDisplayMode("expanded")` | Deep |
| any other reasoning value | Standard |
| `SetExpandThinking(*)` | Standard |
| process fold `expanded` / `auto` | Deep / Standard |

Every canonical write mirrors `display=standard` and `reasoning=auto|expanded` for rollback compatibility. These fields, setters, events, and local-storage mirrors may be removed only after the next complete release has shipped and downgrade support is no longer required.
