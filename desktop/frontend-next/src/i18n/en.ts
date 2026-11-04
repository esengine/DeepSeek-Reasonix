import { EN_SURFACES } from "./en_surfaces";
import { EN_COMPOSITION } from "./en_composition";
import { EN_DOCUMENT } from "./en_document";
// English catalogue, keyed by the Chinese source text. A key that is missing
// here renders as Chinese rather than as a blank, so an incomplete translation
// degrades instead of breaking — and i18n.test.ts is what stops it from
// staying incomplete quietly.
//
// Grouped the way the interface is, not alphabetically: a translator reads a
// screen at a time, and a term is only right relative to the ones beside it.
import { EN_KERNEL } from "./en_kernel";
import { EN_REMOTE } from "./en_remote";
import { EN_WINDOW } from "./en_window";
import { EN_GRAPH } from "./en_graph";
import { EN_TRAJECTORY } from "./en_trajectory";
import { EN_SETTINGS } from "./en_settings";
import { EN_TOOL_BLURBS } from "./en_tool_blurbs";
import { EN_PERSONA } from "./en_persona";
import { EN_METRICS } from "./en_metrics";
import { EN_FIND } from "./en_find";
import { EN_STORAGE } from "./en_storage";
import { EN_USAGE } from "./en_usage";
import { EN_COMPOSER } from "./en_composer";
import { EN_BACKUP } from "./en_backup";
import { EN_FEEDBACK } from "./en_feedback";
import { EN_COMMUNITY } from "./en_community";
import { EN_SPONSOR } from "./en_sponsor";
import { EN_MEMORY } from "./en_memory";

