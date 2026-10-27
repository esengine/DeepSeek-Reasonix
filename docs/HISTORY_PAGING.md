---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-24
---

# Transcript paging / 正文分页

Paging limits frontend projection and transfer. The model still receives the
full authoritative conversation; stored formats, write leases and provider
prefixes are unchanged. In-memory page indexes are disposable derived data.

分页限制前端正文转换与传输量。模型仍使用完整权威会话；存储格式、写租约及模型前缀不变。
内存分页索引是可重建的派生数据。

| Contract / 协议 | Behavior / 行为 |
| --- | --- |
| `GET /history/page` | Latest 200 raw messages by default; `limit` accepts 1–500. Returns absolute `start/end/total`, `snapshot`, `nextCursor`, and cross-page tool context. 默认最近 200 条原始消息，范围使用全局下标，附带快照、游标及跨页工具上下文。 |
| `cursor` | Bound to a session generation and immutable prefix length; append preserves it, rewrite/rebind refuses with `history.snapshot_changed`. 追加保留游标，重写或重新绑定使游标失效。 |
| ETag | Checks snapshot/range, display settings and selected attachment metadata before body projection. 正文转换前检查快照范围、显示设置和当前页附件元数据。 |
| `/history` | Existing full response retained; existing clients keep reading it. 保留全量响应，现有客户端继续使用它。 |

The controller exposes optional paging reads without enlarging `SessionAPI`.
CLI and ACP retain their existing full-history behavior. The desktop wire mirrors
are declared in `tools/repolint/wireparity.go`; `frontend-parity` continues to
check capabilities carried by each frontend's declared port.

控制器通过可选读取能力提供分页，不扩充 `SessionAPI`。CLI 与 ACP 保留既有完整历史行为。
桌面镜像协议声明位于 `tools/repolint/wireparity.go`；`frontend-parity` 继续检查各前端端口
声明的能力。

This API can ship before the incremental UI. The wire types and error translations
are additive declarations; no desktop caller opts into paging in this change.
The UI is delivered separately and must fall back to `/history` only when paging
is explicitly unsupported. Removing the UI does not remove this endpoint.

接口可先于渐进加载界面独立发布。镜像类型与错误翻译仅新增声明，本改动不让桌面调用方
切换到分页。界面单独交付，仅在明确不支持分页时回退到 `/history`；撤回界面不移除接口。
