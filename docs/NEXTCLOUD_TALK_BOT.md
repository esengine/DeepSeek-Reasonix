# Nextcloud Talk bot adapter

This document describes the Nextcloud Talk bot integration added for #8117.

Nextcloud Talk exposes a signed webhook bot API (`bots-v1`). A Talk bot receives
ActivityStreams events and sends messages back through the Talk OCS bot message
endpoint. Reasonix uses that bot API directly rather than requiring a Nextcloud
user account or app password.

## Security model

The shared bot secret is not stored directly in Reasonix TOML configuration.
Reasonix stores an environment-variable reference and resolves the secret through
its normal secret store/environment handling.

Inbound requests are accepted only when both `X-Nextcloud-Talk-Random` and
`X-Nextcloud-Talk-Signature` are present and the HMAC-SHA256 signature matches
the random value plus the raw HTTP request body.

Outbound messages use `X-Nextcloud-Talk-Bot-Random` and
`X-Nextcloud-Talk-Bot-Signature`, with the same shared secret. The adapter also
sets `OCS-APIRequest: true` for the Talk OCS endpoint.

## Webhook runtime

The default local webhook endpoint is:

- listen address: `127.0.0.1:38017`
- path: `/reasonix/nextcloud-talk`

A reverse proxy can publish this loopback endpoint over HTTPS for a remote
Nextcloud server. The externally reachable URL is the URL supplied when the bot
is installed in Nextcloud Talk.

## Nextcloud-side registration

Use a shared secret between 40 and 128 characters and enable both webhook and
response features so Reasonix can receive and send messages:

```sh
sudo -u www-data php occ talk:bot:install \
  --feature webhook \
  --feature response \
  -- \
  "Reasonix" \
  "<40-to-128-character-shared-secret>" \
  "https://reasonix.example.com/reasonix/nextcloud-talk" \
  "Reasonix coding-agent bot"
```

After installing the bot, enable it for the required Talk conversation from the
Talk UI or with the appropriate `talk:bot:setup` command for the installed Talk
version.

## Desktop setup

Open **Settings -> Bots -> Nextcloud Talk** and configure:

1. Nextcloud server URL, for example `https://cloud.example.com`.
2. Local listen address, normally `127.0.0.1:38017`.
3. Webhook path, normally `/reasonix/nextcloud-talk`.
4. Environment-variable name for the shared secret, normally
   `NEXTCLOUD_TALK_BOT_SECRET`.
5. The shared secret value.

The desktop stores the non-secret fields in the normal `[[bot.connections]]`
record and stores the secret through Reasonix's secret handling. Per-connection
model, workspace, access controls, routes, session mappings, approvals, and Ask
flows use the same bot gateway infrastructure as the existing channels.

## Headless usage

The same configured connection works with the headless bot gateway:

```sh
reasonix bot doctor
reasonix bot start --channels nextcloud-talk --dir /path/to/project
```

`reasonix bot doctor` checks the Nextcloud server URL and shared-secret
environment reference. Desktop diagnostics also support a test send when a Talk
conversation token is available.

## Message mapping

For normal inbound `Create` / `Note` / `message` events:

- Talk conversation token -> `InboundMessage.ChatID`
- Talk message ID -> `InboundMessage.MessageID`
- actor identifier -> `InboundMessage.UserID` / `OperatorID`
- actor display name -> `InboundMessage.UserName`
- message text -> `InboundMessage.Text`

Events authored by an ActivityStreams `Application` actor are ignored to avoid
processing bot-originated events as new user turns.

Outbound text uses:

`POST /ocs/v2.php/apps/spreed/api/v1/bot/{token}/message`

and maps `OutboundMessage.ReplyToMsgID` to Talk's `replyTo` field when the ID is
numeric.

## Integration coverage

Nextcloud Talk is wired through the normal Reasonix bot stack:

- first-class `nextcloud-talk` platform identifier
- persistent connection configuration
- runtime adapter registration
- `reasonix bot start --channels nextcloud-talk`
- `reasonix bot doctor` checks
- desktop **Settings -> Bots** configuration
- desktop diagnostics and test-send support
- sidebar/session mapping support
- per-connection access controls and routing
- signed inbound and outbound requests
- focused Go tests for the adapter, runtime/config integration, and desktop
  credential round-trip
