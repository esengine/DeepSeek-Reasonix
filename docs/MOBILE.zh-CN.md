# Reasonix Mobile — 产品规格（冻结基线）

本文冻结 iOS / Android 双运行模式客户端的产品与协议契约。实现以本文为准；
破坏性变更必须提升移动信封版本号。

## 目标

- 统一 React + Capacitor 应用，覆盖 iPhone、iPad、Android 手机与平板。
- 两种**创建后不可变**的会话运行时：
  - `local`：手机经 `mobilecore` 直连 OpenAI-compatible / Anthropic-compatible 供应商，运行轻量 Agent。
  - `remote`：连接电脑或服务器上的 Reasonix Node，运行完整 Shell、Git、文件、MCP 与后台任务。
- 本地供应商与局域网 / Tailscale 直连默认无需账号；官方中继、跨网配对与推送需要 Reasonix 账号。
- 最低系统：iOS 17、Android 10（API 29）。渠道：App Store、Google Play、签名 APK；中国大陆安卓商店为后续阶段。

## 非目标（v1）

- 手机本地完整编码环境（不含 Shell / Git / 任意文件系统 / stdio MCP）。
- 完整会话内容与 API Key 的云端端到端同步。
- React Native 或双端独立业务实现。

## 架构

```
┌──────────────── mobile (Capacitor) ────────────────┐
│  UI  →  SessionBackend  →  LocalBackend | RemoteBackend  │
└───────────────┬───────────────────┬────────────────┘
                │                   │
        mobilecore (Go)      WebSocket MobileEnvelope
        Keychain/Keystore    (局域网 / Tailscale / 中继)
                │                   │
         Provider APIs        reasonix node（多会话）
```

### SessionBackend

UI 只依赖 `SessionBackend`。`runtime` 在会话创建后不可原地修改。本地与远程迁移只能通过「复制到此设备」或「交给节点」创建**新**会话。

### MobileEnvelope

```ts
MobileEnvelope {
  version: number
  type: string         // command | event | ack | hello | snapshot | error | ping | pong
  requestId?: string
  sessionId?: string
  seq?: number
  ack?: number
  payload?: unknown
}
```

- 写命令必须带 `requestId`，节点侧有限去重表；重试返回原结果。
- 事件使用每会话单调递增 `seq`（从 1 起）。
- 重连发送 `lastAckSeq`：游标有效则增量补发，过期则返回完整 snapshot。

### SessionDescriptor

```ts
SessionDescriptor {
  id: string
  runtime: "local" | "remote"   // 创建后不可变
  nodeId?: string
  providerRef?: string
  capabilities: string[]
  revision: number
  lastEventSeq: number
  title?: string
  status?: string
  updatedAt?: string
}
```

Go 侧新字段一律 `omitempty`，保证旧客户端与桌面 / CLI 可读写共存。

## 本地 Runtime（`mobilecore`）

- `gomobile bind` → Android AAR + iOS XCFramework；经 Capacitor 原生插件暴露 JSON API。
- 复用 Go 侧 Provider 序列化、reasoning 适配、重试、usage、compaction、eventwire 与缓存诊断；**禁止**在 TS / Kotlin / Swift 复制 Provider 协议。
- 固定 API：`CreateSession`、`RestoreSession`、`Submit`、`Cancel`、`Answer`、`Approve`、`Snapshot`、`SubscribeEvents`、`ListModels`、`ProbeProvider`。
- 本地工具仅：网页读取、用户授权附件读取、图片输入、HTTP MCP。
- 工具集合与顺序在会话创建时冻结；设备动态状态只作 user-turn 注入（缓存优先）。
- API Key 存 Keychain / Keystore；会话快照 AES-GCM 加密后写入 App 私有目录。

## 远程 Runtime（`reasonix node`）

- 多会话守护进程；保留现有单会话 `reasonix serve`。
- 直连与官方中继共用同一 WebSocket 应用协议。
- 手机退后台、断网或被杀**不得**中止节点侧任务。
- 每会话单写入 Runtime、多观察客户端；lease 与 failure-atomicity 与桌面 / CLI 一致。

## 账号、中继与推送

- 云端仅存账号、设备/节点公钥、会话索引、在线状态与通知元数据。
- 中继只转发密文（Noise XX + AEAD）。优先已验证局域网 / Tailscale，再回退官方中继。
- APNs / FCM 仅发「完成 / 失败 / 需要审批」等最小信号。

## 导航与设计

底部四栏：**会话、节点、供应商、设置**。聊天、审批、Diff、附件为详情路由。

视觉：精密工业风 — 炭黑 / 冷灰表面、暖铜主强调色（与桌面 `#d97757` 家族对齐），成功绿 / 信息蓝 / 警告黄 / 危险红独立语义。完整深浅主题；圆角 ≤ 8px；不嵌套卡片；工具执行紧凑时间线；代码与 Diff 稳定等宽字体。

动效仅用于页面进入、流式状态、审批反馈、节点连接（120 / 180 / 260 ms）。禁止装饰性渐变球、模糊背景与持续无意义动画。

三语：英 / 简中 / 繁中。无障碍：VoiceOver / TalkBack、WCAG 2.2 AA、44 / 48pt 触控目标、减少动态效果、高对比度、非颜色状态提示。

## 缓存策略

触及 Provider 请求、system prompt、tool schema 或 compaction 的 PR 必须包含：

```
Cache-impact: ...
Cache-guard: ...
System-prompt-review: ...
```

本地 Runtime 在创建时冻结工具，动态状态只进 user-turn，保持 provider-visible prefix 稳定。

## 与现有面兼容

| 面 | 契约 |
| --- | --- |
| `reasonix serve` | HTTP + SSE 单会话 API 不变 |
| Desktop / CLI | 会话文件格式不变；新字段 `omitempty` |
| 移动信封 | 版本化；未知类型安全忽略或拒绝 |

## 里程碑（摘要）

1. 规格冻结、设计令牌、协议 Schema、`SessionBackend`、缓存基准。
2. Capacitor 壳、导航、会话 UI、安全存储、`mobilecore`、本地对话。
3. `reasonix node`、多会话 Hub、WebSocket、快照、幂等命令、局域网配对。
4. 账号扩展、Noise 中继、节点管理、APNs/FCM、离线重连。
5. 附件、轻量工具、HTTP MCP、审批、Todos、Diff、平板、三语。
6. 安全审计、故障注入、商店素材、Beta、公开发布。

## CI（当前）

面向 `main-v2` 的 PR / push 会跑 `.github/workflows/ci.yml` 中的 **`mobile`** job：

- Go：`internal/mobileprotocol`、`internal/mobilecore`、`internal/node`
- React 壳：`mobile/` 下 `npm ci`、typecheck、test、Vite 生产构建

本地镜像：`make mobile-ci`。原生 iOS/Android 打包、`gomobile bind`、商店发布尚未接入。

## 发布标签

- 稳定：`mobile-vX.Y.Z`
- RC：`mobile-vX.Y.Z-rc.N`
