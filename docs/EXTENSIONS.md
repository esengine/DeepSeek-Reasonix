# Reasonix Extensions

Extensions let a plugin package change what Reasonix does at runtime —
rewrite input, intercept tool calls, replace the system prompt, contribute
streaming model providers, publish structured UI, and ship prompts and
themes — using a stable, versioned contract.

Two kinds of plugin capabilities exist:

- **Declarative** (any plugin package): skills, agents, commands, prompts,
  hooks, MCP servers, and themes. These are files and configuration; they
  run with the host's normal permissions.
- **Code runtime** (Manifest v2 `runtime` block): a sidecar process speaking
  the Extension Protocol. Code extensions are **full trust** — see the
  security section below before installing one.

## Installing and managing

Extensions install exactly like any plugin package:

```bash
reasonix plugin install git:github.com/owner/extension --dry-run   # preview
reasonix plugin install git:github.com/owner/extension --yes       # install
reasonix plugin show <name>                                        # details
reasonix plugin doctor <name>                                      # validate
```

For a plugin with a `runtime` block, the preview and `show` output include a
**FULL TRUST** block: the runtime command, the events it intercepts, the
replacement slots it owns, and its provider/UI capabilities. Installing,
updating, replacing, or `--link`ing is the authorization — there is no
second confirmation, and `--link` keeps trusting changed content. Only
install runtimes you trust completely.

## What extensions can do

- **Interceptors** — observe and rule on 17 hook points (input, tool calls,
  permission decisions, provider requests/responses, compaction, session
  lifecycle, frontend events). An interceptor can `continue`, `block` with a
  user-visible reason, or `replace` the payload; the host re-validates every
  replacement.
- **Replacement strategies** — single-owner slots (`system_prompt`,
  `context`, `provider_request`, `provider_response`, `compaction`,
  `session_policy`, `permission`, `frontend_events`, `tool:<name>`,
  `provider:<ref>`). One owner per slot across all installed plugins; a
  collision fails the runtime build with both sources named.
- **Streaming providers** — new models appear as
  `plugin/<plugin>/<provider>/<model>` in the model picker, streamed with
  the same text/reasoning/tool-call/usage semantics as built-in providers.
  The ref works everywhere a built-in ref does: `default_model`, `--model`,
  the CLI/Desktop/ACP pickers, and mid-session model switches — including on
  the very first boot.
- **Structured UI** — status entries, cards, forms, and notifications
  rendered natively in the CLI transcript, the Desktop app, and ACP clients
  (with text fallbacks), plus `/<plugin>:<action>` actions in the slash
  menu, the Desktop command palette, and ACP's discoverable commands.
- **Prompts and themes** — `/<plugin>:<name>` prompt templates and
  read-only plugin themes (`plugin:<plugin>:<theme>`) in Desktop Settings.

## Runtime reload

Changing an installed extension (install, update, enable/disable, or
`--link` content changes) never mutates a running turn. Reloading is one
fail-atomic operation through every interactive frontend — CLI `/reload`,
Desktop `/reload` (composer slash menu) or **Reload Runtime** (command palette),
Serve `/reload`, and the ACP
vendor method `_reasonix.io/session/reloadExtensions`:

1. If a turn or background work is running, CLI/Desktop/ACP queue exactly one
   reload; Serve rejects the request so the browser can retry once idle.
2. When idle, Reasonix starts new sidecars and builds a new runtime
   snapshot.
3. On full success it swaps atomically, carrying over the session path,
   transcript, approval grants, and goal/recovery state.
4. If the new build fails, the old runtime keeps working untouched.
5. Only after the swap are the old sidecars retired.

Each turn pins one runtime generation for the whole turn, tool batch, and
compaction — extension changes apply to the *next* turn, and a no-op reload
leaves the provider prompt-cache prefix byte-identical.

## Performance and prompt cache

