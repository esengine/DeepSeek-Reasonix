# reasonix-remote-gateway

Low-bandwidth, message-only remote access gateway for Reasonix Studio. It
authenticates registered devices and one-time controller grants through the
account service, then joins both WebSockets in a per-device Durable Object.

The gateway treats message bodies as opaque end-to-end encrypted payloads. It
does not persist messages, proxy files, or carry desktop video. Idle sockets use
the Durable Objects Hibernation API.

## WebSocket endpoints

- `GET /v1/devices/:deviceId/connect` with `Authorization: Bearer <deviceCredential>`
- `GET /v1/sessions/connect` with `Authorization: Bearer <oneTimeGrant>`

Text messages are limited to 64 KiB and each device accepts at most four
controller connections. Each controller receives a private connection ID;
device replies must target that ID, so responses are never broadcast to other
controllers. The account service and this Worker share the
`REMOTE_GATEWAY_TOKEN` Worker secret.

Device admissions expire after 30 minutes and controller admissions after 15
minutes. Clients reconnect with fresh credentials so account or device
revocation takes effect without keeping a server-side revocation stream alive.
