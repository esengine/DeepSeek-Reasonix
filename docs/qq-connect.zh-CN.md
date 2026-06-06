# QQ 连接指南

Reasonix 可以把现有的 `chat`、`code` 或桌面端会话延伸到 QQ 上，作为远程通道使用。QQ 扩展的是当前会话，不是独立的新运行模式。

连接成功后，QQ 可以：

- 把普通消息送进当前会话
- 接收后续助手回复
- 继续确认、选择、checkpoint、plan 这类二次交互

## 开始前先准备

请先确认：

- 使用的是已经包含 QQ 支持的较新 Reasonix 版本
- QQ 账号已经完成实名认证
- 已经从 QQ 开放平台拿到机器人 `App ID` 和 `App Secret`

QQ 开放平台入口：

- [QQ 开放平台](https://q.qq.com/qqbot/openclaw/login.html)

注意：

- `App Secret` 显示时就要保存好
- 你的机器人环境可能需要选择 `sandbox` 或 `prod`

## 获取 QQ 机器人凭据

QQ 开放平台界面可能会变化，但通常流程是：

1. 打开 [QQ 开放平台](https://q.qq.com/qqbot/openclaw/login.html) 并登录
2. 创建 QQ 机器人
3. 打开机器人的开发设置
4. 复制 `App ID`
5. 查看并保存 `App Secret`

## 在 CLI 里连接

先启动一个会话：

~~~bash
reasonix code
# 或 reasonix chat
~~~

然后运行：

~~~text
/qq connect
~~~

首次连接时会这样引导：

1. 先在当前 TUI 里提示你输入 `App ID`
2. 再提示你输入 `App Secret`
3. 任一步输入 `/cancel` 都可以取消

这些提示和 `/qq` 结果会跟随当前 CLI 语言切换。
如果本地已经保存过凭据，`/qq connect` 会直接复用，不会重复询问。

也可以直接一次性传参：

~~~text
/qq connect <appId> <appSecret> [sandbox|prod]
~~~

其他相关命令：

- `/qq connect`
- `/qq status`
- `/qq disconnect`

第一次连接成功后，只要 QQ 保持启用，后续 `chat` 和 `code` 会话都会自动启动 QQ 通道。

## 桌面端快速上手

如果你使用桌面客户端：

1. 打开 `设置 -> 通用 -> QQ通道`
2. 点击 `配置`
3. 填入 `App ID` 和 `App Secret`
4. 选择正确的 QQ 环境：测试用 `沙箱`，正式机器人选 `正式`
5. 点击 `保存并连接`
6. 从 QQ 给机器人发一条消息，确认这条消息会出现在当前桌面会话记录里
7. 等待助手回复，确认回复会回到 QQ

桌面端和 CLI 复用同一份 QQ 配置。

### 当前活动标签页绑定

桌面端会把 QQ 绑定到**当前活动标签页**：

- 来自 QQ 的新消息会进入当前活动标签页
- 该标签页中的助手回复会回到同一个 QQ 通道
- 如果你切换到另一个标签页，后续 QQ 消息会进入新的当前标签页

## 典型使用方式

1. 启动 `reasonix code` 或 `reasonix chat`
2. 完成一次 QQ 连接
3. 从 QQ 发一条消息
4. 本地 Reasonix 会话继续运行
5. 需要时直接在 QQ 里继续回复、确认或选择

QQ 只是扩展当前会话，不替代 `chat` 或 `code`。

## 排障

### 首次 `/qq connect` 失败

优先检查：

- `App ID` 是否正确
- `App Secret` 是否正确
- QQ 开放平台里的机器人是否已启用
- 当前环境是否选对了：`sandbox` 或 `prod`

必要时可以直接显式传参重试：

~~~text
/qq connect <appId> <appSecret> [sandbox|prod]
~~~

### 桌面端里已经配置 QQ，但没有消息往返

优先检查：

- 使用的桌面端版本是否已经包含桌面 QQ runtime 支持
- `设置 -> 通用 -> QQ通道` 里当前状态是否正常
- 当前活动标签页是否仍然停留在你希望接收 QQ 消息的会话上
- 本地桌面会话是否仍然在运行

### QQ 能收到消息，但没有后续回复

先确认本地 Reasonix 会话还在运行，而且 QQ 通道仍然在线：

~~~text
/qq status
~~~

### 已安装的 npm 版本里没有 `/qq` 命令

说明本地包版本太旧。请升级到已经包含 QQ 支持的发行版，或者直接使用仓库最新 `main` 分支。
