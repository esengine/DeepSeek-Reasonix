---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-21
---

# File delivery and preview / 文件交付与预览

`present_files` declares one to four existing files. The host validates every
file before returning a versioned result; failure publishes no delivery cards.
The declaration identifies deliverables, not verified completion.

`present_files` 每次声明一到四个现有文件。宿主全部校验成功后才产生交付卡片；
声明交付文件不等于验证内容正确，也不替代任务完成判定。

## Settings / 设置

In Studio, **Settings → Tools & permissions → AI file delivery cards** is the
second item, below Tool approval. It saves the user configuration without
rebuilding the running session; reopen the session to apply the change.

在 Studio 的 **设置 → 工具与权限 → AI 文件交付卡片** 中修改开关。
它是「工具批准」下方的第二项，只保存用户配置，不会重建当前运行中的会话；
重新打开会话后生效。

## Reading / 阅读

The model-facing tool is **on by default**. Existing explicit choices are preserved.
Set `tools.present_files = false` in the user configuration
(`~/.reasonix/config.toml`, or `config.toml` under `REASONIX_HOME`) to disable it,
then reopen the session.

A project cannot enable or disable it. `tools.disabled` and an explicit
`tools.enabled` allowlist still apply.

While off, it is absent from
both the tool registry and provider requests; file previews and existing cards
remain available.

Enabling it adds its fixed schema to the next session's tool
prefix, which may cause a first-request cache miss, never mid-session churn.

模型工具**默认开启**，保留已有的显式选择。在用户配置（`~/.reasonix/config.toml`，或
`REASONIX_HOME` 下的 `config.toml`）中设置 `tools.present_files = false` 可关闭，
重新打开会话后生效；项目配置不能修改此选择。
`tools.disabled` 和显式的 `tools.enabled` 白名单仍然有效。

关闭时工具不会进入注册表或模型请求；文件预览、消息文件链接和历史交付卡片仍然可用。

启用会改变下一会话的工具前缀，
可能使首个请求缓存未命中，但不会在会话运行中改变工具定义。

```toml
[tools]
present_files = false
```

| Format / 格式 | View / 展示 | Limit / 限额 |
| --- | --- | --- |
| Markdown, MDX | Rendered Markdown; JSX is not executed / Markdown 正文，不执行 JSX | 2 MiB |
| Text, code / 文本、代码 | Existing source editor / 原有源码编辑器 | 2 MiB |
| CSV, TSV | Worker parsing, virtual rows and columns / Worker 解析、虚拟表格 | 2 MiB, 100,000 cells / 单元格 |
| HTML | Sanitized, sandboxed static iframe / 清理后的静态沙箱预览 | 2 MiB |
| PDF | Local PDF.js worker, page controls, zoom, selectable text / 本地阅读器、分页、缩放、文字选择 | 32 MiB |
| Images / 图片 | Image element with zoom / 图片缩放 | 10 MiB |
| Office, archives, binaries / Office、压缩包、二进制 | Information and available actions / 文件信息与可用操作 | No inline preview / 无内联预览 |

The workspace tree, message links and cards share workbench tabs. Full references
include the authorized root and relative path; display names do not identify files.
Open records read the source file's current content, without version snapshots.

文件树、消息链接和卡片复用工作台标签。文件身份包含授权根目录和相对路径，
不能以显示名称去重。历史交付记录读取源文件当前内容，不保存版本快照。

Cards use two columns at a container width of 720 CSS pixels, otherwise one.
The More menu contains copy, attachment and supported native actions. The current
marker follows the actual visible workbench file. Type icons do not imply support.

卡片容器达到 720 CSS 像素时显示两列，否则显示一列。“更多”菜单提供复制路径、
添加到对话及宿主支持的原生操作。“查看中”跟随实际可见文件；图标不代表预览能力。

## Boundaries / 边界

- Controller preview APIs use the shared bounded, root-confined file service.
  内容采用原始字节读取，不通过 JSON/base64 传输 PDF。
- Root confinement remains enforced on the opened descriptor. Missing, denied,
  non-regular, oversized and non-text files have typed errors.
  文件缺失、拒绝访问、非普通文件、超限和非文本均有独立错误码。
- HTML scripts, submission, redirects and external resources are blocked.
  本地桌面可显式选择已有的“交互运行”；远程页面不能调用本机系统打开。
- Only host-confirmed successful presenter results project to cards. Live and
  restored projections use the original tool history, including full raw results.
  普通工具输出相同 JSON 不会产生交付卡片，失败和拒绝调用也不会产生成功卡片。
- Visible binary previews inspect metadata every three seconds and reload bytes
  only on change. Obsolete requests are aborted or discarded.
  可见二进制预览约每三秒检查元数据，变化后才读取正文；旧请求会取消或丢弃。
- Saved text disappears after a typed missing/denied response. Dirty drafts stay
  intact and show the observation failure; saving still checks revisions.
  文件删除或撤权后清除旧的已保存正文，未保存草稿仍保留，并继续使用保存冲突检查。

New `deliveries` and `observedVersion` fields are optional. Old histories remain
readable; older clients keep the ordinary tool result. Tool schemas stay stable
within a run, while adding the capability changes the upgrade-time catalog.

新增字段均可缺省；旧会话仍可读取，旧客户端仍显示普通工具结果。
运行期间工具定义不变；升级时新增能力会改变工具目录，可能造成一次缓存失效。