With no code runtime installed, the Agent takes the existing nil-dispatcher
path: no sidecar process, JSON encoding, RPC, or event queue is involved.
When runtimes are installed, Reasonix initializes at most four sidecars at once
inside one shared 30-second generation startup budget. A stalled optional
runtime therefore cannot multiply boot or reload time by the number of installed
packages. Packages that do not start inside that budget degrade or fail according
to their `runtime.required` setting.
Enabled synchronous interceptors are deliberately on the matching hot path and
run serially, so their RPC and handler latency is additive; keep input, tool,
permission, and provider interceptors small and deterministic. Observation
events use a bounded non-blocking queue and are dropped with a warning under
backpressure instead of stalling the turn.

An observation-only extension does not change the provider-visible cache
prefix. A stable system-prompt or tool replacement creates one intentional
cold prefix after install/reload and remains cacheable afterwards. A strategy
that injects timestamps, random values, session IDs, or other per-turn data
into the system prompt, tool schemas, context prefix, or provider request can
destroy cache reuse; dynamic data should stay in the current turn tail when
possible. Maintainers can measure host overhead with:

```bash
go test ./internal/extension/... -run '^$' -bench 'Extension|Dispatch' -benchmem
```

## Developing an extension

Start with the complete
[`starterextension`](../sdk/go/examples/starterextension/README.md) package.
It keeps the manifest, Sidecar source, cross-platform build commands, linked
installation, and first observable intercept in one directory. The normal
development loop is:

1. Add `apiVersion: "reasonix.io/plugin/v2"` to `reasonix-plugin.json` and
   declare `contributes` and (optionally) `runtime` — see
   [Plugin Packages](./PLUGIN_PACKAGES.md#manifest-v2-extensions).
2. Implement the Sidecar. The [Go SDK](../sdk/go/README.md) (standard library
   only) handles transport, handshake, sequencing, content references, and
   shutdown; the [wire contract](./EXTENSION_PROTOCOL.md) and
   [generated method index](./EXTENSION_PROTOCOL.generated.md) are the
   language-neutral references.
3. Build the runtime binary, preview its trust and capabilities with
   `reasonix plugin install /path/to/plugin --dry-run`, then install it with
   `--link --yes`.
4. Validate with `reasonix plugin doctor <name>`, run `/reload` while idle,
   and exercise the contributed intercept, Provider, UI action, or resource.

SDK releases use immutable `sdk/go/vX.Y.Z` tags. The first public version is
`sdk/go/v1.0.0`; until that tag exists, use the starter from a source checkout
instead of relying on an unversioned module.

## Compatibility

- Native `reasonix-plugin.json` manifests must declare the exact
  `reasonix.io/plugin/v2` API version. There is no v1 dual-read or automatic
  migration path because extension manifests were not publicly released on v1.
- Older Reasonix versions ignore extension-only state: the per-session
  `<session>.extensions.json` sidecar file, `plugin/...` model refs (they
  simply resolve as unavailable models), and the `extension_surface` /
  `extension_status` event kinds (older frontends drop unknown kinds; ACP
  clients without `reasonix.extensionSurface` get text fallbacks).
- `plugin-packages.json` keeps its existing schema; an enabled installed
  runtime *is* the trust record.

## Security model

A code extension runs outside the Reasonix sandbox with the unfiltered
inherited environment. It can read the full session and environment, bypass
permissions and workspace restrictions, and operate the machine directly;
its `permission.decision` "allow" overrides a host deny. In return the host
enforces:

- only plugins installed through the plugin flow can start a runtime —
  project configuration can never declare one;
- the handshake rejects any capability beyond the manifest;
- replacements are re-validated against each point's DTO and schema;
- sidecar diagnostics, structured UI, interceptor reasons, and provider errors
  are credential-redacted by the host before they reach the UI, logs, or error
  surfaces; ordinary provider/model content is preserved as product data;
- a crashed sidecar fails its own operations explicitly — Reasonix never
  silently falls back to another model or strategy.
