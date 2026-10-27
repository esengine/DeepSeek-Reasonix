---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-24
---

# Incremental transcript UI / 正文渐进分页界面

With a paging-capable server, opening a conversation reads the latest 200 raw
messages. Load earlier messages, scrolling upward near the beginning, or searching
requests older pages. Search reports incomplete coverage until older history has
loaded; regex and case-sensitive search remain available.

后端支持分页时，打开会话先读取最近 200 条原始消息。点击“加载较早消息”、向上滚动至
已加载内容起点附近或搜索时读取更早的分页。历史尚未读完时，搜索明确显示覆盖不完整；
正则与大小写搜索仍然可用。

Prepending pages preserves absolute message positions, the reader's visible
anchor and mounted extension controls. New reader intent cancels pending anchor
restoration. Live messages and queued interactions stay owned by the session.
Responses from a previous binding are discarded; invalidated snapshots reload.

前插分页保留全局消息位置、可见阅读锚点和已挂载扩展控件。新的阅读操作取消待执行的
锚点恢复。实时消息和排队交互仍由会话持有；旧绑定的响应被丢弃，失效快照会重新加载。

The separate history API PR is the functional dependency for bounded reads.
This UI also works with an older server: a missing endpoint or the explicit
`history.pagination_unsupported` refusal selects full `/history` for that binding.

Authentication, missing-runtime and server failures remain errors, never a silent
full-history fallback. A port without the optional method uses full history.

Rolling back this UI leaves the additive backend API usable by other clients.

独立的历史接口 PR 是有界读取的功能依赖。界面也兼容旧服务端：接口缺失或明确返回
`history.pagination_unsupported` 时，该绑定使用全量 `/history`。鉴权、运行时丢失和
服务端故障仍作为错误处理，不会静默退回全量读取。不提供可选分页方法的端口使用全量
历史。撤回界面后，新增后端接口仍可供其他客户端使用。
