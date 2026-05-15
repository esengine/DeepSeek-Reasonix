# QQ channel setup

Reasonix can attach QQ as a remote communication channel for existing `chat` and `code` sessions. QQ is not a separate runtime mode.

Once connected, QQ messages can be routed into the active session, and interactive prompts can continue remotely without terminal-side input.

## What it supports

The QQ channel can be used for:

- sending normal user messages into the active session
- receiving follow-up assistant replies in QQ
- handling slash commands from QQ
- handling confirmation and pause flows remotely
- continuing plan, checkpoint, and choice-style follow-up interactions through QQ

QQ acts as a remote surface for the same running `chat` or `code` session.

## Commands

Available commands inside a Reasonix session:

- `/qq connect`
- `/qq status`
- `/qq disconnect`

## Quick start

Start a session first:

~~~bash
reasonix code
# or
reasonix chat
~~~

Then connect QQ from inside the session:

~~~text
/qq connect
~~~

If credentials are already configured, Reasonix reuses them directly. If not, it prompts for the QQ Open Platform `App ID` and `App Secret`.

You can also provide credentials inline:

~~~text
/qq connect <appId> <appSecret> [sandbox|prod]
~~~

After a successful connection, later `chat` and `code` sessions auto-start the QQ channel when it is enabled.

## Runtime model

QQ is attached to the existing session runtime:

- `reasonix code` keeps filesystem, shell, and edit workflows
- `reasonix chat` stays chat-only
- QQ only adds a remote communication channel on top

This keeps the interaction model aligned with the rest of Reasonix instead of introducing a third mode.

## QQ Open Platform setup

To use the QQ channel, you need a bot application from QQ Open Platform.

The general setup flow is:

1. Sign in to QQ Open Platform.
2. Create a bot application.
3. Open the bot's developer settings.
4. Copy the `App ID` and `App Secret`.
5. Use those credentials with `/qq connect`.

Depending on your bot's environment, you may also need to choose `sandbox` or `prod`.

Official entry point: [QQ Open Platform](https://q.qq.com/)

## Registering a QQ bot

The QQ Open Platform UI may change over time, but the usual process is:

1. Open the QQ Open Platform developer console.
2. Create a new bot application.
3. Complete the required registration fields.
4. Enable the bot capability for the application.
5. Copy the generated `App ID` and `App Secret`.
6. Use those credentials in Reasonix.

Example:

~~~text
/qq connect 1234567890 your_app_secret_here sandbox
~~~

Or run `/qq connect` and enter the values interactively when prompted.

## Typical workflow

1. Start `reasonix code`.
2. Run `/qq connect`.
3. Send a task from QQ.
4. Let the session continue in the terminal.
5. Receive confirmations or follow-up replies back in QQ.
6. Reply from QQ when approval or selection is required.

## Notes

- QQ does not replace `chat` or `code`; it extends them.
- `code` mode remains the only mode with filesystem and shell access.
- Auto-start only happens after QQ has been connected successfully and enabled.
- If QQ is disconnected, the terminal session continues normally.

## Troubleshooting

### `/qq connect` does not connect

Check that:

- your `App ID` is correct
- your `App Secret` is correct
- the bot application is enabled in QQ Open Platform
- you selected the correct environment (`sandbox` or `prod`)

### QQ messages arrive, but no reply is returned

Check that the active session is still running and that the QQ channel is still connected:

~~~text
/qq status
~~~

### The npm package does not show QQ commands

QQ support is only available in versions published after the QQ channel merge landed. If the published package is older than that merge, use the current repository `main` branch until a newer npm release is published.
