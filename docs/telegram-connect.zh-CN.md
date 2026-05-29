# Telegram 连接指南

Reasonix 可以把现有的 `chat` 或 `code` 会话延伸到 Telegram 上，作为远程通道使用。Telegram 扩展的是当前会话，不是独立的新运行模式。

连接成功后，Telegram 可以：

- 把普通消息送进当前会话
- 接收支持 Telegram Markdown 渲染的助手回复
- 用内联按钮继续确认、选择、checkpoint、plan 这类二次交互
- 把 Reasonix slash 命令注册成 Telegram bot command

## 开始前先准备

请先确认：

- 使用的是已经包含 Telegram 支持的较新 Reasonix 版本
- 已经有 Telegram 账号
- 已经从 BotFather 拿到 Telegram bot token
- 已经知道允许驱动 Reasonix 的 Telegram 数字用户 id

注意：

- Telegram 默认 fail-closed：没有配置 `telegram.ownerUserId` 或 `telegram.allowlist` 时，通道会拒绝启动
- bot token 要妥善保管，不要公开
- 只允许你信任的 Telegram 用户驱动这个代码执行 bot

## 获取 Telegram bot token

BotFather 的界面可能会变化，但通常流程是：

1. 在 Telegram 中打开 `@BotFather`
2. 发送 `/newbot`
3. 按提示填写 bot 名称和 username
4. 复制 BotFather 返回的 bot token

## 获取 Telegram 用户 id

Reasonix 的访问控制使用 Telegram 数字用户 id，不使用 `@username`。

常见获取方式：

- 给 `@userinfobot` 这类辅助 bot 发消息查看
- 给自己的 bot 发一条消息后，通过 Telegram `getUpdates` API 查看
- 从你信任的自有 bot 工具里读取

请把这个 id 保存为 `telegram.ownerUserId`，或加入 `telegram.allowlist`。

## 在 CLI 里连接

先启动一个会话：

~~~bash
reasonix code
# 或
reasonix chat
~~~

连接前先配置访问控制。例如编辑 `~/.reasonix/config.json`：

~~~json
{
  "telegram": {
    "ownerUserId": "123456789"
  }
}
~~~

然后运行：

~~~text
/telegram connect
~~~

首次连接时会这样引导：

1. 在当前 TUI 里提示你输入 Telegram bot token
2. 输入 `/cancel` 可以取消
3. 连接成功后，Reasonix 会保存 token 并启用后续自动启动

如果本地已经保存过 token，`/telegram connect` 会直接复用。

也可以直接一次性传参：

~~~text
/telegram connect <botToken>
~~~

其他相关命令：

- `/telegram status`
- `/telegram disconnect`

第一次连接成功后，只要 Telegram 保持启用，后续 `chat` 和 `code` 会话都会自动启动 Telegram 通道。

## 环境变量

也可以用环境变量配置 Telegram：

~~~bash
TELEGRAM_BOT_TOKEN=123456:botfather-token
TELEGRAM_OWNER_USER_ID=123456789
TELEGRAM_ALLOWLIST=123456789,987654321
~~~

环境变量会覆盖本地保存的 token 和访问控制配置。`TELEGRAM_ALLOWLIST` 支持用逗号或空白分隔多个 id。

## 典型使用方式

1. 启动 `reasonix code` 或 `reasonix chat`
2. 配置 `telegram.ownerUserId` 或 `telegram.allowlist`
3. 完成一次 `/telegram connect`
4. 从 Telegram 给 bot 发一条消息
5. 本地 Reasonix 会话继续运行
6. 需要时直接在 Telegram 里继续回复、确认或选择

Telegram 只是扩展当前会话，不替代 `chat` 或 `code`。

## 安全说明

- 访问控制默认 fail-closed；没有 owner 或 allowlist 时通道不会启动。
- 未授权用户的消息和按钮点击都会被忽略。
- 确认按钮会绑定到当前 Telegram 确认消息，旧确认里的按钮不能批准新的请求。
- 授权用户的消息在进入 agent prompt 路径前会被限流。

## 排障

### `/telegram connect` 报访问控制错误

至少配置一个允许访问的 Telegram 用户：

~~~json
{
  "telegram": {
    "ownerUserId": "123456789"
  }
}
~~~

或者：

~~~json
{
  "telegram": {
    "allowlist": ["123456789", "987654321"]
  }
}
~~~

然后重新连接：

~~~text
/telegram connect
~~~

### `/telegram connect` 报 token 错误

优先检查：

- token 是否完整复制自 BotFather
- 是否多了空格或 shell 引号问题
- BotFather 里这个 bot 是否已删除或重新生成 token

必要时可以直接显式传参重试：

~~~text
/telegram connect <botToken>
~~~

### Telegram 能收到消息，但没有后续回复

先确认本地 Reasonix 会话还在运行，而且 Telegram 通道仍然在线：

~~~text
/telegram status
~~~

### Telegram 提示已有另一个 poller

同一个 Telegram bot 同一时间只能有一个 long-polling 进程。请停止其他使用同一个 token 的 Reasonix 进程或 bot 进程，然后重新连接。

### 已安装的 npm 版本里没有 `/telegram` 命令

说明本地包版本太旧。请升级到已经包含 Telegram 支持的发行版，或者直接使用仓库最新 `main` 分支。
