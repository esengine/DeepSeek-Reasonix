---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-01
---

# Tool Extension

This installable Go SDK example serves one model-callable tool through
`Options.Tools`. It counts whitespace-separated words in the supplied `text`
using Go's `strings.Fields`. The tool does not read files, write state, or
contact a service.

| Supplied text | Result |
| --- | --- |
| Empty or whitespace only | `0` |
| `hello world again` | `3` |
| `你好世界` | `1` |

This is a whitespace count, not language-aware segmentation.

The manifest declares the `tools` capability and the `count_words` name,
description, input schema, and `readOnly` claim.

The SDK advertises the `Options.Tools` keys in its initialize result;
`Initialize` does not repeat them. Keep the manifest and handler names aligned. The host rejects a
handshake that exceeds the manifest's declarations.

## Build and install

From this directory in a Reasonix source checkout, on macOS or Linux:

```sh
go test .
go build -o bin/word-counter.exe .
plugin_root="$(pwd -P)"
reasonix plugin install "$plugin_root" --dry-run
reasonix plugin install "$plugin_root" --link --replace --yes
reasonix plugin doctor word-counter
```

From PowerShell on Windows:

```powershell
go test .
go build -o bin/word-counter.exe .
$pluginRoot = (Resolve-Path .).Path
reasonix plugin install $pluginRoot --dry-run
reasonix plugin install $pluginRoot --link --replace --yes
reasonix plugin doctor word-counter
```

The fixed `.exe` path works on Unix and gives Windows its required suffix.

Review the dry-run's `FULL TRUST` block before installing. `readOnly` describes
this tool's behavior; it does not sandbox the sidecar.

Linking authorizes future changes in this directory, so use copy installation
for a fixed local build instead of a development link.

## Verify a call

Start a new session or run `/reload` while idle. With a tool-capable model,
ask it to use the word-counter tool to count the whitespace-separated words
in `hello world again`. Check the tool result `3` in the trajectory, rather
than accepting an answer the model computed itself.

The model discovers the tool through `use_capability`, rather than receiving
an extra schema in its cached prefix. The exact calls are:

```json
{"action":"search","query":"count whitespace-separated words"}
```

```json
{"action":"call","capability_id":"tool:ext__word-counter__count_words","arguments":{"text":"hello world again"}}
```

These are model tool calls, not shell commands or slash commands. Calls still
pass the host's normal permission check. Missing, null, or non-string text
returns a tool error; an empty string is valid and returns `0`.

`internal/assembly/boot/effect_sdk_tool_test.go` builds this actual example,
installs a copied package through preview and approved apply, deletes the
source, and drives discovery and a call through the real host to a scripted
recording provider.

It checks absent, installed, disabled, re-enabled, and removed states. This is
offline host evidence, not a live model quality test.

## Iterate and remove

Edit `main.go`, run `go test .`, rebuild the binary, then run `/reload` while
idle. Callbacks can overlap, so mutable state added to the handler must follow
the [SDK concurrency contract](../../README.md#concurrency-contract).

Use the callback's context for work; keep stdout exclusively for the protocol and
write diagnostics to stderr.

Disable or remove the package with the existing CLI:

```sh
reasonix plugin disable word-counter
reasonix plugin enable word-counter
reasonix plugin uninstall word-counter --yes
```

Run `/reload` while idle, or start a new session, after changing activation or
removing the package. Uninstalling a linked package preserves this source
directory.

See [Plugin Packages](../../../../docs/PLUGIN_PACKAGES.md#manifest-v2-extensions)
for manifest and trust rules, and [Extensions](../../../../docs/EXTENSIONS.md#runtime-reload)
for reload behavior.
