# Starter Extension

This directory is a complete, installable Extension Protocol v2 plugin. Its
sidecar intercepts `input.receive`; text beginning with `starter: ` is passed to
the model with ` [rewritten by starter-extension]` appended.

The `.exe` suffix is intentional: using one fixed runtime path keeps the
manifest identical on every platform. Unix executes the binary normally, and
Windows requires the executable suffix.

## Build and install

From this directory on macOS or Linux:

```sh
go build -o bin/starter-extension.exe .
plugin_root="$(pwd -P)"
reasonix plugin install "$plugin_root" --dry-run
reasonix plugin install "$plugin_root" --link --replace --yes
```

From PowerShell on Windows:

```powershell
go build -o bin/starter-extension.exe .
$pluginRoot = (Resolve-Path .).Path
reasonix plugin install $pluginRoot --dry-run
reasonix plugin install $pluginRoot --link --replace --yes
```

Review the `FULL TRUST` block in the dry-run output before installing. The
linked package trusts future changes in this directory and runs outside the
Reasonix sandbox.

Start a new session, or run `/reload` while the current session is idle. Send:

```text
starter: explain what an Extension Protocol sidecar does
```

The model receives the rewritten text. Edit `main.go`, rebuild the binary, run
`/reload`, and try again. Use `reasonix plugin doctor starter-extension` when
the manifest or binary fails validation.

## Next steps

- [`../../README.md`](../../README.md) documents SDK callbacks and the
  concurrency contract.
- [`../../../../docs/EXTENSIONS.md`](../../../../docs/EXTENSIONS.md) explains
  reload, performance, cache behavior, compatibility, and trust.
- [`../../../../docs/PLUGIN_PACKAGES.md`](../../../../docs/PLUGIN_PACKAGES.md)
  defines every Manifest v2 field.
- [`../../../../docs/EXTENSION_PROTOCOL.md`](../../../../docs/EXTENSION_PROTOCOL.md)
  is the wire-protocol reference.
- [`../fullsidecar/main.go`](../fullsidecar/main.go) demonstrates providers,
  structured UI, strategies, tools, content references, and shutdown.

For a distributable plugin, build binaries for the target platforms, keep the
manifest runtime path aligned with the packaged binary, and publish immutable
source or release artifacts for users to review before installation.
