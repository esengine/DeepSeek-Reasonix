# Starter Extension

This directory is a complete, installable Extension Protocol v2 plugin. Its
sidecar intercepts `input.receive` and appends
` [rewritten by starter-extension]` to each non-empty composed input.

This event receives the complete input after Reasonix adds workspace, skill,
and other turn context. The example preserves that text byte for byte before
appending its marker; a command prefix typed by the user need not be at the
start of this payload. No special prefix is required to try the example.

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
explain what an Extension Protocol sidecar does
```

The model receives your request and the demonstration marker together with
Reasonix's turn context. The marker demonstrates the input interceptor; it does
not change the cache-stable system prompt.

Edit `main.go`, rebuild the binary, run `/reload`, and try again. Use
`reasonix plugin doctor starter-extension` when the manifest or binary fails
validation.

## Verify without an API key

1. Keep `starter-extension` installed, then build and install the
   [offline provider example](../providerextension/README.md#build-and-install).
   Review its dry-run trust block as well. This local echo provider needs no
   API key and does not call another model or execute tools.
2. From your workspace, run a fresh CLI turn:

   ```sh
   reasonix --model plugin/echo-provider/offline/echo -p "SDK-STARTER-CHECK hello"
   ```

   The assistant text SHOULD begin with `Offline echo: ` and include
   `SDK-STARTER-CHECK hello` and one `[rewritten by starter-extension]` marker.
   Additional host-composed context may appear. This checks the installed
   interceptor's delivery to the provider, not language-model quality.
3. Repeat the same command after each activation change below. Every `-p`
   invocation starts a new session; inspect that invocation's assistant text,
   not messages from an earlier session. Keep `echo-provider` enabled until
   these checks finish.

| Starter state | Expected marker count in the fresh turn's assistant text |
| --- | --- |
| Installed and enabled | 1 |
| Disabled | 0 |
| Re-enabled | 1 |
| Removed | 0 |

Disable `starter-extension` in Studio's installed-plugin list and start a new
session to verify that the marker stops appearing. Re-enable it and start a new
session to restore the interceptor. Remove the package when finished:

```sh
reasonix plugin remove starter-extension --yes
```

Removing this linked installation leaves the example's source and built binary
in place. Delete `bin/starter-extension.exe` separately if it is no longer needed.

Remove the offline provider after the final marker check:

```sh
reasonix plugin remove echo-provider --yes
```

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
