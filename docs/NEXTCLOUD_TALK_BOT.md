# Nextcloud Talk bot adapter

This document describes the initial Nextcloud Talk adapter added for #8117.

Nextcloud Talk exposes a webhook bot API (`bots-v1`). A Talk bot receives signed
ActivityStreams events and sends messages back through the Talk OCS bot message
endpoint. The Reasonix adapter follows that model rather than requiring a
Nextcloud user account or app password.

## Security model

The shared bot secret must be stored outside the Reasonix TOML configuration and
referenced through an environment variable.

Inbound requests are accepted only when both `X-Nextcloud-Talk-Random` and
`X-Nextcloud-Talk-Signature` are present and the HMAC-SHA256 signature matches
the random value plus the raw HTTP request body.

Outbound messages use `X-Nextcloud-Talk-Bot-Random` and
`X-Nextcloud-Talk-Bot-Signature`, with the same shared secret. The adapter also
sets `OCS-APIRequest: true` for the Talk OCS endpoint.

## Webhook runtime

The adapter defaults to:

- listen address: `127.0.0.1:38017`
- path: `/reasonix/nextcloud-talk`

A reverse proxy can publish this local path over HTTPS for the Nextcloud server.
The externally reachable URL is the URL supplied when installing the Talk bot.

Example Nextcloud-side registration:

```sh
sudo -u www-data php occ talk:bot:install \
  "Reasonix" \
  "<shared-secret>" \
  "https://reasonix.example.com/reasonix/nextcloud-talk"
```

Use the exact command/options supported by the installed Nextcloud Talk version.
The shared secret should then be exposed to Reasonix through the environment
variable configured for the connection.

## Message mapping

For inbound `Create` / `Note` / `message` events:

- Talk conversation token -> `InboundMessage.ChatID`
- Talk message ID -> `InboundMessage.MessageID`
- actor identifier -> `InboundMessage.UserID` / `OperatorID`
- actor display name -> `InboundMessage.UserName`
- message text -> `InboundMessage.Text`

Events authored by an ActivityStreams `Application` actor are ignored to avoid
processing bot-originated messages as new user turns.

Outbound text uses:

`POST /ocs/v2.php/apps/spreed/api/v1/bot/{token}/message`

and maps `OutboundMessage.ReplyToMsgID` to Talk's `replyTo` field when the ID is
numeric.

## Integration status

The package in `internal/bot/nextcloudtalk` provides the authenticated webhook
receiver, outbound sender, message mapping, loop protection, and unit tests.

The remaining integration work for full end-user support is intentionally kept
separate so the connection configuration shape can be reviewed before it is
made persistent:

- register the adapter in `internal/botruntime`
- add explicit Nextcloud server/listen/secret fields to bot connection config
- expose Nextcloud Talk in desktop **Settings -> Bots**
- add `nextcloud-talk` to the CLI channel selector and bot doctor output
- add desktop connection diagnostics/test-send support

Until those wiring changes land, this adapter is a foundation for #8117 rather
than a user-visible stable channel.


## Desktop setup

Reasonix exposes Nextcloud Talk under **Settings -> Bots -> Nextcloud Talk**.
The desktop form stores the server URL, listener and webhook path in the normal
`[[bot.connections]]` record, while the shared secret is stored through the
Reasonix secret environment store.

The same connection can be used by the headless gateway:

```sh
reasonix bot start --channels nextcloud-talk --dir /path/to/project
```
