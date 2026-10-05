---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-03
---

# Offline Provider Extension

This installable Go SDK example contributes one text-only provider through
`Options.Provider`. It returns `Offline echo: ` followed by the last user
message's content, then a done chunk. It needs no API key or network service
and emits no tool calls. It is an integration example, not a language model.

The host may compose workspace context and reminders into that message. The
example echoes the provider-visible content unchanged; it does not extract
the original typed input. It does not implement vision, reasoning, token
accounting, or structured response formats.

## Build and install

From this directory in a Reasonix source checkout, with Go 1.23+ and the
`reasonix` CLI available, on macOS or Linux:

```sh
go test .
go build -o bin/echo-provider.exe .
plugin_root="$(pwd -P)"
reasonix plugin install "$plugin_root" --dry-run
reasonix plugin install "$plugin_root" --replace --yes
reasonix plugin doctor echo-provider
```

From PowerShell on Windows:

```powershell
go test .
go build -o bin/echo-provider.exe .
$pluginRoot = (Resolve-Path .).Path
reasonix plugin install $pluginRoot --dry-run
reasonix plugin install $pluginRoot --replace --yes
reasonix plugin doctor echo-provider
```

The fixed `.exe` path works on Unix and gives Windows its required suffix.
Review the dry-run's `FULL TRUST` block before installation. The CLI copies
the package by default; rebuilding the source does not change that copy.
For development, add `--link` to authorize changes in the source directory.

## Run a turn

From your workspace, use the installed provider directly:

```sh
reasonix --model plugin/echo-provider/offline/echo -p "SDK-PROVIDER-CHECK hello"
```

The assistant text MUST begin with `Offline echo: ` and include
`SDK-PROVIDER-CHECK hello`. Additional host-composed context may also appear.
The provider does not call another model or execute tools.

In an existing session, run `/reload` while idle, then use `/model` to choose
`plugin/echo-provider/offline/echo`. The same reference works as
`default_model` in `reasonix.toml`, including on the first runtime build.

`internal/assembly/boot/effect_sdk_provider_test.go` builds this actual
example, installs a copy through preview and approved apply, deletes the
source, and checks the first controller turn and a second turn on the same
sidecar.

It repeats after re-enabling the package and verifies the typed
unknown-model error when the configured plugin default is absent, disabled,
or removed. This is offline host evidence, not model quality evidence.

## Author contract

| Part | Rule |
| --- | --- |
| Manifest | Declare the `providers` runtime capability and `plugin/echo-provider` provider capability with ID `offline/echo`. |
| Identity | The host sets `REASONIX_PLUGIN_NAME`; both initialize and catalog MUST return the matching `plugin/<id>/offline/echo` reference. |
| Fingerprint | This example's `schemaHash` is SHA-256 of the compact JSON encoding of its default `ProviderDescriptor`, as checked by `go test .`; it is an author capability fingerprint, not the full protocol schema hash. |
| Streaming | Return promptly, produce chunks on the channel, and close it. `DoneChunk` ends the assistant turn; channel closure lets the SDK emit the stream end. |
| Cancellation | The producer MUST stop when the callback's context is cancelled, including while waiting to send. |
| Concurrency | This provider is stateless. Added shared mutable state MUST follow the [SDK concurrency contract](../../README.md#concurrency-contract). |
| Output | stdout belongs to the SDK protocol. Diagnostics MUST go to stderr. |

## Iterate and remove

Run `go test .`, rebuild, and replace the installed copy after editing. For a
linked development install, rebuild and run `/reload` while idle. Start a new
session or reload after changing activation:

```sh
reasonix plugin disable echo-provider
reasonix plugin enable echo-provider
reasonix plugin uninstall echo-provider --yes
```

Select another model before disabling or removing the active provider. A
`default_model` pointing to an inactive plugin cannot resolve at boot; change
that default before starting a new session.

See [Plugin Packages](../../../../docs/PLUGIN_PACKAGES.md#manifest-v2-extensions)
for manifest and trust rules, and [Extensions](../../../../docs/EXTENSIONS.md#runtime-reload)
for reload behavior.
