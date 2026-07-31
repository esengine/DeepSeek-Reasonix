# Channel 模块

`Channel` 是在 `ws.Client` / `event.EventDispatcher` / `lark.Client` 之上封装的高层模块，把飞书机器人接入过程中的传输、消息归一化、安全策略、出站发送、流式回复、媒体上传、卡片交互、生命周期回调等杂活封装好，使用者只需要专注业务逻辑。

**什么时候用 Channel**：需要做会话式机器人（AI 对话、流式回复、卡片按钮、媒体上传、@所有人策略等）时。若只是收少量事件做简单处理，`ws.Client` + `event.EventDispatcher` 组合已经够用。

---

## 目录

- [最小示例](#最小示例)
- [事件监听](#事件监听)
- [生命周期回调](#生命周期回调)
- [策略控制](#策略控制)
- [`NormalizedMessage` 字段](#normalizedmessage-字段)
- [发送消息](#发送消息)
- [流式回复](#流式回复)
- [辅助能力](#辅助能力)
- [错误处理](#错误处理)
- [常见问题](#常见问题)

---

## 最小示例

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

	// 1. 初始化 API Client
	client := lark.NewClient(appID, appSecret, lark.WithLogLevel(core.LogLevelInfo))

	// 2. 初始化 WebSocket Client
	wsClient := larkws.NewClient(appID, appSecret, larkws.WithLogLevel(core.LogLevelInfo))

	// 3. 创建 Channel
	ch := channel.NewChannel(client, wsClient)

	// 4. 可选：注册生命周期回调
	ch.OnReady(func() {
		fmt.Println("channel 已就绪")
	})

	// 5. 注册事件处理器
	ch.OnMessage(func(ctx context.Context, msg *types.NormalizedMessage) error {
		fmt.Printf("收到消息：%s\n", msg.Content)

		_, err := ch.Send(ctx, &types.SendInput{
			ChatID:         msg.ChatID,
			Markdown:       fmt.Sprintf("已收到：%s", msg.Content),
			ReplyMessageID: msg.MessageID,
		})
		return err
	})

	// 6. 通过 Channel 启动，让生命周期回调由 Channel 完成接线
	if err := ch.Start(context.Background()); err != nil {
		panic(err)
	}

	// 7. 阻塞等待退出信号
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	<-stop

	_ = ch.Stop(context.Background())
}
```

建议使用 `ch.Start(ctx)`，不要直接调用 `wsClient.Start(ctx)`。因为 `Channel` 会在 `Start()` 中接入自己的 `OnReady` / 重连 / 断线 / 错误回调。

---

## 事件监听

```go
ch.OnMessage(func(ctx context.Context, msg *types.NormalizedMessage) error {
    // 文本 / 富文本 / 媒体消息
    return nil
})

ch.OnReaction(func(ctx context.Context, event *types.ReactionEvent) error {
    // emoji 表态新增 / 移除
    return nil
})

ch.OnBotAdded(func(ctx context.Context, event *types.BotAddedEvent) error {
    // bot 被拉进群
    return nil
})

ch.OnComment(func(ctx context.Context, event *types.CommentEvent) error {
    // 文档内 @bot 评论
    return nil
})

ch.OnCardAction(func(ctx context.Context, event *types.CardActionEvent) error {
    // 交互卡片回调
    return nil
})

ch.OnReject(func(ctx context.Context, event *types.RejectEvent) error {
    // 消息被策略门禁拦截
    return nil
})
```

---

## 生命周期回调

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

这些回调由 `ch.Start(ctx)` 完成接线。

---

## 策略控制

默认行为：

- 群聊默认要求显式 @bot
- 默认不响应 `@all`
- 单聊默认开放

可以在运行时调整策略：

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

如果你想观察哪些消息被策略拒绝，可以在 `OnReject(...)` 中统一处理，如 `no_mention`、`mention_all_blocked`、`group_not_allowed` 等原因。

---

## `NormalizedMessage` 字段

`message` 事件的 payload。飞书各种消息类型（text / post / image / file / audio / video / sticker / share_chat / share_user / merge_forward 等）都会被转换成统一的 markdown + XML-style 标签格式，开发者不需要自己解析飞书原生消息结构。

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `EventID` | string | 用于去重的唯一事件 ID |
| `MessageID` | string | 消息 ID |
| `ChatID` | string | 会话 ID |
| `ChatType` | string | 单聊 (`p2p`) / 群聊 (`group`) |
| `UserID` | string | 发送者用户 ID |
| `Content` | string | 归一化后的文本内容（markdown 格式，媒体用 XML-style 标签表示） |
| `RawContentType` | string | 原始飞书消息类型 |
| `Mentions` | `[]types.Mention` | @ 列表（不含 bot 自己） |
| `MentionAll` | boolean | 是否 @所有人 |
| `MentionedBot` | boolean | 是否 @了 bot 本身 |
| `Resources` | `[]types.Resource` | 消息里引用的资源（image / file / audio / video / sticker） |
| `CreateTimeMs` | int64 | 毫秒时间戳 |
| `RawEvent` | interface{} | 原始事件体 |

---

## 发送消息

`ch.Send(ctx, input)` 支持多种 input 类型：

```go
// 基础文本类
ch.Send(ctx, &types.SendInput{ChatID: chatID, Text: "plain text"})
ch.Send(ctx, &types.SendInput{ChatID: chatID, Markdown: "hello **world**"})
ch.Send(ctx, &types.SendInput{ChatID: chatID, Card: cardJSONV2})

// 媒体（传入本地路径，Uploader 会自动上传后再发送）
ch.Send(ctx, &types.SendInput{ChatID: chatID, ImagePath: "./fixtures/sample.png"})
ch.Send(ctx, &types.SendInput{ChatID: chatID, FilePath: "./fixtures/doc.pdf"})

// 回复消息
ch.Send(ctx, &types.SendInput{
    ChatID:         chatID,
    Markdown:       "please check",
    ReplyMessageID: msg.MessageID,
    Mentions:       msg.Mentions, // 结构化 @
})
```

**不要在文本里手写 `@用户名`**。请用 `Mentions` 结构化传入 `[]types.Mention`，SDK 负责正确拼装 Feishu 的 @ 占位符。

### 自动降级

出站发送内置自动降级：

- **回复目标已撤销**（`target_revoked`）-> 自动去掉 `ReplyMessageID`，重发为普通消息
- **post 结构校验失败**（`format_error`）-> 如果有 `Markdown` 或 `Text`，自动降级为纯文本重发

这两条对调用方透明。如果降级后仍然失败，向调用方返回错误。

---

## 流式回复

`ch.Stream(ctx, input)` 会返回一个 `types.StreamController`。当前有两种模式：

### Markdown 流

文本流请使用 `Append()`：

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

行为说明：

- 当 `Card` 为空时，`Channel` 会先发一条初始消息，再返回面向 markdown 的流控制器
- 如果 `Markdown` 和 `Text` 都为空，初始内容会默认设置为 `"..."`，这样后续有消息可更新
- `Append()` 会做节流更新
- `Flush()` 会立即把缓冲内容推送出去
- 如果内容长度超过 `TextChunkLimit`，SDK 会通过后续 reply 消息继续延展这次流式输出

### Card 流

如果初始输入里包含 `Card`，`Stream()` 返回的是卡片流控制器，此时应使用 `UpdateCard()` 而不是 `Append()`：

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

Card 流不支持 `Append()`；Markdown 流不支持 `UpdateCard()`。

---

## 辅助能力

除了收发消息外，`Channel` 还提供了一些常用辅助能力：

```go
// 通过 file key 下载媒体
data, err := ch.DownloadFile(ctx, fileKey, "image")

// 获取并缓存 bot 身份
bot := ch.GetBotIdentity(ctx)
```

`DownloadFile()` 支持 `image`、`file`、`audio`、`video`、`media`。

返回的 `bot` 信息包含 `OpenID`、`Name` 以及 `ActivateStatus`。

`GetBotIdentity()` 使用内存缓存，默认 TTL 为 `30m`。当缓存过期时，刷新尝试会受 `MinRefreshInterval` 限制；该值默认 `1m`，最小保护为 `30s`。

```go
ch := channel.NewChannel(client, wsClient,
    types.WithBotIdentityCacheConfig(types.BotIdentityCacheConfig{
        TTL:                10 * time.Minute,
        MinRefreshInterval: 2 * time.Minute,
    }),
)
```

---

## 错误处理

出站失败会被归类为 `*types.FeishuChannelError`：

```go
var channelErr *types.FeishuChannelError
if errors.As(err, &channelErr) {
    fmt.Println("code:", channelErr.Code)
    fmt.Println("message:", channelErr.Message)
}
```

稳定错误码包括：

- `target_revoked`：回复目标已删除 / 撤回
- `permission_denied`：鉴权失败 / 权限不足
- `format_error`：消息格式校验失败
- `rate_limited`：HTTP 429 或等价限流
- `ssrf_blocked`：远端媒体源被 SSRF 防护拒绝
- `send_timeout`：网络超时 / deadline exceeded
- `unknown`：其他未归类错误

对于可重试的发送失败，SDK 会先自动重试，再向调用方返回错误。

---

## 常见问题

1. **不要在回复文本里写 `@用户名`**。请使用 `SendInput.Mentions` 结构化传参。
2. **群消息是否触发取决于“订阅权限 + 策略配置”两层。** 你可能需要申请 `im:message.group_msg`，也可能需要通过 `UpdatePolicy(...)` 放宽 `RequireMention` / `GroupAllowlist`。
3. **卡片按钮不响应**通常是：没加 `card.action.trigger` 订阅，或者卡片仍是 V1 schema。V2 建议使用 `column_set` -> `column` -> `button`，并配置 `behaviors: [{ type: "callback", value }]`。高层处理入口是 `OnCardAction(...)`。
4. **流式请用 `Stream()`**。Markdown 流用 `Append()`，卡片流用 `UpdateCard()`，不要手动循环 `Send()` 模拟流式。
5. **请通过 `ch.Start(ctx)` 启动 Channel**。如果你直接启动 `wsClient`，`Channel` 自己的 `OnReady()` / `OnReconnected()` 等生命周期回调不会由 `Channel` 接线。
