Coordinate concurrent chats and subagents with short write transactions and retained isolated results.

- `auto`, `shared` and `isolated` coordinate individual writes, reject stale views and expose recovery controls. Refs #14025.
- Configured Linux verification runtimes can validate and automatically integrate an exact candidate snapshot. Refs #14025.
- New installations default to automatic collaboration; existing configurations retain their saved semantics. Refs #14025.
- Root conversation forks and ancestor task continuation retain independent environments, with versioned recovery and previous-reader fences. Refs #14025.
- macOS/Windows support a configured local Linux container verifier. Automatic isolation falls back when program confinement is unavailable; explicit unsupported execution refuses. Refs #14025.
