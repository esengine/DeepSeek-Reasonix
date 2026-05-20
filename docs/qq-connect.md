# QQ channel setup

Reasonix can attach QQ to an existing `chat` or `code` session as a remote channel. QQ is not a third runtime mode.

Once connected, QQ can:

- send normal user messages into the active session
- receive follow-up assistant replies
- continue confirmation, choice, checkpoint, and plan-style follow-up interactions

## Before you start

Prepare these first:

- a recent Reasonix release that already includes QQ support
- a QQ account that has completed real-name verification
- a QQ bot `App ID` and `App Secret` from QQ Open Platform

QQ Open Platform entry:

- [QQ Open Platform](https://q.qq.com/qqbot/openclaw/login.html)

Important:

- save the `App Secret` when it is shown
- depending on your bot, you may need `sandbox` or `prod`

## Get your QQ bot credentials

The exact QQ Open Platform UI may change, but the flow is usually:

1. Open [QQ Open Platform](https://q.qq.com/qqbot/openclaw/login.html) and sign in.
2. Create a QQ bot.
3. Open the bot's developer settings.
4. Copy the `App ID`.
5. Reveal and save the `App Secret`.

## Connect from the CLI

Start a session first:

~~~bash
reasonix code
# or
reasonix chat
~~~

Then run:

~~~text
/qq connect
~~~

First-time behavior:

1. Reasonix asks for the QQ `App ID` in the current TUI.
2. Then it asks for the `App Secret`.
3. Enter `/cancel` at either step to abort.

The prompts and `/qq` status messages follow the current CLI language.

If credentials are already saved, `/qq connect` reuses them directly.

You can also pass credentials inline:

~~~text
/qq connect <appId> <appSecret> [sandbox|prod]
~~~

Other QQ commands:

- `/qq status`
- `/qq disconnect`

After the first successful connection, later `chat` and `code` sessions auto-start the QQ channel while it stays enabled.

## Desktop quick start

If you use the desktop client:

1. Open `Settings -> General -> QQ Channel`.
2. Click `Configure`.
3. Enter `App ID`, `App Secret`, and the correct QQ environment.
4. Click `Save and connect`.
5. Send a message from QQ and check that it appears in the current desktop transcript.
6. Wait for the desktop reply to route back to QQ.

The desktop app uses the same underlying QQ config as the CLI, but the runtime is attached to the current active desktop tab.

That means:

- QQ messages enter the current active tab
- replies from that tab route back to QQ
- if you switch tabs, later QQ messages follow the new active tab

## Typical usage

1. Start `reasonix code` or `reasonix chat`.
2. Connect QQ once.
3. Send a message from QQ.
4. Let the local Reasonix session keep running.
5. Continue replies, approvals, and follow-up interactions from QQ when needed.

QQ extends the current session. It does not replace `chat` or `code`.

## Troubleshooting

### `/qq connect` fails on first setup

Check these first:

- `App ID` is correct
- `App Secret` is correct
- the QQ bot is enabled in QQ Open Platform
- you selected the right environment: `sandbox` or `prod`

If needed, reconnect with explicit arguments:

~~~text
/qq connect <appId> <appSecret> [sandbox|prod]
~~~

### QQ receives the message, but no reply comes back

Check that the local Reasonix session is still running and the channel is still connected:

~~~text
/qq status
~~~

### Desktop shows QQ configured, but no message round-tripping happens

First confirm you are using a desktop build that already includes desktop QQ runtime support.

Then check:

- the status in `Settings -> General -> QQ Channel`
- that the current active desktop tab is the one you expect QQ to drive
- that the local desktop session is still running

### `/qq` commands do not exist in your installed package

Your installed npm version is too old. Upgrade to a release that already includes QQ support, or use the current repository `main` branch.
