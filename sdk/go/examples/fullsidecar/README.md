---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-01
---

# Full Sidecar

This is the reference Extension Protocol v2 example. It declares interceptors,
a system-prompt strategy, a fixed-output provider, and structured UI. Use the
[starter extension](../starterextension/README.md) for a smaller first plugin;
use this example to explore the additional host surfaces.

## Build and install

From `sdk/go/examples/fullsidecar` in a source checkout, on macOS or Linux:

```sh
go build -o bin/full-sidecar .
plugin_root="$(pwd -P)"
reasonix plugin install "$plugin_root" --dry-run
reasonix plugin install "$plugin_root" --yes
reasonix plugin doctor full-sidecar
```

From the same directory in PowerShell on Windows:

```powershell
go build -o bin/full-sidecar.exe .
$pluginRoot = (Resolve-Path .).Path
reasonix plugin install $pluginRoot --dry-run
reasonix plugin install $pluginRoot --yes
reasonix plugin doctor full-sidecar
```

Review the dry-run's `FULL TRUST` block before installing: the sidecar executes
outside the Reasonix sandbox and owns the `system_prompt` strategy slot. The
commands above copy the package into Reasonix's global plugin inventory.

Rebuilding the source does not update that copy; reinstall with
`--replace --yes`, then reload. For local iteration, use
`--link --replace --yes`; a linked installation also trusts future changes in
the source directory.

Start a new session, or run `/reload` while the current session is idle. Keep
your normal configured model selected for the following checks.

## Observe the installed contributions

1. Send an ordinary prompt to start a turn. Expect the `fullsidecar online`
   status and the `fullsidecar` card. Choose `Run demo` to open the form;
   frontends with extension slash actions also expose `/full-sidecar:demo`.

2. Enter a name and choose whether to shout. The host returns the answers to
   the sidecar, which publishes `Hello, <name>!` (uppercase when shouting).
   Dismissing the form cancels the demo without a greeting.

3. The strategy wraps the system prompt with
   `You are Reasonix running under the fullsidecar demo strategy.` The
   automated check below uses a separate recording provider to verify this
   at the provider request.

4. The installed provider appears as `plugin/full-sidecar/fake/echo`. It
   ignores the task and streams `fake-hello fake-world`, a `lookup` tool call,
   and fixed usage numbers. This is a protocol fixture, not a model for normal
   work.

5. The `/fs ` prefix is a raw `input.receive` fixture, not a registered slash
   command. The host composes turn context before calling the interceptor, so
   its `text` need not begin with the user's text.

6. Some frontends refuse unknown slash commands before a turn starts. Verify
   installation through the demo action and the host checks below, rather
   than `/fs hello` in a composer.

7. The tool interception fixtures block `dangerous_exec` and add a `sandbox`
   argument to `read`. These names and arguments do not grant or enforce host
   sandbox authority.

## Disable, restore, and remove

```sh
reasonix plugin disable full-sidecar
reasonix plugin enable full-sidecar
reasonix plugin remove full-sidecar --yes
```

After each change, start a new session or reload while idle. Disabling or
removing the package removes its strategy, provider entry, and actions from
the new runtime.

A disabled package remains installed and can be enabled
again. Removing a copied package deletes the installed copy; removing a
linked package leaves its source directory in place.

## Run the host checks

From the repository root:

```sh
go test ./internal/assembly/boot/ -run '^TestEffectFullsidecarInstalledHostSurfaces$' -count=1
go test ./internal/ext/extension/conformance/ -count=1
```

The boot effect test builds this actual SDK example without fetching modules,
previews and applies a copy installation in an isolated home, removes the
source, and drives the real boot assembly across absent, installed, disabled,
reenabled, and removed states.

It asserts the strategy at the provider
request, the provider descriptor in the merged catalog, and the status,
card, blocking form, and answered greeting at the controller's frontend
event sink. It also waits for each started sidecar to exit on close.

Conformance separately checks raw input and tool interception, provider
streaming and cancellation, protocol validation, and shutdown. Neither suite
proves a live model's task quality or a browser's rendering of the surfaces.
