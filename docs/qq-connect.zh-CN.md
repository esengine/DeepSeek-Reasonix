# QQ 连接指南

Reasonix 可以把 QQ 挂到现有的 `chat` 和 `code` 会话上，作为远程通信通道使用。QQ 不是一个独立的新运行模式。

连接成功后，QQ 消息可以进入当前会话；需要继续确认、选择或补充输入时，也可以直接在 QQ 里完成，不需要回到终端。

## 支持什么

QQ 通道可以用于：

- 把普通用户消息发送到当前会话
- 在 QQ 中接收后续助手回复
- 从 QQ 触发斜杠命令
- 远程处理确认和暂停类交互
- 继续处理 plan、checkpoint、choice 这类二次交互

换句话说，QQ 是同一个 `chat` 或 `code` 会话的远程交互面。

## 命令

在 Reasonix 会话内可用的 QQ 命令：

- `/qq connect`
- `/qq status`
- `/qq disconnect`

## 快速开始

先启动一个会话：

~~~bash
reasonix code
# 或
reasonix chat
~~~

然后在会话里连接 QQ：

~~~text
/qq connect
~~~

如果本地已经保存了凭据，Reasonix 会直接复用；如果没有，则会提示输入 QQ 开放平台的 `App ID` 和 `App Secret`。

也可以直接内联传入：

~~~text
/qq connect <appId> <appSecret> [sandbox|prod]
~~~

连接成功后，只要 QQ 保持启用，后续的 `chat` 和 `code` 会话都会自动启动 QQ 通道。

## 它和运行模式的关系

QQ 只是挂接到现有会话上的通道：

- `reasonix code` 仍然负责文件、Shell 和编辑流程
- `reasonix chat` 仍然保持纯聊天
- QQ 只是在其上增加一个远程通信入口

这样可以保持现有交互模型的一致性，而不是引入第三种模式。

## QQ 开放平台准备

要使用 QQ 通道，需要先在 QQ 开放平台创建一个机器人应用。

一般流程是：

1. 登录 QQ 开放平台。
2. 创建机器人应用。
3. 打开该机器人的开发设置。
4. 复制 `App ID` 和 `App Secret`。
5. 在 Reasonix 里用 `/qq connect` 录入这些凭据。

根据机器人当前环境，你可能还需要选择 `sandbox` 或 `prod`。

官方入口：[QQ 开放平台](https://q.qq.com/)

## 如何申请 QQ 机器人

QQ 开放平台界面可能会调整，但通常流程是：

1. 打开 QQ 开放平台开发者控制台。
2. 创建一个新的机器人应用。
3. 按要求填写注册信息。
4. 为该应用启用机器人能力。
5. 在开发设置里复制 `App ID` 和 `App Secret`。
6. 回到 Reasonix 完成连接。

例如：

~~~text
/qq connect 1234567890 your_app_secret_here sandbox
~~~

或者直接运行 `/qq connect`，按提示交互式输入。

## 典型使用方式

1. 启动 `reasonix code`
2. 运行 `/qq connect`
3. 从 QQ 发起一个任务
4. 让会话在终端继续执行
5. 在 QQ 中接收确认提示或后续回复
6. 当需要确认或选择时，直接在 QQ 中回复

## 说明

- QQ 不替代 `chat` 或 `code`，只是扩展它们
- 只有 `code` 模式具备文件系统和 Shell 能力
- 只有在 QQ 成功连接并启用之后，后续会话才会自动启动 QQ 通道
- 如果 QQ 断开，终端会话本身仍然可以继续运行

## 排查

### `/qq connect` 没有连接成功

请检查：

- `App ID` 是否正确
- `App Secret` 是否正确
- QQ 开放平台上的机器人应用是否已启用
- 当前选择的环境是否正确（`sandbox` 或 `prod`）

### QQ 能收到消息，但没有后续回复

先确认当前会话还在运行，并且 QQ 通道仍然在线：

~~~text
/qq status
~~~

### npm 发布版里看不到 QQ 命令

QQ 支持只会出现在合并该功能之后发布的 npm 版本里。如果当前 npm 发布版早于该次合并，请先使用仓库的最新 `main` 分支，等后续 npm 版本发布。