export const EN: Record<string, string> = {
  "鉴权失败": "Auth failed",
  "缺少密钥": "Missing key",
  "需要付费": "Payment required",
  "配额耗尽": "Quota exhausted",
  "暂时限流": "Rate limited",
  "连接超时": "Timed out",
  "连接异常": "Connection failed",
  "跳转被拒": "Redirect refused",
  "连接正常，模型调用未验证": "Connection reachable; model calls unverified",
  "连接鉴权失败，请检查密钥或权限": "Authentication failed; check key or permissions",
  "未配置 API 密钥": "API key not configured",
  "服务商要求付费，请检查账户": "Payment required; check your account",
  "可用配额已耗尽": "Available quota exhausted",
  "请求暂时被限流，请稍后重试": "Temporarily rate limited; retry later",
  "连接检测超时，请稍后重试": "Connection check timed out; retry later",
  "连接异常，请检查网络或服务商": "Connection failed; check network or provider",
  "连接跳转被拒绝，请检查地址": "Redirect refused; check the address",
  "余额不足，请充值后重试": "Insufficient balance; top up and retry",
  "余额查询鉴权失败": "Balance query authentication failed",
  "余额未知（未配置查询接口）": "Balance unknown (no query endpoint configured)",
  "余额暂时无法确认": "Balance could not be confirmed",
	...EN_COMPOSITION,
  ...EN_DOCUMENT,
  ...EN_SURFACES,
  "自动命名": "Auto name",

  "工作台": "Workbench",
  ...EN_SETTINGS,
  ...EN_TOOL_BLURBS,
  ...EN_PERSONA,
  ...EN_COMMUNITY,
  ...EN_SPONSOR,
  ...EN_MEMORY,
  ...EN_METRICS,
  ...EN_FIND,
  ...EN_STORAGE,
  ...EN_USAGE,
  ...EN_COMPOSER,
  ...EN_BACKUP,
  ...EN_FEEDBACK,
  ...EN_KERNEL,
  ...EN_REMOTE,
  ...EN_WINDOW,
  ...EN_GRAPH,
  ...EN_TRAJECTORY,
  // ── 转录：卡片与流 ───────────────────────────────────────────────
  "Agent": "Agent",
  "思考中…": "Thinking…",
  "推送": "Push",
  "无法读取这个仓库的提交历史": "Could not read this repository's commit history",
  "这不是一个提交编号": "That is not a commit id",
  "这个仓库里没有这个提交": "This repository has no such commit",
  "无法读取这个提交": "Could not read this commit",
  "丢弃请求的格式不对": "The discard request is malformed",
  "对话正在运行，它可能正在写这个文件，稍后再丢弃": "A turn is running and may be writing this file; discard it later",
  "这个文件没有未提交的改动": "This file has no uncommitted change",
  "只有上次提交里已有的文件，才能丢弃它的改动": "Only a change to a file the last commit already holds can be discarded",
  "丢弃失败了": "The discard failed",
  "推送请求的格式不对": "The push request is malformed",
  "这个工作区还没有被你信任，不能从这里推送": "You have not trusted this workspace, so nothing can be pushed from it",
  "现在不在任何分支上，没有可以推送的分支": "HEAD is not on a branch, so there is no branch to push",
  "这个分支还没有对应的远端分支，不能推送": "This branch has no upstream to push to",
  "远端已经有这个分支的全部提交，没有可推送的内容": "The upstream already has every commit; there is nothing to push",
  "这个远端用的传输方式不允许推送，只支持 https、ssh 和 git://": "This remote's transport is not allowed for a push; only https, ssh and git:// are supported",
  "确认之后仓库又变了，请重新预览再推送": "The repository changed after you confirmed; preview again and then push",
  "远端有这个分支没有的提交，推送被拒绝（不会强制推送）": "The remote has commits this branch does not, so the push was rejected (it is never forced)",
  "远端需要认证，请先在 git 里配置凭据": "The remote needs authentication; set up a credential in git first",
  "推送失败了": "The push failed",
  "推送时 git 出错了": "git failed during the push",
  "这个仓库不属于当前工作区": "That repository is not part of this workspace",
  "无法列出这个工作区里的仓库": "Could not list the repositories in this workspace",
  "这一步没有回报结果就随本轮结束了": "This step ended with the turn without reporting a result",

  // ── 运行状态 ─────────────────────────────────────────────────────
  "空闲": "Idle",
  "运行中": "Running",
  "联网": "Network",
  "子任务": "Subtask",
  "{n} tokens": "{n} tokens",
  "思考中": "Thinking",
  "正在回答": "Answering",
  "已完成": "Done",
  "已中断": "Interrupted",
  "请求未能送达内核（HTTP {status}）": "That request never reached the kernel (HTTP {status})",
  "等待批准": "Waiting for approval",
  "等待工作区": "Waiting for the workspace",
  "等待模型回复": "Waiting for the model",

  // ── 面板与栏 ─────────────────────────────────────────────────────
  "沙盒": "Sandbox",
  "每小时": "Every hour",
  "每 2 小时": "Every 2 hours",
  "每 6 小时": "Every 6 hours",
  "每 12 小时": "Every 12 hours",
  "每天": "Every day",
  "每周": "Every week",
  "已到期": "Expired",
  "已熔断": "Stopped by the safety breaker",
  "你暂停了它": "You paused it",
  "连续失败后自动暂停": "Paused automatically after repeated failures",
  "额度保护自动暂停": "Paused automatically to protect the budget",
  "用完了总额度": "The lifetime budget is used up",
  "记录损坏": "Its record is damaged",
  "等你决定": "Waiting for you",
  "额度用尽": "Budget used up",
  "被中断": "Interrupted",
  "已跳过": "Skipped",
  "即将开始": "About to start",
  "错过了时间": "Missed its time",
  "额度窗口已满": "The budget window is full",
  "有任务在运行": "Another run was in progress",
  "定时运行": "Scheduled runs",
  "暂停": "Pause",
  "恢复": "Resume",
  "追踪导出": "Trace export",
  "这个服务器没有要求更多权限": "This server has not asked for more access",
  "这个客户端不能修改追踪导出设置": "This client cannot change the trace export settings",
  "收集器地址必须是带主机名的 http 或 https 地址": "The collector address must be an http or https address with a host name",
  "收集器地址里不能带用户名、密码、查询串或片段": "The collector address must not contain a user name, password, query or fragment",
  "这不是可用的请求头名称": "That is not a usable header name",
  "凭据环境变量必须以 OTEL_ 或 REASONIX_OTEL_ 开头": "The credential variable must start with OTEL_ or REASONIX_OTEL_",
  "追踪导出设置没有保存成功": "The trace export settings were not saved",
  "这个客户端不能修改定时运行": "This client cannot change scheduled runs",
  "定时任务只能在你确认预览后创建": "A schedule is created only after you confirm its preview",
  "要创建的内容和你确认的预览不一致": "What is being created is not what you confirmed",
  "找不到这个定时任务或结果": "No such schedule or result",
  "间隔低于允许的最小值": "The interval is under the minimum",
  "定时任务数量已达上限": "The schedule limit is reached",
  "请只设置“每隔多久”或“某个时间”其中一个": "Set either an interval or a time, not both and not neither",
  "这个文件夹不存在": "That folder does not exist",
  "定时运行只能读已受信任的项目文件夹": "A scheduled run can only read a project folder you have trusted",
  "这个模型还没有配置": "That model is not configured",
  "这个定时任务的内容不合法": "The schedule is not valid",
  "定时任务由更新的版本写入，这里只读": "The schedules were written by a newer version and are read-only here",
  "这个定时任务当前不可用（已暂停或已过期）": "The schedule is not active (paused or expired)",
  "定时任务没有保存成功": "The schedule was not saved",
  "取消中…": "Stopping…",
  "联网：搜索与抓取": "Web: search and fetch",
  "SearXNG": "SearXNG",
  "DeepSeek 官方搜索": "DeepSeek search",
  "本机抓取": "Local fetch",
  "DeepSeek 官方抓取": "DeepSeek fetch",
  "你自己部署的搜索服务，免费，需要填它的地址。": "A search service you run yourself; free, and it needs its address.",
  "DeepSeek 官方的搜索接口，按次计费。": "DeepSeek's own search API; billed per call.",
  "由本机直接下载网页，免费。": "This machine downloads the page directly; free.",
  "DeepSeek 官方的网页抓取接口，按次计费。": "DeepSeek's own page-fetch API; billed per call.",
  "已阻塞": "Blocked",
  "已停止": "Stopped",
  "达到花费上限": "the spend limit was reached",
  "目标没有进展": "the goal was not making progress",
  "无法评估目标是否完成": "the goal could not be evaluated",
  "你手动停止": "you stopped it",
  "在等你回答": "waiting for your answer",
  "没能测试这个地址：{detail}": "Could not test this address: {detail}",
  "这个 SearXNG 没有开启 JSON 格式：在它的 settings.yml 的 search.formats 里加上 json，然后重启它。": "This SearXNG does not allow the JSON format: add json to search.formats in its settings.yml, then restart it.",
  "连不上这个地址：检查它是否在运行，地址和端口是否写对。": "Cannot reach this address: check that it is running and that the address and port are right.",
  "这个地址有应答，但不是 SearXNG 的 JSON 搜索结果：确认地址指向 SearXNG 本身，而不是登录页或别的服务。": "This address answers, but not with SearXNG's JSON results: make sure it points at SearXNG itself, not a login page or another service.",
  "这个地址把请求转到了别处：请直接填 SearXNG 的最终地址。": "This address redirects elsewhere: enter SearXNG's final address directly.",
  "测试不会连接这个地址：它是链路本地、云元数据或未指定地址。自托管实例请用本机或局域网地址。": "The test will not connect to this address: it is link-local, a cloud metadata endpoint or unspecified. A self-hosted instance belongs on this machine or your local network.",
  "撤销": "Revoke",
  "尚无改动": "No changes yet",
  "系统提示": "System prompt",
  "工具定义": "Tool definitions",
  "模型回复": "Model replies",
  "工具输出": "Tool output",
  "自动批准": "Auto approve",
  "这条命令里嵌套或间接地执行了别的命令。自动放行和宽泛的允许规则都无法确认真正会跑的是什么；要么批准这一条，要么切到全部放行。":
    "This command runs another command through nesting or indirection. Auto and broad allow rules cannot tell what would actually run; approve this one, or switch to full access.",
  "这一步会把密码、一次性验证码或银行卡信息输入网站。自动放行、该站点已有的授权和宽泛规则都不能代你回答；要么批准，要么切到全部放行。":
    "This step types a password, a one-time code or card details into the site. Auto, the grant this site already has and broad rules do not answer for you; approve it, or switch to full access.",
  "这会操作电脑上的另一个应用，拿到的是那个应用本身的权限。Windows 只把按键送给最前面的窗口，所以输入文字、按键和粘贴会先把这个应用切到前台。自动放行和宽泛规则都不能代你回答；要么为这个应用批准，要么切到全部放行。":
    "This operates another application on the computer, with that application's own access. Windows sends keys only to the window in front, so typing, key presses and pastes bring this application forward first. Auto and broad rules do not answer for you; approve it for this application, or switch to full access.",
  "这会读取或操作电脑上的另一个应用，拿到的是那个应用本身的权限。自动放行和宽泛规则都不能代你回答；要么为这个应用批准，要么切到全部放行。":
    "This reads or operates another application on the computer, with that application's own access. Auto and broad rules do not answer for you; approve it for this application, or switch to full access.",
  "这一步会接管你正在用的鼠标指针——移动它、用它点击，并把那个应用切到前台，而不是让界面元素自己动作。自动放行、该应用已有的授权和宽泛规则都不能代你回答；要么批准，要么切到全部放行。用完指针会回到原处。":
    "This step takes the pointer you are using — it moves your cursor, clicks with it and brings that application forward — rather than letting an element act for itself. Auto, the grant this application already has and broad rules do not answer for you; approve it, or switch to full access. The pointer goes back where it was.",
  "输出": "Output",
  "价目未上报": "No pricing reported",
  "尚未产生用量": "No usage yet",
  "多币种未合并": "Currencies not combined",
  "显示币种不可用": "Display currency unavailable",
  "主循环": "Main loop",
  "压缩": "Compaction",
  "规划": "Planning",
  "分类": "Classifying",
  "标题": "Titling",
  "计划": "Plan",
  "停止": "Stop",
  "外部服务": "External services",
  "失败": "Failed",
  "OpenAI 兼容": "OpenAI-compatible",
  "Anthropic 兼容": "Anthropic-compatible",
  "Gemini 原生（实验）": "Gemini native (experimental)",
  "扩展": "Extension",
  "新增": "Added",
  "已删除": "Deleted",
  "重命名": "Renamed",

  // ── 会话树 ───────────────────────────────────────────────────────
  "新会话": "New session",
  "此内核不支持添加工作区。": "This kernel does not support adding workspaces.",
  "隔离": "Isolated",
  "删除": "Delete",
  "展开": "Expand",
  "收起": "Collapse",

  // ── 待发队列 ─────────────────────────────────────────────────────
  "已暂停": "Held",
  "排队": "Queued",
  "进行中": "running",
  "这条插话在本轮结束前没来得及送达，没有发给模型": "This guidance was not delivered before the turn ended, so the model never saw it",
  "本轮已结束，但对话记录没能保存": "The turn ended, but the transcript could not be saved",
  "本轮已结束，但队列没能确认这一条": "The turn ended, but the queue could not acknowledge this item",
  "运行这一条的会话已不在了": "The session that was running this item is gone",
  "队列记录损坏，这一条是从残留文件里找回的": "The queue record was corrupt; this item was salvaged from leftover files",
  "这一条带着 1.x 排队的图片，这个版本读不了，所以先停住，没有去掉图片照发": "This entry was queued with images by Reasonix 1.x that this build cannot read, so it was held instead of being sent without them",

  // ── 外观设置 ─────────────────────────────────────────────────────
  "外观": "Appearance",
  "大小": "Size",
  "紧凑": "Compact",
  "标准": "Standard",
  "宽松": "Comfortable",
  "更大": "Larger",
  "小": "Small",
  "大": "Large",
  "字体": "Typeface",
  "等宽": "Monospace",
  "壁纸": "Wallpaper",
  "语言": "Language",
  "跟随系统": "Follow the system",
  "中文": "中文",
  "英文": "English",

  // ── 高级外观 ─────────────────────────────────────────────────────
  "高级外观": "Advanced appearance",

  // ── 明暗与配色 ───────────────────────────────────────────────────
  "明暗": "Light and dark",
  "浅色": "Light",
  "深色": "Dark",
  "配色": "Palette",
  "文字粗细": "Text weight",
  "视觉效果": "Visual effects",
  "节能": "Reduced",
  "背景模糊、动态天空与颗粒质感全部开启": "Background blur, the animated sky and the grain layer are all on",
  "关闭背景模糊、动态天空与颗粒质感，集成显卡或老旧电脑上更省电": "Turns off background blur, the animated sky and the grain layer; lighter on integrated graphics and older machines",
  "图形渲染": "Graphics",
  "软件渲染": "Software rendering",
  "跟随语言": "Match the language",
  "中文界面使用中等字重，西文使用常规字重": "Medium for Chinese, regular for Latin",
  "常规": "Regular",
  "笔画更细，字距更宽松": "Thinner strokes, looser texture",
  "加粗": "Bolder",
  "小字号下更扎实，屏幕距离较远或有反光时更易读": "Firmer at small sizes, and in glare",
  "文字对比度": "Text contrast",
  "柔和": "Soft",
  "更强": "Stronger",
  "内置": "Built in",
  "定时": "Scheduled",
  "深色开始时间": "Dark mode starts",
  "恢复浅色时间": "Light mode resumes",

  // ── 连接与来源 ───────────────────────────────────────────────────
  "正在读取…": "Loading…",
  "模型": "Models",
  "数据": "Data",
  "界面语言": "Interface language",
  "备份": "Backup",
  "批准": "Approval",
  "对话显示": "Conversation display",
  "操作与性能": "Controls and performance",
  "对话里显示什么、怎么折叠": "What the conversation shows and how it folds",
  "快捷键、标签、运行中发消息、性能": "Shortcuts, tabs, sending while running, performance",
  "权限与安全": "Permissions and safety",
  "最常改的几项放这里，打开设置先看到它们": "The few settings changed most often, first in view when settings opens",
  "接哪个服务、每种用途用哪个模型、上下文怎么维护": "Which service to use, which model for each purpose, how context is kept",
  "它怎么想、怎么问你、怎么协作": "How it thinks, how it asks you, how it works with others",
  "它能碰什么：批准、规则、沙箱、联网": "What it can touch: approval, rules, sandbox, network",
  "技能、外部工具、插件、自动化": "Skills, external tools, plugins, automation",
  "明暗、配色、字体、壁纸": "Light or dark, colours, fonts, wallpaper",
  "记忆、用量、存储、备份": "Memory, usage, storage, backup",
  "网络、远程机器、手机": "Network, remote machines, phone",
  "账号、版本、日志、社区": "Account, version, logs, community",
  "常用": "Common",
  "账号与关于": "Account and About",
  "Agent 行为": "Agent behaviour",
  "验证": "Verify",
  "已配置": "Configured",
  "现场探测": "Endpoint catalog",
  "用户添加": "Added manually",
  "本次未返回": "Not returned this time",
  "已验证可用": "Verified available",
  "当前不可用": "Currently unavailable",
  "模型不存在或账号未开放": "The model does not exist or is not enabled for this account",
  "端点不接受这个模型": "The endpoint does not accept this model",
  "能对话但不接受工具调用，用不了": "Answers chat but refuses tool calls, so it cannot run here",
  "待验证": "Not verified",
  "凭据未通过，尚未确认": "Credentials were rejected; availability is still unknown",
  "遇到限流，尚未确认": "Rate limited; availability is still unknown",
  "网络异常，尚未确认": "Network error; availability is still unknown",
  "地址跳转到了另一个主机，已拒绝，尚未确认": "The address redirected to another host and was refused; availability is still unknown",
  "验证超时，尚未确认": "Verification timed out; availability is still unknown",
  "本机服务没有在时限内回答，尚未确认；模型若刚启动可能还在加载，稍后再验证一次": "The local service did not answer in time, so this is unconfirmed. A model that was just started may still be loading; verify again shortly.",
  "请求被拒绝，尚未确认": "Request rejected; availability is still unknown",
  "尚未确认": "Availability is still unknown",
  "推理": "Reasoning",
  "无法连接": "Unreachable",
  "忘记": "Forget",
  "定义有改动": "Definition changed",
  "无法核对": "Cannot be checked",
  // 收工回执
  "未验证": "not verified",
  "通知": "Notifications",
  "tokens ≈": "tokens ≈",
  "编辑": "Edit",
  "代理": "Proxy",
  "域名解析": "Name resolution",
  "建立连接": "Connection",
  "加密握手": "TLS handshake",
  "凭据": "Credentials",
  "已保存，尚未生效": "Saved, not applied yet",

  // ── 外部服务与插件 ───────────────────────────────────────────────
  "完成": "Done",
  "快捷键": "Keyboard shortcuts",
  "在对话里查找": "Find in the conversation",
  "搜索会话": "Search conversations",
  "在提问之间移动": "Move between your questions",
  "工作台全屏": "Workbench full screen",
  "缩小 / 放大 / 复原界面": "Make the interface smaller / larger / its default size",
  "发送（发送键设为修饰键 + Enter 时）": "Send when the send key is set to the modifier plus Enter",
  "发送，或用 Shift 换行": "Send, or a new line with Shift",
  "关闭当前弹层": "Close the open panel",
  "聚焦分隔条后调宽或用 Home 复位": "Widen a divider once focused, or reset it with Home",
  "更新": "Update",
  "常驻进程": "Long-running process",

  // ── 自动化 ───────────────────────────────────────────────────────

  // ── 网络 ─────────────────────────────────────────────────────────
  "协议": "Protocol",
  "服务器": "Server",

  // ── 命令解释器 ───────────────────────────────────────────────────

  // ── 卡片：批准、还原、收工 ───────────────────────────────────────
  "输出不完整，部分内容未进入上下文": "The output is incomplete; some content did not enter context",
  "读取了 {n} 个文件": "Read {n} files",
  "完成了 {n} 次搜索": "Completed {n} searches",
  "查阅了 {n} 项内容": "Inspected {n} items",
  "网页搜索": "Web search",
  "获取网页": "Fetch page",
  "委派任务": "Delegate task",
  "运行命令": "Run command",
  "命令输出": "Command output",
  "停止命令": "Stop command",
  "等待任务": "Wait",
  "读取文件": "Read file",
  "搜索代码": "Search code",
  "列出目录": "List directory",
  "编辑文件": "Edit file",
  "写入文件": "Write file",
  "编辑多个文件": "Edit files",
  "更新计划": "Update plan",
  "保存记忆": "Save memory",
  "调用 MCP": "Call MCP",
  "建立索引": "Build index",
  "完成步骤": "Complete step",
  "审阅结果": "Review results",
  "更新目标": "Update goal",
  "安全检查": "Safety check",
  "压缩上下文": "Compact context",
  "删除内容": "Delete content",
  "删除符号": "Delete symbol",
  "移动文件": "Move file",
  "编辑笔记本": "Edit notebook",
  "提交计划": "Submit plan",
  "询问用户": "Ask user",
  "并行任务": "Parallel tasks",
  "只读任务": "Read-only task",
  "读取子任务结果": "Read subtask result",
  "完成子任务": "Complete subtask",
  "代码诊断": "Code diagnostics",
  "读取 · {n} 行": "Read · {n} lines",
  "搜索": "Search",
  "{n} 处": "{n} matches",
  "{n} 项": "{n} items",
  "读取文件 ×{n}": "Read files ×{n}",
  "搜索命中 {n} 处": "Search · {n} matches",
  "查找文件 ×{n}": "Find files ×{n}",
  "列出目录 ×{n}": "List directories ×{n}",
  "{n} 项失败": "{n} failed",
  "推荐": "Recommended",
  "你": "You",
  "已撤回": "Withdrawn",
  "改动": "Changes",
  "代码和对话": "Code and conversation",
  "只还原代码": "Code only",
  "只回退对话": "Conversation only",
  "bash 命令所作的修改没有快照": "Changes made by bash commands have no snapshot",

  // ── 开场 ─────────────────────────────────────────────────────────

  // ── 版本 ─────────────────────────────────────────────────────────

  // ── 账号 ─────────────────────────────────────────────────────────

  // ── 分工与模型 ───────────────────────────────────────────────────

  // ── 记忆 ─────────────────────────────────────────────────────────
  "只能点名": "By name only",
  "只能模型自选": "Model's choice only",
  "调不到": "Unreachable",
  "文件修改后自动格式化": "Format files after they are edited",
  "每次写入后执行一次格式化命令；失败时仅提醒，不中断":
    "Runs a formatter after every write; a failure only warns and does not interrupt",
  "写入密钥文件前请求确认": "Ask me before writing a secrets file",
  "访问 .env 一类路径时拦截，由你决定是否放行":
    "Stops on paths like .env and leaves the call to you",
  "结束前执行一次测试": "Run the tests before wrapping up",
  "每轮结束时运行测试，失败时以提醒形式显示": "Runs the tests at the end of a turn; a red one shows up as a warning",

  // ── 其余 ─────────────────────────────────────────────────────────
  "连不上内核：/runtimes 没有回应。": "Cannot reach the kernel: /runtimes did not answer.",

  // ── 常量表：档位、分区、状态 ─────────────────────────────────────
  "每次执行操作前请求确认": "Asks before every action",
  "低风险操作自动放行，写入操作仍需确认": "Low-risk steps proceed; writes still ask",
  "不显示审批请求；需要批准的操作一律不执行": "No approval prompts; anything needing approval is not done",
  "不再请求确认，仅在完全信任当前工作区时使用": "Stops asking. Only for a workspace you fully trust",
  "会话": "Session",
  "工具与权限": "Tools and permissions",
  "高级": "Advanced",
  "项目": "Project",
  "我的": "Mine",
  "环境变量": "Environment variables",
  "手动": "Manual",
  "手动设置": "Set manually",
  "直连": "Direct",
  "连接中": "Connecting",
  "已关闭": "Disabled",
  "未连接": "Not connected",
  "待命 · 首次调用时启动": "Standby · starts on first call",
  "待授权 · 来自项目声明": "Awaiting approval · declared by the project",
  "这个项目": "This project",
  "导出": "Export",
  "仅生成计划，不写入": "Drafts a plan, read-only",
  "决策": "Decision",
  "system_one 询问的后端": "The backend system_one asks",
  "派发的子任务": "Work sent out",
  "看图": "Vision",
  "处理主模型无法识别的图片": "Reads images the main model cannot",
  "复核": "Review",
  "独立复核本轮": "Audits this turn independently",
  "支持 && 和 ||，但语法仍为 PowerShell，而非 bash":
    "Understands && and ||, but the syntax is still PowerShell, not bash",
  "不支持 && 和 ||，链式命令需拆分为两条": "Does not accept && or ||; chained commands must be split in two",
  "验证中…": "Verifying…",
  "使用系统或环境变量中已配置的代理": "Uses whatever proxy the system or the environment already sets",
  "{n} 个": "{n}",
  "{n} 个技能": "{n} skills",
  "{n} 个子代理": "{n} subagents",
  "{n} 个提示词": "{n} prompts",
  "{n} 套配色": "{n} palettes",
  "{n} 条钩子": "{n} hooks",
  "{n} 个服务": "{n} services",
  "{n} 个命令": "{n} commands",

  // ── 面板标签与关闭确认 ───────────────────────────────────────────

  // ── 版本 ─────────────────────────────────────────────────────────

  // ── 插件安装 ─────────────────────────────────────────────────────
  "安装": "Install",

  // ── 来源与自动化 ─────────────────────────────────────────────────
  "超时了，这一步会被当成失败": "Timed out — this step counts as a failure",

  // ── 卡片 ─────────────────────────────────────────────────────────
  "显示对话": "Show conversation",
  "显示运行分析": "Show run analysis",
  "显示工作台": "Show workbench",
  "交付验收卡片": "Delivery receipt card",

  // ── 通用动作 ─────────────────────────────────────────────────────
  "已保存": "Saved",
  "没有更改": "No changes",
  "有未保存的更改": "Unsaved changes",
  "关闭": "Close",
  "设置": "Settings",
  "主题": "Theme",

  // ── 作用范围：一处例外归谁管 ─────────────────────────────────────

  // ── 接入外部服务 ─────────────────────────────────────────────────

  // ── 首次连接 ─────────────────────────────────────────────────
  "硅基流动": "SiliconFlow",
  "月之暗面": "Moonshot",
  "智谱": "Zhipu",
  "中转站 / 自建": "Relay / self-hosted",
  "正在保存…": "Saving…",

  // ── 回合中的通知（notices.ts 按 code 查表，扫描器看不到）──────
  "这一轮模型没有给出回答，正在让它重说一次": "The model gave no answer this round; asking it again",
  "模型直接给了答案却没动手，正在要求它先用工具": "The model answered without acting; asking it to use its tools first",
  "分支保存的模型已不可用，本次改用其他已配置的模型；可以在模型设置中重新选择": "The model saved for this branch is unavailable, so another configured model is in use; choose a model again in settings",
  "同一个检查连着几轮都报同样的结果，是否继续由你定":
    "The same check has reported the same thing for several rounds; whether to keep at it is yours to call",
  "一个 MCP 服务有工具的定义是新增的或与你批准过的不同，已暂不提供给模型；详情里是这些工具，核对服务后再接受":
    "An MCP server has tools whose definitions are new or differ from what you approved; they are held out of the model's tools until you accept them (details list them)",
  "你选择了跟随的一个 MCP 服务改了或新增了工具定义，已按你的选择直接采用；详情里是这些工具":
    "An MCP server you follow changed or added tool definitions and they were taken as you chose; the details list them",

  // ── 宿主通知卡片自己的头部（NoticeCard）───────────────────────
  "提示": "Notice",

  // ── 连不上时卡在哪一步（kernel.ts 的 refusal 措辞）─────────────

  // ── 内核其余的拒绝措辞 ─────────────────────────────────────────
  "该供应商拒绝了当前密钥，无法读取余额": "The provider refused this key, so the balance cannot be read",
  "该供应商的余额接口无响应": "The provider's balance endpoint did not answer",
  "DeepSeek 格式": "DeepSeek format",
  "Moonshot 格式": "Moonshot format",
  "OpenRouter 格式": "OpenRouter format",
  "无法解析该供应商余额接口返回的内容": "The provider's balance endpoint answered in a shape this build cannot read",


  // ── 钱包 ───────────────────────────────────────────────────────
  "已停用": "Suspended",
  "置顶": "Pinned",
  "归档": "Archived",
  "连接失败": "Connection failed",
  "自动检测连接与余额": "Automatically check connections and balances",
  "执行过程": "Activity",
  "读取": "Read",
  "文件改动": "File changes",
  "折叠中间过程": "Fold the work in between",
  "中间过程": "Work in between",
  "快速": "Fast",
  "平衡": "Balanced",
  "深入": "Deep",
  "极致": "Maximum",
  "行宽": "Line width",
  "适宜阅读": "Reading",
  "铺满窗口": "Full width",
  "连输入框一起铺满": "Full width, composer too",
  "正文和输入框都跟随窗口宽度；输入框很宽时一行不易回到行首": "Prose and the composer both follow the window; a very wide composer makes a line harder to find the start of",
  "每行长度控制在一眼能回到行首的范围": "Keeps a line short enough that your eye finds the next one",
  "正文跟随窗口宽度，宽屏上不留两侧空白": "Prose follows the window, with nothing left empty at the sides",
  "自定义比例": "Custom percentage",
  "按比例调整对话和输入框占用的区域": "Set the conversation and composer width as a percentage",
  "对话区域宽度": "Conversation width",
  "预览": "Preview",
  "操作": "Action",
  "优化提示词":
    "Refine prompt",
  "优化失败，请重试":
    "Refining failed; try again",
  "输入框是空的，先写点内容再优化":
    "The box is empty; write something first",
  "内容超过 {max_bytes} 字节，无法优化":
    "The text is over {max_bytes} bytes and cannot be refined",
  "当前会话没有可用的模型，无法优化提示词":
    "This session has no model to refine the prompt with",
  "优化超时，请重试":
    "Refining timed out; try again",
  "模型没有给出结果，请重试":
    "The model returned nothing; try again",
  "优化请求格式不正确":
    "The refine request was malformed",
  "当前会话没有可用的模型，不能起草。请手动填写提交说明。": "This session has no model to draft with. Write the commit message yourself.",
  "起草超时了。请手动填写提交说明，或重新起草。": "Drafting timed out. Write the commit message yourself, or draft again.",
  "模型没有给出提交说明。请手动填写，或重新起草。": "The model gave no commit message. Write it yourself, or draft again.",
  "模型起草失败。请手动填写提交说明，或重新起草。": "The model failed to draft. Write the commit message yourself, or draft again.",
  "这个工作区不是 git 仓库，无法提交":
    "This workspace is not a git repository, so there is nothing to commit",
  "回合运行中，等它结束后再分支": "A turn is running; branch after it ends",
  "这个位置无法分支（重启后恢复的会话，或已被压缩的历史）": "This point cannot be branched (a session restored after a restart, or history that was compacted)",
  "分支失败，请重试": "Branching failed; try again",
  "暂存区是空的：先在改动列表里勾选要提交的文件，或点「暂存全部」":
    "Nothing is staged: tick the files to commit in the changes list, or press Stage all",
  "这个文件已经不在改动列表里了，请刷新后再试": "That file is no longer in the changes list; refresh and try again",
  "暂存请求无法识别": "The stage request was not understood",
  "暂存失败，请重试": "Staging failed; try again",
  "暂存区在你确认之后又变了，请重新起草":
    "The staged changes changed after you reviewed them; draft again",
  "暂存的内容里有疑似密钥的文件，需要你确认后才能提交":
    "The staged changes include likely secrets; confirm them before committing",
  "提交说明是空的":
    "The commit message is empty",
  "提交说明含有无法记录的字符":
    "The commit message contains characters git cannot record",
  "提交说明超过 {max_bytes} 字节":
    "The commit message is over {max_bytes} bytes",
  "git 没有配置提交者姓名和邮箱（user.name、user.email）":
    "git has no author name and email configured (user.name, user.email)",
  "git 没能记录这次提交":
    "git could not record the commit",
  "读取暂存区失败":
    "Reading the staged changes failed",
  "提交请求格式不正确":
    "The commit request was malformed",
  "另一个会话正在写入这个工作区，请稍后重试切换分支":
    "Another session is writing to this workspace; try switching branches again shortly",
  "后台任务仍在运行，请先停止再切换分支":
    "Background jobs are still running; stop them before switching branches",
  "任务运行中，分支要等这轮结束再切":
    "A turn is running; wait for it to finish before moving the branch",
  "这个工作区不是 git 仓库，没有分支可切换":
    "This workspace is not a git repository, so there is no branch to switch to",
  "切换分支的请求格式不正确":
    "The branch switch request was malformed",
  "这不是有效的分支名":
    "That is not a usable branch name",
  "没有这个名字的本地分支":
    "No local branch with that name",
  "有未提交的修改会被这次切换覆盖，请先提交或暂存（git stash）":
    "Uncommitted changes would be overwritten by the switch; commit or stash them first",
  "该分支已在另一个 worktree 中检出，请先在那边切走":
    "That branch is checked out in another worktree; switch away there first",
  "git 未能完成这次分支切换":
    "git could not complete the branch switch",
  "新增工具":
    "New tools",
  "agent 可以调用，每次调用照常经过权限确认":
    "The agent can call them; every call still goes through permission checks",
  "等待你的输入": "Waiting for you",
  "后台任务已开始：{name}": "Background job started: {name}",
  "后台任务已结束：{name}": "Background job finished: {name}",
  "后台任务已终止：{name}": "Background job killed: {name}",
  "后台任务 {name} 失败，需要处理": "Background job {name} failed: needs attention",
  "后台任务 {name} 已有 {quiet} 没有活动（{work}）：可能在等某个东西，也可能卡住了；可以用 wait 或 bash_output 查看，或用 kill_shell 结束它":
    "Background job {name} has been inactive for {quiet} ({work}): it may be waiting on something, or stuck; check it with wait or bash_output, or stop it with kill_shell",
  "{n} 步在静默前完成": "{n} step(s) completed before going quiet",
  "静默前没有任何进展": "no progress reported",
  "被信号 {signal} 终止": "Ended by signal {signal}",
  "退出码 {code}": "Exit code {code}",
  "已恢复 {n} 条未完成的指令。待发送已暂停，请先在输入框上方的队列里查看，再点“继续派发”": "Recovered {n} unfinished instruction(s). Sending is paused: review them in the queue above the input box, then press Release",
  "待发送已暂停，这条消息已排入队列，点“继续派发”后才会发送": "Sending is paused. This message is queued and goes out once you press Release",
  "引导没有生效：这一轮在处理它之前就结束了。如果仍然需要，请再发送一次：": "Guidance was not applied because the turn ended before it could be processed. Send it again if it is still needed:",
  "费用显示币种已设为 {mode}": "Cost display currency set to {mode}",
  "这个会话保存的压缩结果没有被复用：它所总结的历史之后发生了变化，现在发送的是完整历史，可能超出模型容量": "This conversation's saved compaction was not reused: the history it summarised has changed since. The full history is being sent, which may exceed the model's window.",
  "这个会话保存的压缩结果是在另一个模型或工作区下生成的，与当前历史对不上，没有被复用，现在发送的是完整历史": "This conversation's saved compaction was made under another model or workspace and does not match its history, so it was not reused. The full history is being sent.",
  "这些工作区在同一个 Git 仓库 {repo} 内，同一仓库共用一个写入域；把项目移出该仓库，或在用户配置里设 write_lease = \"optimistic\"，才能并行写入": "These workspaces are inside the same Git repository, {repo}, and one repository is one write domain; move the project out of it, or set write_lease = \"optimistic\" in your user config, to write in parallel",
  "配置文件里的提问等待时长无效（须为 0 或 10 到 86400 之间的整数），提问现在会一直等你回答；到设置里重新设一个": "The question wait time in the config file is not valid (it must be a whole number, 0 or 10 to 86400), so questions wait for you as long as it takes; set it again in Settings.",
  "模型卡在重复输出同一段文字，这一轮已停止；可以重试、补充引导，或换一个供应商/模型": "The model is stuck repeating the same text; this turn was stopped. Try again, add guidance, or switch provider/model.",
  "扩展 {ext} 的配套后台程序没有运行，该扩展本次（在 {point}）已被跳过；到「工具与集成」里查看并启动它，或停用该扩展": "The companion background program of extension {ext} is not running, so the extension was skipped this time (at {point}). Open Tools and integrations to check and start it, or disable the extension.",
  "有 {n} 条权限规则没有对应的工具，匹配不到任何调用，因此起不到限制作用（如「{list}」里的 {rule}）；到「设置 → 权限」里删除或改写，命令要写成 Bash(命令:*)": "{n} permission rule(s) name no tool, so they match no call and restrict nothing (for example {rule} in {list}). Delete or rewrite them under Settings > Permissions; a shell command is written Bash(command:*).",
};
