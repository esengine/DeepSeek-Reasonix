# Community Plugins

Reasonix loads external tools as **MCP servers** — subprocesses it talks to over
stdio JSON-RPC — declared in `reasonix.toml` (see
[SPEC §3.3](./SPEC.md#33-plugins-internalplugin--mcp-client) and
[§5](./SPEC.md#5-configuration-toml)). Plugins live in their own repos/packages and
are **not** bundled into core; this page is a discovery list so users can find them
and authors can be linked.

> This is a discovery list, not an endorsement or a quality guarantee. Vet a
> plugin before installing it — an installed MCP server is trusted with its tools.

## How to add a plugin

Each `[[plugins]]` entry names a server and how to launch it. `type` defaults to
`stdio`; `${VAR}` / `${VAR:-default}` are expanded in `command` / `args` / `env`.

```toml
[[plugins]]
name    = "example"
command = "npx"
args    = ["some-reasonix-plugin"]
# env   = { SOME_TOKEN = "${SOME_TOKEN}" }
```

After launch, a plugin's tools appear in-session namespaced as
`mcp__<name>__<tool>`. Run `/mcp` in the chat TUI to see connected servers and
their tool counts.

## Plugins

| Plugin | Tools | Install |
| --- | --- | --- |
| [reasonix-plugin-git-context](https://github.com/kashifmahi/reasonix-plugin-git-context) | Read-only git: `blame`, `log`, `show`, `diff`, `file_history`, `pickaxe`, `pr_context` | `npx reasonix-plugin-git-context` |

<!-- Add your plugin above in the same format: name (link), a short tool summary,
     and the launch command. Keep entries alphabetical by plugin name. -->

## Publishing your own

1. Build an MCP server (any language) that speaks stdio JSON-RPC — see
   `cmd/reasonix-plugin-example` for a runnable reference.
2. Declare its tools with honest `annotations` (`readOnlyHint` for safe readers).
3. Ship it as its own package with a README that includes the `[[plugins]]` snippet.
4. Open a PR adding a row to the table above.
