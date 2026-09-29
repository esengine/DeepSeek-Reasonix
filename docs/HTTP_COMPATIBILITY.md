---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-09-29
---

# HTTP connection compatibility / HTTP 连接兼容

Studio can force a configured Chat Completions, Anthropic Messages, or Responses
connection to use HTTP/1.1 when its server or network path has HTTP/2 problems.
Automatic negotiation remains the default. This is a compatibility workaround;
an HTTP/2 protocol error alone does not identify which network component failed.

Studio 支持为 Chat Completions、Anthropic Messages、Responses 连接选择
HTTP/1.1，以绕过服务端或网络链路的 HTTP/2 兼容问题。默认仍为自动协商。
这是兼容措施；仅凭 HTTP/2 协议错误无法判断是哪一段网络组件出了问题。

## Change one connection / 修改单个连接

In model sources, add a connection or edit the affected connection. Expand
**Advanced connection options** when adding, or **Connection compatibility and
reasoning** when editing. Select **HTTP/1.1** under **HTTP connection protocol**,
then save.

Model discovery and explicit model checks use the current
draft choice without changing the saved configuration.

Saving rebuilds an idle
connection through the existing settings flow; if work is still running, follow
the saved-settings notice and reload the connection after that work ends.

在模型来源中新增连接或编辑有问题的连接。新增时展开 **高级连接选项**，
编辑时展开 **连接兼容与推理设置**，在 **HTTP 连接协议** 中选择
**HTTP/1.1** 后保存。刷新模型目录和主动验证模型使用
当前草稿的协议选项，但不会保存草稿。保存通过现有设置流程重建空闲连接；
如果仍有工作在运行，按保存提示在工作结束后重新加载连接。

Saving does **not** resend a failed conversation request. Review any partial
output and decide whether to send another request yourself. Selecting
**Automatic negotiation (default)** restores normal negotiation.

保存**不会自动重发**失败的对话请求。请检查已有的部分输出，再自行决定是否
重新发送。选择 **自动协商（默认）** 即可恢复正常协商。

## Configuration and compatibility / 配置与兼容性

The same setting is available as `http1_only = true` inside the affected
`[[providers]]` entry in `config.toml`. Leave it absent or false for automatic
negotiation. Only true is written by the renderer.

Older HTTP API clients that
omit `http1Only` when editing preserve the saved choice; explicit false clears it.

Older application versions may ignore or remove this new setting when saving,
so check it again after a downgrade. Proxy settings and TLS certificate
validation are unchanged; the provider request body is unchanged.

也可以在 `config.toml` 对应的 `[[providers]]` 条目中设置
`http1_only = true`。省略或设为 false 表示自动协商，配置渲染器只写入 true。
旧 HTTP API 客户端编辑时省略 `http1Only` 会保留原值，显式 false 会清除它。

旧版应用可能忽略该字段或在保存时删除它，因此降级后请重新检查。代理设置、
TLS 证书验证和模型请求正文保持原有行为。

## Verify in the affected network / 在问题网络中验证

Compare a small new request using automatic negotiation and HTTP/1.1 with the
same endpoint, model, proxy and credentials. If HTTP/1.1 succeeds, retain it for
that connection.

If both fail, collect sanitized diagnostics and investigate the
endpoint or proxy separately. Passing local TLS fixtures does not prove that a
particular user's network is repaired.

在相同地址、模型、代理和凭据下，分别用自动协商与 HTTP/1.1 发送一条简短新
请求。如果 HTTP/1.1 成功，可以为此连接保留该设置。如果两者都失败，请收集
脱敏诊断并另行检查端点或代理。本地 TLS 回归通过不能证明某个用户的实际
网络问题已经解决。
