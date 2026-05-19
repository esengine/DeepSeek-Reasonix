# QQ 连接指南

Reasonix 可以把 QQ 挂到现有的 `chat` 或 `code` 会话上，作为远程通道使用。QQ 不是第三种运行模式。

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

1. 打开 [QQ 开放平台](https://q.qq.com/qqbot/openclaw/login.html) 并登录。
2. 创建 QQ 机器人。
3. 打开机器人的开发设置。
4. 复制 `App ID`。
5. 查看并保存 `App Secret`。

## 在 CLI 里连接

先启动一个会话：

~~~bash
reasonix code
# 或
reasonix chat
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

- `/qq status`
- `/qq disconnect`

第一次连接成功后，只要 QQ 保持启用，后续 `chat` 和 `code` 会话都会自动启动 QQ 通道。

## 在桌面端连接

如果你使用桌面客户端：

1. 打开 `Settings`
2. 进入 `General`
3. 在底部找到 `QQ Channel`
4. 点击 `Configure...`
5. 填入 `App ID`、`App Secret`，再选择 `Sandbox` / `Production`
6. 点击 `Save and connect`

桌面端和 CLI 复用同一份 QQ 配置。

## 典型使用方式

1. 启动 `reasonix code` 或 `reasonix chat`
2. 先完成一次 QQ 连接
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

必要时可以直接显式传参重连：

~~~text
/qq connect <appId> <appSecret> [sandbox|prod]
~~~

### QQ 能收到消息，但没有后续回复

先确认本地 Reasonix 会话还在运行，而且 QQ 通道仍然在线：

~~~text
/qq status
~~~

### 已安装的 npm 版本里没有 `/qq` 命令

说明本地包版本太旧。请升级到已经包含 QQ 支持的发行版，或者直接使用仓库最新 `main` 分支。
