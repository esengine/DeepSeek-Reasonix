# Harness chat presentation / Harness 聊天展示移植

Source: local DeepSeek Harness checkout, commit
`c291e7961a515f6d7af9304e7fd1d257929aef26`. Upstream MIT terms are retained in
`LICENSE`. This directory contains adapted source, not a runtime dependency on
the other checkout.

来源为本机 DeepSeek Harness 上述提交。本目录保留 MIT 许可证，组件源码随 Reasonix
构建，不依赖用户电脑上的 Harness 目录。

| Source component | Reasonix integration / 接入方式 |
| --- | --- |
| ui-primitives DisclosureRow | Original compact disclosure layout, hover chevron and keyboard toggle / 原紧凑行、悬停箭头、键盘展开 |
| ui-chat ReasoningRow | First/latest nonempty line, no duplicated expanded heading / 首行与最新行摘要，展开不重复标题 |
| ui-chat TurnProcessNodeView | Turn counts, disclosure and separator; ChatSource owns boundaries / 轮次计数与折叠，适配已有轮次数据 |
| ui-chat TurnNavigator | Right-side turn rail, hover/focus preview and active tick; existing scroll controller / 右侧轮次刻度、预览与高亮，对接现有跳转 |
| ui-chat ContextInjectionRow | Compact system/permission record, lazy body / 系统与权限记录收起为摘要 |
| ui-tool ToolRow | Status icon, description, inline body and inspection action / 工具状态、描述、原位展开与详情入口 |
| ui-primitives TerminalBlock, StateDot, Pill, FoldToggle, ANSI helpers | Original command/output card, states, line cap and copy / 终端输出、状态、行数折叠与复制 |
| ui-primitives DiffBlock | Original added/deleted lines and totals, adapted tool arguments / 文件差异行与统计 |
| ui-primitives WebBlock | Source list with Reasonix safe-link host and Markdown / 搜索来源列表，沿用安全链接与 Markdown 宿主 |
| ChatView, MessageItem and MarkdownText styles | Scoped flow rhythm, bubbles, typography, headings and tables / 独立作用域下的间距、气泡、标题与表格 |

CSS module class names are expanded to stable `dsh-*` names. Design tokens map
to Reasonix theme and conversation typography. Expanded tool styles and bodies
load together; they do not increase render-blocking CSS beyond the existing
budget. Localized labels use Reasonix's locale provider. Clipboard, safe links,
history/full-content access, fork commands, Markdown workers and lifecycle stay
with the existing Reasonix owners. Unsupported tool payloads use the generic
IN/OUT card rather than guessing a Harness typed result.

CSS 类名使用 `dsh-*` 前缀，设计变量接入 Reasonix 主题与会话字体。工具详情及其样式
按需加载。剪贴板、安全链接、历史与全文 API、分支命令、Markdown Worker 及会话生命
周期沿用 Reasonix。无法可靠映射的工具结果使用通用输入／输出卡片。

Recovered tool errors do not prevent folding a completed turn with a final
answer; their count remains in the collapsed summary. Interrupted turns and
terminal failures remain visible. Manual disclosure preferences survive a
session revisit in memory. The standard/deep chat presentation setting is
removed from the settings UI and search; its persisted field is retained for
compatibility. The default approval control is unchanged.

工具失败后重试成功，不再阻止已完成且有最终回答的轮次折叠；摘要保留失败次数。
中断与终止故障仍保持可见。手动展开状态在本次应用运行中保留。“标准／深入”设置
入口及搜索接线已删除，旧持久化字段保留兼容；新会话默认审批设置不变。

This is a presentation port. It does not import Cordis, replace the backend or
alter provider request bytes. The existing composer and interactive approval
hosts remain outside the migrated component tree. Like/dislike is omitted.

此次移植限于聊天展示层，不导入 Cordis、不替换后端、不修改模型请求字节。输入框和
审批等交互继续使用现有宿主，不增加点赞或点踩按钮。
