# Lark (飞书) Bot

Reasonix can operate as a Lark bot, letting you interact with it through Lark
chat — group conversations or direct messages. Responses stream progressively,
tool approvals use interactive card buttons, and each chat maintains an isolated
session.

## Prerequisites

- A **self-built** Lark/Feishu app with bot capability enabled
- App credentials: `App ID` and `App Secret` from the [Feishu Developer Console](https://open.feishu.cn/app)

## App Setup

1. **Create a self-built app** at [open.feishu.cn](https://open.feishu.cn/app)
2. Go to **Features → Bot** and enable the bot capability
3. Go to **Permissions** and add:
   - `im:message` (read messages)
   - `im:message:send_as_bot` (send messages as bot)
4. Go to **Events & Callbacks → Event Subscription**:
   - Subscription mode: **Receive events through persistent connection** (使用长连接接收事件)
   - Add event: `im.message.receive_v1` (receive messages)
5. Go to **Version Management → Create version** and publish (requires admin approval)

## Quick Start

### 1. Set up credentials

Add your credentials to the Reasonix credentials file at
`~/Library/Application Support/reasonix/credentials` (macOS) or
`~/.config/reasonix/credentials` (Linux):

```
LARK_APP_ID=cli_xxxx
LARK_APP_SECRET=your_app_secret
DEEPSEEK_API_KEY=sk-xxxx
```

Or export them as environment variables in your shell profile (`~/.zshrc`, `~/.bashrc`):

```bash
export LARK_APP_ID="cli_xxxx"
export LARK_APP_SECRET="your_app_secret"
```

### 2. Configure `reasonix.toml`

Add a `[lark]` section to your Reasonix config. The minimal config uses the
Provider-style env var pattern:

```toml
# ~/Library/Application Support/reasonix/config.toml (macOS)
# or ~/.config/reasonix/config.toml (Linux)

[lark]
app_id_env     = "LARK_APP_ID"
app_secret_env = "LARK_APP_SECRET"
```

Alternatively, use `${VAR}` expansion:

```toml
[lark]
app_id     = "${LARK_APP_ID}"
app_secret = "${LARK_APP_SECRET}"
```

### 3. Start the bot

```bash
reasonix lark
```

You should see:

```
reasonix lark — connected to Lark bot
lark bot ready
```

Now send a message to the bot in Lark — either in a group where the bot is a
member, or via direct message by searching for the app name.

## Configuration Reference

All fields are optional except credentials. Defaults are shown in comments.

```toml
[lark]
# ── Credentials (required) ──────────────────────────────────────
# Env var style (recommended — matches Provider api_key_env pattern):
app_id_env     = "LARK_APP_ID"
app_secret_env = "LARK_APP_SECRET"

# Direct style (supports ${VAR} expansion):
# app_id     = "${LARK_APP_ID}"
# app_secret = "${LARK_APP_SECRET}"

# ── Session Management ──────────────────────────────────────────
# session_ttl  = "1h"    # idle sessions expire after this duration
# max_sessions = 50      # concurrent chat sessions, 0 = unlimited

# ── Permissions ─────────────────────────────────────────────────
# Per-context mode: "read-only" | "interactive" | "bypass"
# group_permission = "read-only"    # groups: plan mode, no writes
# dm_permission    = "interactive"  # DMs: prompt before writes

# ── Message Policy ──────────────────────────────────────────────
# require_mention       = false   # when true, only respond to @bot in groups
# respond_to_mention_all = false   # when true, respond to @all mentions
# allow_groups          = []      # group allowlist (empty = all)
# allow_dms             = []      # DM user allowlist (empty = all)

# ── Output Formatting ───────────────────────────────────────────
# show_tool_progress  = false   # true = inline tool markers; false = summary at end
# show_reasoning      = false   # include thinking/reasoning in output
# max_response_length = 8000    # truncate responses exceeding this (chars)
# approval_timeout    = "5m"    # auto-deny pending approvals after this
```

### Permission Modes

| Mode | Behavior |
|------|----------|
| `read-only` | Bot runs in plan mode — it can read files and research, but cannot write or execute side-effecting commands |
| `interactive` | Bot prompts for approval via interactive cards before executing write operations |
| `bypass` | Bot auto-approves all tool calls without prompting (use with caution) |

## Slash Commands

In Lark chat, the bot responds to these slash commands:

| Command | Description |
|---------|-------------|
| `/new` | Reset the session — starts a fresh conversation |
| `/model <name>` | Switch to a different configured model (e.g., `/model deepseek-pro`) |

## How It Works

```
User sends message in Lark
        │
        ▼
Lark pushes event via WebSocket
        │
        ▼
SDK Channel.OnMessage → session router
        │
        ▼
control.Controller processes the turn
        │
        ▼
Events stream back via ch.Stream()
        │
        ▼
Model text → progressive Lark message
Tool calls → accumulated silently
        │
        ▼
TurnDone → tool summary + token count
           sent as a markdown message
```

### Message Format

Each turn produces up to two messages:

1. **Streamed reply** — model text only, progressive
2. **Tool summary** (markdown) — only if tools were used:

   ```
   **3 tool calls**
   `read_file` ✅: main.go (2.1KB)
   `grep` ✅: 15 matches
   `bash` ✅: tests passed

   11,750 tokens
   ```

### Approval Flow

When the model needs to run a write operation:

1. Current stream is closed
2. An interactive card is sent with `[Allow]` `[Deny]` `[Always Allow]` buttons
3. User clicks → approval handler resolves → new stream opens
4. Clicking "Always Allow" persists the rule to your Reasonix config

## Troubleshooting

### Bot doesn't respond to messages

1. Make sure the app is **published** (not just in development mode)
2. Verify the bot is a member of the chat (group) or you're in a DM with the bot
3. Check that `im.message.receive_v1` event subscription is added and event mode is "persistent connection"
4. Check the terminal logs for any errors

### Authentication fails

1. Verify `DEEPSEEK_API_KEY` is set in `~/.env` or `credentials` file
2. Run `reasonix setup` to reconfigure the model provider
3. Check that your API key is not expired

### Session feels slow on first message

The first message in a chat creates a new controller via `boot.Build`, which
loads config, tools, plugins, memory, and skills. This takes 2-5 seconds.
Subsequent messages reuse the session and respond quickly.

### Config section lost after `reasonix setup`

Reasonix v1.0+ uses merge-on-save for existing config files. If your `[lark]`
section disappears, make sure your Reasonix binary is up to date
(`reasonix --version`).
