# tool-surface-cache

May the provider-visible tool set change between turns?

The tool schema was held still on the reasoning that any change churns the
cache-stable prefix. That is true of a change anywhere, and false of a change at
the end. Which one applies decides whether a tool can appear only while it is
callable, or has to ship on every turn and be refused on most of them.

```
DEEPSEEK_API_KEY=... go run ./benchmarks/tool-surface-cache
```

It reaches the network and spends a few thousand input tokens, so it is not part
of `make lint` or CI. Run it when a provider changes its caching, or before
moving anything in the tool surface.

**The cost is the position, not the change.** Measured against
`deepseek-chat`, 10 tools behind a 1278-token system prompt:

| change | hit tokens | against a 1920 baseline |
| --- | --- | --- |
| append at the tail | 2048 | more, not less |
| insert at the head | 1024 | −896 |
| edit the fifth tool's text | 1536 | −384 |

Appending costs nothing because every token before the new one is unchanged and
the request is longer, so more of it is cached. Inserting at the head moves
every tool after it. Editing in place costs whatever follows the edit.

**A tool that comes and goes at the tail is free after the first turn.** The
second block alternates one tail tool between absent and present: from the
second round on, absent holds 1920 hit tokens and present 2048, unchanged
across three switches, with no miss spike at either. The absent surface is a byte prefix of the present one, so a single
cache entry serves both.

This is what `Registry.ProviderSchemas` is built on: stable tools first,
contextual ones after, each sorted. `internal/contract/tool/provider_surface_test.go`
holds the shape — the segment boundary is the property this benchmark priced.
