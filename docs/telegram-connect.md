# Telegram channel setup

Reasonix can attach Telegram to an existing `chat` or `code` session as a remote channel. Telegram is not a third runtime mode.

Once connected, Telegram can:

- send normal user messages into the active session
- receive follow-up assistant replies with Telegram Markdown rendering
- continue confirmation, choice, checkpoint, and plan-style follow-up interactions with inline buttons
- expose Reasonix slash commands through the Telegram bot command menu

## Before you start

Prepare these first:

- a recent Reasonix release that already includes Telegram support
- a Telegram account
- a Telegram bot token from BotFather
- the numeric Telegram user id that should be allowed to drive Reasonix

Important:

- Telegram is fail-closed: the channel refuses to start until `telegram.ownerUserId` or `telegram.allowlist` is configured
- keep the bot token private
- only allow Telegram users you trust to run a code-driving bot

## Get your Telegram bot token

The BotFather UI may change, but the flow is usually:

1. Open Telegram and start a chat with `@BotFather`.
2. Send `/newbot`.
3. Follow the prompts for bot name and username.
4. Copy the bot token BotFather returns.

## Get your Telegram user id

Reasonix access control uses Telegram's numeric user id, not the `@username`.

Common ways to get it:

- send a message to a helper bot such as `@userinfobot`
- use Telegram's `getUpdates` API after messaging your bot once
- read it from your own trusted bot tooling

Save this value as `telegram.ownerUserId` or include it in `telegram.allowlist`.

## Connect from the CLI

Start a session first:

~~~bash
reasonix code
# or
reasonix chat
~~~

Configure access control before connecting. For example, edit `~/.reasonix/config.json`:

~~~json
{
  "telegram": {
    "ownerUserId": "123456789"
  }
}
~~~

Then run:

~~~text
/telegram connect
~~~

First-time behavior:

1. Reasonix asks for the Telegram bot token in the current TUI.
2. Enter `/cancel` to abort.
3. Reasonix saves the token and enables Telegram auto-start after a successful connection.

If a token is already saved, `/telegram connect` reuses it directly.

You can also pass the token inline:

~~~text
/telegram connect <botToken>
~~~

Other Telegram commands:

- `/telegram status`
- `/telegram disconnect`

After the first successful connection, later `chat` and `code` sessions auto-start the Telegram channel while it stays enabled.

## Environment variables

You can also configure Telegram through environment variables:

~~~bash
TELEGRAM_BOT_TOKEN=123456:botfather-token
TELEGRAM_OWNER_USER_ID=123456789
TELEGRAM_ALLOWLIST=123456789,987654321
~~~

Environment values override the saved config for token and access checks. `TELEGRAM_ALLOWLIST` accepts comma- or whitespace-separated ids.

## Typical usage

1. Start `reasonix code` or `reasonix chat`.
2. Configure `telegram.ownerUserId` or `telegram.allowlist`.
3. Complete `/telegram connect` once.
4. Send a message to your Telegram bot.
5. Let the local Reasonix session keep running.
6. Continue replies, approvals, and follow-up interactions from Telegram when needed.

Telegram extends the current session. It does not replace `chat` or `code`.

## Security notes

- Access is fail-closed. With no owner or allowlist, the channel does not start.
- Unauthorized senders and button presses are ignored.
- Confirmation buttons are bound to the current Telegram confirmation message, so stale buttons from older confirmations cannot approve newer requests.
- Authorized users are rate-limited before their messages reach the agent prompt path.

## Troubleshooting

### `/telegram connect` fails with an access control error

Configure at least one allowed Telegram user:

~~~json
{
  "telegram": {
    "ownerUserId": "123456789"
  }
}
~~~

or:

~~~json
{
  "telegram": {
    "allowlist": ["123456789", "987654321"]
  }
}
~~~

Then reconnect:

~~~text
/telegram connect
~~~

### `/telegram connect` fails with a token error

Check these first:

- the token was copied exactly from BotFather
- there are no extra spaces or shell quoting mistakes
- the bot has not been deleted or regenerated in BotFather

If needed, reconnect with an explicit token:

~~~text
/telegram connect <botToken>
~~~

### Telegram receives the message, but no reply comes back

Check that the local Reasonix session is still running and the channel is still connected:

~~~text
/telegram status
~~~

### Telegram says another poller is active

Only one long-polling process can run for a Telegram bot at a time. Stop the other Reasonix process or any other bot process using the same token, then reconnect.

### `/telegram` commands do not exist in your installed package

Your installed npm version is too old. Upgrade to a release that already includes Telegram support, or use the current repository `main` branch.
