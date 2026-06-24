# Reasoning Language

<a href="./GUIDE.md">Guide</a>
&nbsp;·&nbsp;
<a href="./REASONING_LANGUAGE.zh-CN.md">简体中文</a>

`agent.reasoning_language` controls the preferred language of visible
reasoning or thinking text when a provider exposes it.

It does not set the final answer language, rewrite code, translate identifiers,
or change hidden model reasoning. The user's explicit language request in a turn
still wins for the final answer.

## Why It Exists

Some users read visible reasoning more comfortably in Chinese or English even
when the task itself mixes languages. This setting makes that preference
explicit without changing the stable system prompt or tool definitions.

The setting is intentionally small:

- `auto` follows the conversation language and injects no extra instruction.
- `zh` asks visible reasoning to prefer Simplified Chinese.
- `en` asks visible reasoning to prefer English.

## Desktop

Open:

```text
Settings -> Models -> Usage -> Agent runtime -> Thinking language
```

The desktop setting writes the user-level default. A project can still override
it with `./reasonix.toml`.

## CLI And TUI

For shell scripts or one-off configuration:

```bash
reasonix config reasoning-language auto
reasonix config reasoning-language zh
reasonix config reasoning-language en
```

By default this writes the user config. To write a project-local override:

```bash
reasonix config reasoning-language --local zh
```

Inside `reasonix`, use the slash command:

```text
/reasoning-language auto
/reasoning-language zh
/reasoning-language en
```

The slash command writes the user-level setting and updates the current chat
controller for subsequent turns. It does not rewrite the current project's
`reasonix.toml`; use the shell command with `--local` for that.

Headless runs also use the same setting:

```bash
reasonix run "explain this module"
```

## Config File

User or project config:

```toml
[agent]
reasoning_language = "auto" # auto|zh|en
```

Resolution order for this setting:

```text
./reasonix.toml > user config.toml > built-in defaults
```

There is currently no command-line flag for this setting. Prefer config because
the value is a user or project preference rather than a per-invocation task
argument.

## Cache Behavior

`auto` is the cache-friendliest choice. It injects nothing and relies on the
existing stable language policy.

When set to `zh` or `en`, Reasonix adds a small transient
`<reasoning-language>` block to the user turn. It does not change:

- the system prompt
- tool schema bytes or ordering
- the stable provider-visible prefix

This keeps high prompt-cache hit rate intact while still letting an explicit
preference affect the next model call.

## Boundaries

- The setting only matters when visible reasoning text exists.
- It is a preference, not a hard translation layer.
- Code, identifiers, file paths, shell commands, and untranslated technical
  terms should remain in their original form.
- If a user asks for a final answer in a specific language, that request remains
  authoritative for the final answer.
