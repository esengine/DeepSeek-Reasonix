---
owner: @esengine
backup: @SivanCola
status: active
reviewed: 2026-10-01
---

# A local stdio MCP package

This native plugin bundles a small Go MCP server. It exposes one tool,
`count_lines`, that counts lines in text supplied by its caller. It does not
read files, write files, use credentials, or access the network.

The server uses only the Go standard library; it is separate from the Reasonix extension
SDK and declares no extension runtime, skills, hooks, or themes.

## Build and check the server

From this example directory, with Go installed:

```sh
mkdir -p bin
go test main.go main_test.go
go build -o bin/line-counter.exe main.go
```

The same output filename works on macOS, Linux, and Windows: `.exe` is a
required suffix on Windows and an ordinary filename suffix elsewhere.

In PowerShell, create the directory with
`New-Item -ItemType Directory -Force bin`, then run the same Go commands.
Build on the machine that will run the server; a binary for another OS or
architecture cannot be launched there.

The manifest's `${REASONIX_PLUGIN_ROOT}` points at the installed or linked
package root. Keep `bin/line-counter.exe` inside that root. `bin/` is ignored
by Git, so a fresh checkout has to be built before installation. This is a
local source example, not a remotely installable package containing a
cross-platform binary.

For a model-free protocol check on macOS/Linux:

```sh
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"local-check","version":"1"}}}' \
  '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' \
  '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"count_lines","arguments":{"text":"first\nsecond\n"}}}' \
  | ./bin/line-counter.exe
```

Expect three JSON response lines: initialization, one listed tool, and a
text content item containing `Line count: 2`. The notification has no
response. The process exits when its input closes.

The example negotiates
MCP `2025-03-26` and uses newline-delimited JSON-RPC on stdio; it does not
implement HTTP transport, prompts, resources, or server-initiated requests.

## Install a copy or develop through a link

Run from the built example directory:

```sh
reasonix plugin install . --dry-run
reasonix plugin install . --yes
reasonix plugin doctor mcp-line-counter-kit
reasonix plugin show mcp-line-counter-kit
```

The preview must describe a plugin install. `show` should list one stdio
MCP server, `line_counter`, and no other contributed capabilities. A healthy
manifest or successful copy does not prove the child process can start:
check the connection and tool result as described below.

For local development, use a link after reviewing its preview:

```sh
reasonix plugin install . --link --replace --dry-run
reasonix plugin install . --link --replace --yes
```

Keep the source directory in place while linked. Rebuild the binary after
editing Go source.

Restart the Reasonix session to load the new binary;
changing an executable on disk does not replace a child already running.
A copied install is independent of the source directory. Rebuild first,
then use `install . --replace --dry-run` and `install . --replace --yes`
to update that copy, and start a new session.

## Connect and verify through Reasonix

Start a fresh Reasonix session in a test workspace. In the MCP management
view, connect the configured `line_counter` server. The manifest leaves
the existing default loading policy intact.

Installing the package does
not make its tool a top-level provider tool or promise that it has already
started. An explicit connection verifies that the executable is launchable.

Ask the model to call `count_lines` with these JSON arguments and report
the tool result:

```json
{"text":"first\nsecond\n"}
```

The host exposes the connected tool as
`mcp__line_counter__count_lines`; the capability catalog identifies it as
`mcp-tool:line_counter/count_lines`. A model can discover it with
`use_capability` search and call it through that capability id.

Verify the returned content says `Line count: 2`. Count newline-separated
records, including blank lines; an empty string has zero lines, and one
trailing newline adds no extra record.

CRLF input follows the same count
because the separator is the newline character. This is a record count,
not a word count or a count of visual wrapped lines.

Ordinary model configuration and tool authorization still apply. The
offline tests use a scripted provider and do not establish that a live
model chose the right tool or interpreted a user's task correctly.

## Disable, restore, and remove

Close the test session before changing the package from a separate terminal:

```sh
reasonix plugin disable mcp-line-counter-kit
reasonix plugin enable mcp-line-counter-kit
reasonix plugin remove mcp-line-counter-kit --yes
```

Check each state in a fresh session. A disabled package retains its copied
files but contributes no configured server.

Re-enabling restores the
declaration; connect again if needed. Removal deletes a copied install and
its package registration; a linked source directory is left in place.
Closing the session shuts down the child process.

If connection fails, check the `show` launch path, confirm the binary exists
and matches the host platform, then run the local protocol check.

Keep logs
on stderr: a banner or debug print on stdout corrupts the JSON-RPC stream.
Malformed JSON, unknown methods, and invalid tool arguments receive JSON-RPC
errors.

The input scanner has a 1 MiB limit. The process exits with a stderr
diagnostic when reading or writing the stream fails.

The host regression builds this exact server offline, copies the package
through an approved install plan, deletes the original source, and checks
discovery and tool output at the provider boundary across its lifecycle.

These tests validate the local package path; they do not publish an MCP
registry entry or verify a downloadable release for every platform.
