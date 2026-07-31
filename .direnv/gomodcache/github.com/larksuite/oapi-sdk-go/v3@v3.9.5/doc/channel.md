# Channel module

`Channel` is a high-level module built on top of `ws.Client` / `event.EventDispatcher` / `lark.Client`. It bundles the chores of running a Feishu bot — transport, message normalization, safety policy, outbound sending, streaming replies, media upload, card interactions, and lifecycle hooks — so you can focus on the business logic.

**When to use Channel**: conversational bots (AI chat, streaming replies, interactive card buttons, media handling, mention-all policy, etc.). If you only need to receive a few events and do simple processing, `ws.Client` + `event.EventDispatcher` is sufficient.

---

## Table of contents

- [Minimal example](#minimal-example)
- [Event listening](#event-listening)
- [Lifecycle hooks](#lifecycle-hooks)
- [Policy control](#policy-control)
- [`NormalizedMessage` fields](#normalizedmessage-fields)
- [Sending messages](#sending-messages)
- [Streaming replies](#streaming-replies)
- [Helper methods](#helper-methods)
- [Error handling](#error-handling)
- [Common issues](#common-issues)

---

## Minimal example

```go
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/larksuite/oapi-sdk-go/v3/channel"
	"github.com/larksuite/oapi-sdk-go/v3/channel/types"
	"github.com/larksuite/oapi-sdk-go/v3/core"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
)

func main() {
	appID := os.Getenv("FEISHU_APP_ID")
	appSecret := os.Getenv("FEISHU_APP_SECRET")

	// 1. Initialize API Client
	client := lark.NewClient(appID, appSecret, lark.WithLogLevel(core.LogLevelInfo))

	// 2. Initialize WebSocket Client
	wsClient := larkws.NewClient(appID, appSecret, larkws.WithLogLevel(core.LogLevelInfo))

	// 3. Create Channel
	ch := channel.NewChannel(client, wsClient)

	// 4. Optional lifecycle hooks
	ch.OnReady(func() {
		fmt.Println("channel is ready")
	})

	// 5. Register event handlers
	ch.OnMessage(func(ctx context.Context, msg *types.NormalizedMessage) error {
		fmt.Printf("received message: %s\n", msg.Content)

		_, err := ch.Send(ctx, &types.SendInput{
			ChatID:         msg.ChatID,
			Markdown:       fmt.Sprintf("received: %s", msg.Content),
			ReplyMessageID: msg.MessageID,
		})
		return err
	})

	// 6. Start Channel so lifecycle hooks are wired through Channel itself
	if err := ch.Start(context.Background()); err != nil {
		panic(err)
	}

	// 7. Wait for termination
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	<-stop

	_ = ch.Stop(context.Background())
}
```

Use `ch.Start(ctx)` instead of calling `wsClient.Start(ctx)` directly. `Channel` wires its own ready/reconnect/disconnect/error hooks during `Start()`.

---

## Event listening

```go
ch.OnMessage(func(ctx context.Context, msg *types.NormalizedMessage) error {
    // text / post / media messages
    return nil
})

ch.OnReaction(func(ctx context.Context, event *types.ReactionEvent) error {
    // emoji reaction added / removed
    return nil
})

ch.OnBotAdded(func(ctx context.Context, event *types.BotAddedEvent) error {
    // bot invited into a group
    return nil
})

ch.OnComment(func(ctx context.Context, event *types.CommentEvent) error {
    // doc comment mentioning the bot
    return nil
})

ch.OnCardAction(func(ctx context.Context, event *types.CardActionEvent) error {
    // interactive card callback
    return nil
})

ch.OnReject(func(ctx context.Context, event *types.RejectEvent) error {
    // message rejected by policy gate
    return nil
})
```

---

## Lifecycle hooks

```go
ch.OnReady(func() {
    fmt.Println("ready")
})

ch.OnError(func(err error) {
    fmt.Printf("channel error: %v\n", err)
})

ch.OnReconnecting(func() {
    fmt.Println("reconnecting")
})

ch.OnReconnected(func() {
    fmt.Println("reconnected")
})

ch.OnDisconnected(func() {
    fmt.Println("disconnected")
})
```

These hooks are attached by `ch.Start(ctx)`.

---

## Policy control

Default behavior:

- Group chats require mentioning the bot
- `@all` is blocked by default
- Direct messages are open by default

You can change the policy at runtime:

```go
requireMention := false
respondToMentionAll := true

ch.UpdatePolicy(types.PolicyConfig{
    GroupAllowlist:      []string{"oc_xxx"},
    RequireMention:      &requireMention,
    RespondToMentionAll: &respondToMentionAll,
    DMMode:              "allowlist",
    DMAllowlist:         []string{"ou_xxx"},
})
```

`OnReject(...)` is the place to observe policy drops such as `no_mention`, `mention_all_blocked`, or `group_not_allowed`.

---

## `NormalizedMessage` fields

Payload for the `message` event. Feishu's various message types (text / post / image / file / audio / video / sticker / share_chat / share_user / merge_forward / ...) are converted into a uniform markdown + XML-style-tag format, so you don't need to parse the raw Feishu message structure yourself.

| Field | Type | Description |
| --- | --- | --- |
| `EventID` | string | Unique event ID for deduplication |
| `MessageID` | string | Message id |
| `ChatID` | string | Chat id |
| `ChatType` | string | Direct message (`p2p`) / group (`group`) |
| `UserID` | string | Sender user id |
| `Content` | string | Normalized content (markdown, media rendered as XML-style tags) |
| `RawContentType` | string | Original Feishu message type |
| `Mentions` | `[]types.Mention` | @ list (excludes bot itself) |
| `MentionAll` | boolean | Whether `@all` was used |
| `MentionedBot` | boolean | Whether the bot itself was mentioned |
| `Resources` | `[]types.Resource` | Resources referenced in the message (image / file / audio / video / sticker) |
| `CreateTimeMs` | int64 | Millisecond timestamp |
| `RawEvent` | interface{} | Raw event body |

---

## Sending messages

`ch.Send(ctx, input)` supports various message inputs:

```go
// Text-based
ch.Send(ctx, &types.SendInput{ChatID: chatID, Text: "plain text"})
ch.Send(ctx, &types.SendInput{ChatID: chatID, Markdown: "hello **world**"})
ch.Send(ctx, &types.SendInput{ChatID: chatID, Card: cardJSONV2})

// Media (Uploader automatically uploads them before sending)
ch.Send(ctx, &types.SendInput{ChatID: chatID, ImagePath: "./fixtures/sample.png"})
ch.Send(ctx, &types.SendInput{ChatID: chatID, FilePath: "./fixtures/doc.pdf"})

// Reply to a message
ch.Send(ctx, &types.SendInput{
    ChatID:         chatID,
    Markdown:       "please check",
    ReplyMessageID: msg.MessageID,
    Mentions:       msg.Mentions, // structured @ mentions
})
```

**Do not hand-write `@username` in the text.** Pass `[]types.Mention` via `Mentions` and the SDK will assemble the Feishu @ placeholders correctly.

### Automatic fallbacks

Outbound sending has built-in fallbacks that are transparent to the caller:

- **Reply target revoked** (`target_revoked`) -> drop `ReplyMessageID` and resend as a fresh message
- **Post schema rejected** (`format_error`) -> fall back to a plain-text resend if `Markdown` or `Text` is available

If the fallback also fails, an error is returned.

---

## Streaming replies

`ch.Stream(ctx, input)` returns a `types.StreamController`. There are two modes:

### Markdown stream

Use `Append()` for text chunks:

```go
ch.OnMessage(func(ctx context.Context, msg *types.NormalizedMessage) error {
    streamCtrl, err := ch.Stream(ctx, &types.SendInput{
        ChatID:         msg.ChatID,
        ReplyMessageID: msg.MessageID,
        Title:          "Assistant",
    })
    if err != nil {
        return err
    }

    for chunk := range llmStream(msg.Content) {
        if err := streamCtrl.Append(ctx, chunk); err != nil {
            return err
        }
    }

    return streamCtrl.Close(ctx)
})
```

Behavior:

- If `Card` is empty, `Channel` sends an initial message and returns a markdown-oriented controller
- If both `Markdown` and `Text` are empty, the initial content defaults to `"..."` so the stream has a message to update
- `Append()` buffers text and updates the message with throttling
- `Flush()` forces an immediate update
- If the content grows beyond `TextChunkLimit`, the SDK continues the stream in follow-up reply messages

### Card stream

If the initial input contains `Card`, `Stream()` returns a card-oriented controller. Use `UpdateCard()` instead of `Append()`:

```go
streamCtrl, err := ch.Stream(ctx, &types.SendInput{
    ChatID: chatID,
    Card:   initialCardJSON,
})
if err != nil {
    return err
}

if err := streamCtrl.UpdateCard(ctx, nextCardJSON); err != nil {
    return err
}

return streamCtrl.Close(ctx)
```

`Append()` is not supported for card streams. `UpdateCard()` is not supported for markdown streams.

---

## Helper methods

Useful supporting APIs beyond send / receive:

```go
// Download media by key
data, err := ch.DownloadFile(ctx, fileKey, "image")

// Resolve and cache bot identity
bot := ch.GetBotIdentity(ctx)
```

`DownloadFile()` supports `image`, `file`, `audio`, `video`, and `media`.

The returned bot snapshot includes `OpenID`, `Name`, and `ActivateStatus`.

`GetBotIdentity()` uses in-memory caching with a default TTL of `30m`. When the cache is stale, refresh attempts are rate-limited by `MinRefreshInterval`, which defaults to `1m` and is clamped to a minimum of `30s`.

```go
ch := channel.NewChannel(client, wsClient,
    types.WithBotIdentityCacheConfig(types.BotIdentityCacheConfig{
        TTL:                10 * time.Minute,
        MinRefreshInterval: 2 * time.Minute,
    }),
)
```

---

## Error handling

Outbound failures are classified as `*types.FeishuChannelError`:

```go
var channelErr *types.FeishuChannelError
if errors.As(err, &channelErr) {
    fmt.Println("code:", channelErr.Code)
    fmt.Println("message:", channelErr.Message)
}
```

Stable error codes include:

- `target_revoked`: Reply target deleted / recalled
- `permission_denied`: Auth failure / insufficient scope
- `format_error`: Message schema validation failed
- `rate_limited`: HTTP 429 or equivalent throttling
- `ssrf_blocked`: Remote media source rejected by SSRF guard
- `send_timeout`: Network timeout / deadline exceeded
- `unknown`: Other unclassified failures

Retryable outbound failures are retried automatically before the error is returned.

---

## Common issues

1. **Don't write `@username` as literal text** in replies. Use the structured `SendInput.Mentions`.
2. **Group message handling depends on both subscription scope and policy.** You may need `im:message.group_msg`, and you may also need to relax `RequireMention` / `GroupAllowlist` via `UpdatePolicy(...)`.
3. **Card buttons not firing** is usually either a missing `card.action.trigger` subscription or a V1 card schema. V2 uses `column_set` -> `column` -> `button` with `behaviors: [{ type: "callback", value }]`. Handle it with `OnCardAction(...)`.
4. **Use `Stream()`** for progressive output. For markdown streams call `Append()`. For card streams call `UpdateCard()`.
5. **Start Channel through `ch.Start(ctx)`**. If you start `wsClient` directly, Channel lifecycle hooks such as `OnReady()` and `OnReconnected()` will not be wired by `Channel`.
