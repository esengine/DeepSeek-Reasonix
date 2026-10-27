---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-25
---

# Frame extension example

## Purpose

A task board drawn in a sandboxed frame. The board's HTML ships in the package (`frames/board.html`); the sidecar owns the cards, publishes them as props, and moves one when the frame sends `advance`.

| File | Role |
| --- | --- |
| `reasonix-plugin.json` | Declares the frame, the two UI actions and a runtime with the `ui` capability. |
| `main.go` | Publishes the frame on `session.start` and again after each action. |
| `frames/board.html` | Speaks `reasonix.frame.v1`: says `ready`, paints `init` and `props`, sends `action`. |

## Steps

1. Build the sidecar.

```bash
go build -o bin/task-board.exe .
```

2. Preview the install. The plan lists the frame with its size and SHA-256, and the runtime as FULL TRUST.

```bash
reasonix plugin install . --dry-run
```

3. Install it linked, then run `/reload` in an idle session.

```bash
reasonix plugin install . --link --replace --yes
```

4. Open the workbench in Studio. The board is its own tab; a card's arrow button moves it one column.

The document gets no network and no access to the Studio page. Everything it shows comes from `props`, and everything it does goes through the two declared actions.
