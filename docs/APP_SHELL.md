# App composition boundary

[简体中文](APP_SHELL.zh-CN.md)

App.tsx only mounts AppRuntime. AppRuntime composes session, navigation and
shell-store owners; AppRuntimeView renders the existing shared regions.
Effects and source-bound commands stay with their domain owners. Extracting
the view must preserve hook order, component identity, draft state and command
registration, and must not introduce a second mutable active-session authority.

The App entry contract rejects direct bridge access, effects and async work.
The AST layer gate follows runtime imports, re-exports, aliases and dynamic
imports, and rejects transitive domain/common dependencies on App owners.
Type-only edges remain distinct. Negative fixtures verify those checks.

Context-window presentation helpers and lazy subagent outcome/preview cards
are separate view modules. The controller retains tool output and a compact tuple for live wire outcomes;
historical outcome text is parsed only when the lazy card renders. The rendered result and source command boundary
remain unchanged.

Use `pnpm check:app-layers`, `pnpm test:all`, `pnpm test:app-lifecycle` and
`pnpm test:app-browser` to verify these contracts. The independent App memory
workflow and native Transcript gates remain required qualification. See
[session ownership](APP_SESSION_OWNERSHIP.md) for the screening protocol and
the separate pending heap-retainer/control attribution duty.
