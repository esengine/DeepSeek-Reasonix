import type { ReactNode } from "react";
import { MODS_SHOWN } from "./modsgate";
import { t, zh } from "../i18n";

/** The settings table of contents: which sections exist, what each is called,
 *  what mark it carries and which question it answers. Kept out of Settings
 *  itself because it is a table, not a screen — the screen reads it. */
export type Section = "session" | "persona" | "model" | "providers" | "tools" | "peers" | "hooks" | "ext" | "network" | "remote" | "memory" | "usage" | "storage" | "account" | "versions" | "appearance" | "advanced";

// Drawn on the same 16-unit grid at 1.45 stroke as the rest of this screen's
// marks. The rail in Nav.tsx draws a section's entry from this table, so one
// destination carries one mark wherever it is shown.
export const ICON: Record<Section, ReactNode> = {
  session: <path d="M2.6 8h10.8M8 2.6v10.8" />,
  // An identity card: the section is about who the agent is, not what it does.
  persona: (
    <>
      <rect x="2.2" y="3.4" width="11.6" height="9.2" rx="1.4" />
      <circle cx="5.8" cy="7.2" r="1.3" />
      <path d="M8.6 6.2h3.2M8.6 8.4h3.2M3.9 11.4c.5-1.1 1.5-1.7 2.7-1.7s2.2.6 2.7 1.7" />
    </>
  ),
  model: (
    <>
      <circle cx="8" cy="8" r="2.4" />
      <path d="M8 1.8v2.2M8 12v2.2M1.8 8h2.2M12 8h2.2" />
    </>
  ),
  providers: (
    <>
      <rect x="2.4" y="2.6" width="11.2" height="4.2" rx="1.2" />
      <rect x="2.4" y="9.2" width="11.2" height="4.2" rx="1.2" />
      <path d="M5 4.7h3.4M5 11.3h3.4M11 4.7h.01M11 11.3h.01" />
    </>
  ),
  // Two conversations with a line between them.
  peers: (
    <>
      <rect x="2.2" y="2.8" width="6" height="4.6" rx="1.2" />
      <rect x="7.8" y="8.6" width="6" height="4.6" rx="1.2" />
      <path d="M5.2 7.4v1.6a1 1 0 0 0 1 1h1.6" />
    </>
  ),
  tools: (
    <>
      <path d="M8 1.9 13.8 4.4v4.2c0 3-2.4 5.1-5.8 5.7-3.4-.6-5.8-2.7-5.8-5.7V4.4Z" />
      <path d="M6.4 8.1 7.6 9.3l2.4-2.4" />
    </>
  ),
  hooks: (
    <>
      <path d="M4.4 2.6v6.2a3.2 3.2 0 0 0 6.4 0V6.2" />
      <circle cx="10.8" cy="4.4" r="1.6" />
    </>
  ),
  ext: (
    <>
      <rect x="2.4" y="2.4" width="5" height="5" rx="1.2" />
      <rect x="8.6" y="8.6" width="5" height="5" rx="1.2" />
      <path d="M7.4 5h6.2M5 7.4v6.2" />
    </>
  ),
  network: (
    <>
      <circle cx="8" cy="8" r="5.8" />
      <path d="M2.4 8h11.2M8 2.2c1.6 1.7 2.4 3.6 2.4 5.8S9.6 12.1 8 13.8C6.4 12.1 5.6 10.2 5.6 8s.8-4.1 2.4-5.8Z" />
    </>
  ),
  remote: (
    <>
      <rect x="2.2" y="2.6" width="11.6" height="4.4" rx="1.3" />
      <rect x="2.2" y="9" width="11.6" height="4.4" rx="1.3" />
      <path d="M4.8 4.8h.01M4.8 11.2h.01" />
    </>
  ),
  storage: (
    <>
      <ellipse cx="8" cy="4" rx="5.4" ry="2.1" />
      <path d="M2.6 4v8c0 1.2 2.4 2.1 5.4 2.1s5.4-.9 5.4-2.1V4" />
      <path d="M2.6 8c0 1.2 2.4 2.1 5.4 2.1s5.4-.9 5.4-2.1" />
    </>
  ),
  memory: <path d="M8 4.4c-1.4-1.3-4.6-1-4.6 1.9 0 2.4 2.6 4.4 4.6 6 2-1.6 4.6-3.6 4.6-6 0-2.9-3.2-3.2-4.6-1.9Z" />,
  usage: <path d="M2.6 12.4V9M6.2 12.4V5.4M9.8 12.4V7.2M13.4 12.4V3.4" />,
  account: (
    <>
      <circle cx="8" cy="5.6" r="2.6" />
      <path d="M2.9 13.4c.8-2.4 2.7-3.6 5.1-3.6s4.3 1.2 5.1 3.6" />
    </>
  ),
  versions: <path d="M8 2.4v7.4M5.2 7.2 8 10l2.8-2.8M3 12.8h10" />,
  appearance: (
    <>
      <circle cx="8" cy="8" r="3.1" />
      <path d="M8 1.4v1.8M8 12.8v1.8M1.4 8h1.8M12.8 8h1.8M3.4 3.4l1.3 1.3M11.3 11.3l1.3 1.3M12.6 3.4l-1.3 1.3M4.7 11.3l-1.3 1.3" />
    </>
  ),
  advanced: <path d="M3 5h5.2M11.2 5H13M3 11h2.2M8.2 11H13M9.4 3.4v3.2M6.4 9.4v3.2" />,
};

// Grouped by the question each answers, in the order they get asked: what this
// turn does, where it runs, what it keeps, what this machine is. Fourteen flat
// rows made the list something to scan; four short ones make it something to
// aim at.
export const NAV: [string, [Section, string][]][] = [
  [
    "common.thisTurn",
    [
      ["session", "common.session"],
      ["persona", "prefsnav.persona"],
      ["model", "prefsnav.modelPreferences"],
      ["providers", "common.modelServices"],
      ["tools", "common.toolsAndPermissions"],
      ["peers", "prefsnav.sessionMessages"],
      ["hooks", "common.automation"],
      ["ext", "common.extension"],
    ],
  ],
  [
    "common.whereItRuns",
    [
      ["network", "common.network"],
      ["remote", "common.remote"],
      ["storage", "common.storage"],
    ],
  ],
  [
    "prefsnav.whatItRemembers",
    [
      ["memory", "common.memory2"],
      ["usage", "common.usage"],
    ],
  ],
  [
    "prefsnav.thisMachine",
    [
      ["account", "common.account"],
      ["versions", "common.version"],
      ["appearance", "prefsnav.appearance"],
      ["advanced", "prefsnav.advanced"],
    ],
  ],
];

/** When a change to this setting is in force.
 *
 *  Three answers, and they are declared rather than derived: nothing here may
 *  be read off a handler's name, a hint's wording or a component's type. Each
 *  value below was taken from what the endpoint behind the control actually
 *  does — whether its handler reaches the kernel's runtime rebuild — not from
 *  what the screen says about itself.
 *
 *  immediate       canonical state changed and the running runtime already
 *                  shows it; nothing is reassembled.
 *  runtime-rebuild canonical state changed and this session's runtime is
 *                  rebuilt to adopt it. Refused while a turn is running.
 *  restart         written and kept now; this process goes on without it and
 *                  the next launch starts with it. Saved is not the same fact
 *                  as in force, and the row says both.
 *  on-open         written and kept now; the running session keeps what it was
 *                  built with, and so does a rebuild, so the sessions opened
 *                  afterwards are the ones that start with it.
 *  next-turn       written and kept now; the running session takes it at its
 *                  next turn boundary, because it changes what the model is
 *                  given (the tool table), which only moves between turns.
 *  next-rebuild    saved without rebuilding now; the next runtime build reads it.
 *  none            nothing here changes anything: the block only reports. */
export type ApplySemantics = "immediate" | "runtime-rebuild" | "restart" | "on-open" | "next-turn" | "next-rebuild" | "none";

/** Who owns what a block governs — the other half of the contract, and the one
 *  the screen could not answer. "When does it take effect" was declared here
 *  from the start; "does this follow the project, the model, or this machine"
 *  was left to be guessed from which page a block happened to sit on.
 *
 *  Each value below came from reading the endpoint behind the control and the
 *  authority it writes, never from the section. Information architecture is not
 *  ownership: the model page holds one setting stored per provider entry, three
 *  stored machine-wide, and one that is not stored at all.
 *
 *  This answers who a change reaches, not which file the bytes land in. Several
 *  machine-wide values are read by the running session first; what makes them
 *  machine-wide is that every later session reads them too.
 *
 *  session   only this conversation and its runtime; a new session starts over
 *  model     follows the model or its configuration block
 *  workspace follows the project directory
 *  machine   every session on this computer, now and later
 *  account   follows the signed-in identity
 *  chosen    the block itself asks where to write, and the kernel takes that
 *            answer as a parameter — see the note on the entries that use it */
export type SettingScope = "session" | "model" | "workspace" | "machine" | "account" | "chosen";

export interface SettingEntry {
  /** Which page it is on. */
  section: Section;
  /** The id the block renders, and what a search result scrolls to. */
  anchor: string;
  title: string;
  /** Words someone might look for that the title does not contain. These buy
   *  discoverability and nothing else: an alias never becomes the setting's
   *  name, its identity, or anything a judgement is made on. */
  keywords?: string[];
  /** Never optional. A setting whose ownership nobody stated is one every
   *  reader has to guess at, and the guess is the section it is filed under —
   *  which is exactly what this replaces. */
  scope: SettingScope;
  apply: ApplySemantics;
  /** Drawn by nothing yet: left out of search and of the section's count. */
  hidden?: boolean;
}

// One row per block the settings screen renders, checked both ways against
// what it really renders — a block with no row here fails, and a row nothing
// renders fails too.
export const SETTINGS: SettingEntry[] = [
  // The kernel still accepts legacy preset values for old clients and saved
  // sessions, but Studio presents one adaptive completion standard. Asking
  // people to predict the amount of verification before writing the task made
  // an internal policy look like a required work mode.
  { section: "session", anchor: "preset", title: "common.completionStandard", scope: "session", apply: "immediate", keywords: ["common.auto", "common.verify", "common.review", "完成判定"] },
  { section: "session", anchor: "plan-mode", title: "common.planMode", scope: "session", apply: "immediate", keywords: ["common.readOnly", "先规划"] },
  { section: "session", anchor: "default-mode", title: "common.howNewConversationsStart", scope: "machine", apply: "on-open", keywords: ["plan", "默认模式", "协作方式", "default_mode", "common.newSession"] },
  { section: "session", anchor: "receipt", title: "common.deliveryReceiptCard", scope: "machine", apply: "immediate", keywords: ["回执", "验收", "common.verify", "prefsnav.notVerified", "receipt"] },
  // Written to the user file only, and read by the running turn at its next round.
  { section: "session", anchor: "ask-timeout", title: "common.questionWaitTime", scope: "machine", apply: "immediate", keywords: ["ask", "common.skipped", "无人回答", "长任务", "超时", "自动继续"] },
  { section: "session", anchor: "session-ref", title: "common.sizeOfAReferencedConversation", scope: "machine", apply: "immediate", keywords: ["引用", "历史会话", "过往会话", "common.context", "长度", "token", "session"] },
  { section: "ext", anchor: "mods", title: "Claude Code mod", scope: "machine", apply: "on-open", hidden: !MODS_SHOWN, keywords: ["mod", "钩子", "hook", "Claude Code", "完全信任", "common.plugins", "common.extension"] },
  { section: "session", anchor: "progress-watch", title: "common.longStretchesWithoutProgress", scope: "machine", apply: "immediate", keywords: ["common.pause", "无进展", "空转", "卡住", "循环", "轮次", "token"] },
  { section: "session", anchor: "changed-files", title: "common.fileChanges", scope: "machine", apply: "immediate", keywords: ["common.changes", "变更", "common.files2", "已更改", "药丸", "输入框"] },
  { section: "session", anchor: "feedback-level-notices", title: "common.levelNotices", scope: "machine", apply: "immediate", keywords: ["等级", "晋级", "升级", "common.feedback", "common.notice"] },
  { section: "session", anchor: "turn-narration", title: "common.narrationBetweenToolCalls", scope: "machine", apply: "immediate", keywords: ["叙述", "总结", "小轮", "工具之间", "common.detailed", "common.brief", "思路", "监控", "narration"] },
  { section: "session", anchor: "fold-work", title: "common.foldTheWorkInBetween", scope: "machine", apply: "immediate", keywords: ["折叠", "common.workInBetween", "工具调用", "思考", "最后一句", "common.collapse", "common.summary"] },
  // Written to the user file only; the kernel's background sweep reads it on each pass.
  { section: "session", anchor: "auto-archive", title: "common.autoArchiveInactiveConversations", scope: "machine", apply: "immediate", keywords: ["common.archived", "过期", "清理", "闲置", "天数"] },
  { section: "persona", anchor: "composition-picker", title: "common.showModePicker", scope: "machine", apply: "immediate", keywords: ["common.presets", "模式", "picker", "Agent"] },
  // Explicit selections bind immutable versions; catalog saves do not rebuild.
  { section: "persona", anchor: "agent-presets", title: "common.agentPresets", scope: "machine", apply: "on-open", keywords: ["common.presets", "组合", "Agent"] },
  { section: "persona", anchor: "persona", title: "common.systemPersona", scope: "machine", apply: "next-rebuild", keywords: ["prefsnav.persona", "persona", "身份", "提示词", "系统提示词", "前缀", "后缀"] },
  { section: "session", anchor: "session-dir", title: "common.whereThisSessionWrites", scope: "workspace", apply: "none", keywords: ["common.workingDirectory", "路径"] },

  { section: "model", anchor: "model", title: "common.modelsByUse", scope: "machine", apply: "runtime-rebuild", keywords: ["prefsnav.primaryModel", "common.defaultModel", "common.models", "切换", "端点", "prefsnav.modelsByTask", "prefsnav.roles", "common.subagents2", "common.planning", "common.vision", "common.review", "common.decision", "common.webSearch2", "web search", "system one", "typesafe", "laya", "common.autoName", "common.effort", "prefsnav.reasoning", "reasoning", "档位"] },
  // Machine-wide despite sitting on the model page, and the clearest case for
  // not reading scope off a section: the economic threshold is one
  // [agent] key in the user config that every session on this computer reads.
  { section: "model", anchor: "context", title: "common.contextMaintenance", scope: "machine", apply: "runtime-rebuild", keywords: ["common.contextWindow", "common.compaction", "compaction"] },
  // Adding a source does not rebuild; changing which protocol a source is
  // reached through switches the model, and that does. The stronger of the two
  // is what the row promises, because the weaker one would be a promise this
  // block cannot keep.
  { section: "providers", anchor: "providers", title: "common.modelServices", scope: "machine", apply: "runtime-rebuild", keywords: ["模型来源", "common.connections", "提供商", "供应商", "api key", "密钥", "common.protocol2", "common.address"] },
  { section: "providers", anchor: "provider-health", title: "common.automaticallyCheckConnectionsAndBala", scope: "machine", apply: "immediate", keywords: ["检测", "余额", "common.connections", "health", "balance"] },

  // Takes effect on this session at once and is also persisted as the default
  // every later session starts from. Machine is the wider of the two answers
  // and the one a reader cannot see from the screen.
  { section: "tools", anchor: "approval", title: "common.toolApprovals", scope: "machine", apply: "immediate", keywords: ["prefsnav.grant", "yolo", "common.ask", "common.allow"] },
  { section: "tools", anchor: "present-files", title: "common.aIFileDeliveryCards", scope: "machine", apply: "on-open", keywords: ["文件交付", "交付卡片", "文件预览", "present_files", "tools.present_files", "PDF", "CSV", "HTML", "file delivery", "preview"] },
  { section: "tools", anchor: "external-origin", title: "common.outsideText", scope: "machine", apply: "runtime-rebuild", keywords: ["外部", "网页", "common.source", "注入", "提示注入", "origin", "yolo", "auto", "common.ask"] },
  { section: "tools", anchor: "plan-approval", title: "common.planApproval", scope: "machine", apply: "immediate", keywords: ["plan", "common.autoApprove", "长任务", "无人值守", "plan_approval", "common.allow", "common.ask"] },
  { section: "tools", anchor: "tool-list", title: "common.toolsTheModelCanUse", scope: "machine", apply: "on-open", keywords: ["禁用", "关闭工具", "tools.disabled", "省 token", "提示词开销", "web_fetch", "denylist"] },
  { section: "tools", anchor: "rules", title: "common.rulesYouSpellOut", scope: "machine", apply: "runtime-rebuild", keywords: ["permissions", "允许", "common.refuse", "配方"] },
  { section: "tools", anchor: "sandbox", title: "common.sandbox", scope: "machine", apply: "runtime-rebuild", keywords: ["common.isolated", "联网", "写权限", "ssh-agent"] },
  { section: "tools", anchor: "shell", title: "common.whichProgramRunsCommands", scope: "machine", apply: "runtime-rebuild", keywords: ["bash", "powershell", "解释器"] },
  // The tools are bound while a runtime is assembled, and the save rebuilds this
  // one. The shared [browser] enabled key belongs to the 1.x command line.
  // The lease is built with the session and a rebuild adopts the one it holds, so
  // the save reaches only sessions opened afterwards; the handler rebuilds nothing.
  { section: "peers", anchor: "peers", title: "common.messagesBetweenSessionsExperimental", scope: "machine", apply: "next-turn", keywords: ["peer", "会话间", "消息", "common.send", "批准", "挂起", "另一个会话"] },
  { section: "peers", anchor: "peer-projects", title: "common.projectsAllowedThrough", scope: "machine", apply: "next-turn", keywords: ["common.allow", "common.project", "不再询问", "长期", "common.revoke", "peer", "release", "会话间"] },
  { section: "tools", anchor: "write-lease", title: "common.severalSessionsWritingOneProject", scope: "machine", apply: "on-open", keywords: ["lease", "写锁", "并行", "多会话", "乐观", "optimistic", "write_lease", "prefsnav.waitingOn"] },
  { section: "tools", anchor: "subagent-claims", title: "common.subAgentWriteScope", scope: "machine", apply: "runtime-rebuild", keywords: ["subagent", "common.subagents2", "并发", "write_paths", "乐观", "optimistic", "写锁"] },
  { section: "network", anchor: "trace-export", title: "common.traceExport", scope: "machine", apply: "on-open", keywords: ["追踪", "opentelemetry", "otlp", "观测", "遥测", "trace", "collector", "收集器"] },
  { section: "tools", anchor: "web-access", title: "common.webSearchAndFetch", scope: "machine", apply: "on-open", keywords: ["common.search", "searxng", "deepseek", "抓取", "计费", "付费", "web search", "fetch"] },
  { section: "tools", anchor: "browser-tools", title: "common.builtInBrowser", scope: "machine", apply: "runtime-rebuild", keywords: ["browser", "网页", "chrome", "浏览器工具"] },
  // The four blocks below carry a scope control of their own, and the kernel
  // takes that choice as a parameter — hook.Scope, McpInstallScope, the skill
  // activation scope, a memory's project/global flag. Naming one authority here
  // would be wrong whenever the reader picks the other.
  { section: "hooks", anchor: "scheduled-runs", title: "common.scheduledRuns", scope: "machine", apply: "immediate", keywords: ["common.scheduled", "common.plan", "prefsnav.task", "common.everyDay", "schedule", "cron", "unattended", "无人值守"] },
  { section: "hooks", anchor: "hooks", title: "common.automation", scope: "chosen", apply: "immediate", keywords: ["钩子", "hook", "触发"] },

  { section: "ext", anchor: "ext-runtime", title: "common.runtime", scope: "session", apply: "immediate", keywords: ["common.extension", "prefsnav.sandbox"] },
  { section: "ext", anchor: "market", title: "common.communityMarket", scope: "machine", apply: "immediate", keywords: ["市场", "market", "社区", "common.discover", "common.download", "common.publish", "publish", "common.theme"] },
  { section: "ext", anchor: "plugins", title: "common.pluginPackages", scope: "machine", apply: "immediate", keywords: ["common.install"] },
  { section: "ext", anchor: "mcp", title: "common.externalTools", scope: "chosen", apply: "immediate", keywords: ["mcp", "common.server", "连接外部"] },
  { section: "ext", anchor: "skills", title: "common.skills2", scope: "chosen", apply: "immediate", keywords: ["skill", "技能包"] },

  { section: "network", anchor: "network", title: "common.network", scope: "machine", apply: "immediate", keywords: ["common.proxy", "proxy", "抓取", "超时"] },
  { section: "remote", anchor: "remote", title: "common.remote", scope: "machine", apply: "immediate", keywords: ["ssh", "common.machine", "远端工作区"] },
  // Machine rather than session: the door is this window's, and a phone that
  // walks through it reaches every pane. Nothing is written, so nothing outlives
  // the window either.
  { section: "remote", anchor: "phone", title: "common.phoneAccess", scope: "machine", apply: "immediate", keywords: ["手机", "扫码", "二维码", "局域网", "配对", "phone", "mobile", "qr"] },
  { section: "remote", anchor: "phone-notify", title: "common.phoneNotifications", scope: "machine", apply: "immediate", keywords: ["common.push", "common.notify", "手机", "批准", "push", "notification", "phone"] },
  { section: "remote", anchor: "phone-device", title: "common.thisPhoneSReminders", scope: "machine", apply: "immediate", keywords: ["common.push", "common.notify", "手机", "push", "notification"] },
  { section: "account", anchor: "account", title: "common.account", scope: "account", apply: "immediate", keywords: ["common.signIn", "社区"] },
  // Shown only while signed in; restoring writes through the ordinary config paths and reloads the runtime.
  { section: "account", anchor: "backup-file", title: "common.exportAndImportConfig", scope: "machine", apply: "immediate", keywords: ["备份", "common.export", "导入", "换机", "common.migrate", "common.files2"] },
  { section: "account", anchor: "backup", title: "common.cloudBackup", scope: "account", apply: "immediate", keywords: ["备份", "common.resume", "换机", "同步", "common.export"] },
  { section: "versions", anchor: "versions", title: "common.version", scope: "machine", apply: "immediate", keywords: ["common.update", "升级"] },
  { section: "versions", anchor: "update-mode", title: "common.updateMode", scope: "machine", apply: "immediate", keywords: ["自动更新", "检查更新", "更新提醒", "后台", "update", "auto update", "notify", "common.download2", "download"] },
  { section: "versions", anchor: "logs", title: "common.logsAndAbnormalExitRecords", scope: "machine", apply: "none", keywords: ["日志", "崩溃", "闪退", "异常退出", "log", "logs", "crash", "路径"] },
  { section: "versions", anchor: "community", title: "common.communityAndContributors", scope: "machine", apply: "none", keywords: ["社区", "交流群", "QQ", "加群", "二维码", "discord", "common.contributors", "contributors", "community"] },
  { section: "versions", anchor: "sponsor", title: "common.supportTheProject", scope: "machine", apply: "none", keywords: ["捐助", "common.alipay", "common.weChatPay", "收款码", "paypal", "donate", "sponsor", "support", "alipay", "wechat"] },
  { section: "memory", anchor: "memory", title: "common.memory2", scope: "chosen", apply: "immediate", keywords: ["记住", "common.forget", "事实"] },
  { section: "usage", anchor: "currency", title: "common.costDisplayCurrency", scope: "machine", apply: "immediate", keywords: ["币种", "货币", "人民币", "美元", "currency", "CNY", "USD", "汇率"] },
  { section: "usage", anchor: "upload-usage", title: "common.uploadVolume", scope: "machine", apply: "none", keywords: ["流量", "common.upload", "带宽", "common.requests", "upload", "traffic", "bandwidth"] },
  { section: "usage", anchor: "usage", title: "common.usageCost", scope: "machine", apply: "none", keywords: ["token", "common.spend", "common.cacheHit"] },
  { section: "storage", anchor: "storage", title: "common.storage", scope: "machine", apply: "restart", keywords: ["搬家", "common.migrate", "磁盘", "common.locations"] },
  { section: "advanced", anchor: "elsewhere", title: "common.notInThisVersionYet", scope: "machine", apply: "none", keywords: ["配置文件"] },

  { section: "appearance", anchor: "language", title: "common.language", scope: "machine", apply: "restart", keywords: ["common.text", "english", "界面语言"] },
  { section: "appearance", anchor: "graphics", title: "common.graphics", scope: "machine", apply: "restart", keywords: ["硬件加速", "显卡", "gpu", "common.softwareRendering", "common.spaceUsed", "卡顿"] },
  { section: "appearance", anchor: "navrail", title: "common.iconRail", scope: "machine", apply: "immediate", keywords: ["侧边栏", "导航", "图标", "sidebar", "nav"] },
  { section: "appearance", anchor: "naventries", title: "common.navigationEntries", scope: "machine", apply: "immediate", keywords: ["侧边栏", "导航", "common.hide", "common.sort", "common.custom", "common.surface", "sidebar", "nav", "hide", "reorder", "customize"] },
  { section: "appearance", anchor: "session-quick-actions", title: "common.sessionRowButtons", scope: "machine", apply: "immediate", keywords: ["common.session", "common.archived", "common.delete", "快捷", "按钮", "hover", "archive", "delete", "session", "row", "sidebar"] },
  { section: "appearance", anchor: "new-answer-marker", title: "common.newAnswerDivider", scope: "machine", apply: "immediate", keywords: ["common.unread", "新回答", "分隔线", "分割线", "unread", "new", "divider", "marker", "transcript"] },
  { section: "appearance", anchor: "open-session-limit", title: "common.sessionTabsKeptOpen", scope: "machine", apply: "immediate", keywords: ["common.tags", "窗格", "common.collapse", "保留", "tabs", "panes", "warm", "common.memory", "memory"] },
  { section: "appearance", anchor: "live-stream-budget", title: "common.sessionTabsKeptLive", scope: "machine", apply: "immediate", keywords: ["common.connections", "实时", "common.tags", "窗格", "卡死", "common.stop", "connection", "stream", "tabs", "stuck", "busy"] },
  { section: "appearance", anchor: "legacy-sessions", title: "common.sessionsFrom1X", scope: "machine", apply: "immediate", keywords: ["1.x", "旧会话", "common.migrate", "common.hide", "legacy", "sessions", "sidebar"] },
  { section: "appearance", anchor: "live-grace", title: "common.keepFinishedSessionsInRunning", scope: "machine", apply: "immediate", keywords: ["common.running2", "common.done2", "保留", "过滤", "running", "grace", "sidebar"] },
  { section: "appearance", anchor: "send-shortcut", title: "common.sendShortcut", scope: "machine", apply: "immediate", keywords: ["common.send", "回车", "换行", "enter", "ctrl", "cmd"] },
  { section: "appearance", anchor: "running-message-intent", title: "common.messagesSentWhileRunning", scope: "machine", apply: "immediate", keywords: ["common.steer", "common.queued", "下一轮", "steer", "queue", "followup", "running"] },
  { section: "appearance", anchor: "global-shortcut", title: "common.globalShowHideShortcut", scope: "machine", apply: "immediate", keywords: ["global", "show", "hide", "shortcut", "keyboard", "全局", "common.show", "common.hide"] },
  { section: "appearance", anchor: "keys", title: "common.keyboardShortcuts", scope: "machine", apply: "immediate", keywords: ["shortcut", "keyboard", "ctrl", "cmd", "esc", "home", "common.find", "common.search", "common.runAnalysis", "common.workbench"] },
  { section: "appearance", anchor: "esc-stop", title: "common.escStopsATask", scope: "machine", apply: "immediate", keywords: ["esc", "common.stop", "中断", "common.confirm", "误触", "stop"] },
  { section: "appearance", anchor: "page-sleep", title: "common.idlePageSleep", scope: "machine", apply: "immediate", keywords: ["休眠", "睡眠", "common.idle", "common.memory", "common.collapse", "browser", "page", "sleep", "memory"] },
  { section: "appearance", anchor: "session-tabs", title: "common.sessionTabs", scope: "machine", apply: "immediate", keywords: ["common.tags", "会话切换", "tabs", "session"] },
  { section: "appearance", anchor: "folding", title: "common.transcriptFolding", scope: "machine", apply: "immediate", keywords: ["common.expand", "common.collapse", "思考", "common.activity", "步骤", "common.output", "简报", "fold"] },
  { section: "appearance", anchor: "plan-progress", title: "common.showCollapsedPlanProgress", scope: "machine", apply: "immediate", keywords: ["common.plan", "进度条", "待办", "common.collapse", "todo", "plan", "progress", "collapsed"] },
  { section: "appearance", anchor: "composer-hint", title: "common.composerHint", scope: "machine", apply: "immediate", keywords: ["占位", "prefsnav.task", "common.notice", "输入框", "placeholder", "composer", "hint"] },
  { section: "appearance", anchor: "office-preview", title: "common.officeDocumentPreview", scope: "machine", apply: "immediate", keywords: ["office", "word", "excel", "docx", "xlsx", "common.workbench", "common.preview", "文档", "表格"] },
  { section: "appearance", anchor: "window", title: "common.window", scope: "machine", apply: "immediate", keywords: ["托盘", "关闭行为", "休眠", "睡眠"] },
  // Immediate because the endpoint hands the holder every running sink reads,
  // not because the file was written: writing it is what a restart would need.
  { section: "appearance", anchor: "notify", title: "common.notifications", scope: "machine", apply: "immediate", keywords: ["common.notify", "notification", "系统通知", "结束", "批准", "提问"] },
  { section: "appearance", anchor: "size", title: "common.size", scope: "machine", apply: "immediate", keywords: ["common.zoom", "字号", "common.lineWidth", "common.conversationWidth", "比例", "width", "percentage"] },
  { section: "appearance", anchor: "font", title: "common.typeface", scope: "machine", apply: "immediate", keywords: ["prefsnav.monospace", "mono"] },
  { section: "appearance", anchor: "wallpaper", title: "common.wallpaper", scope: "machine", apply: "immediate", keywords: ["背景", "common.image"] },
  { section: "appearance", anchor: "advanced-appearance", title: "common.advancedAppearance", scope: "machine", apply: "immediate", keywords: ["prefsnav.advanced", "advanced"] },
  { section: "appearance", anchor: "weight", title: "common.textWeight", scope: "machine", apply: "immediate", keywords: ["common.bolder", "字重"] },
  { section: "appearance", anchor: "contrast", title: "common.textContrast", scope: "machine", apply: "immediate", keywords: ["common.soft", "对比"] },
  { section: "appearance", anchor: "effects", title: "common.visualEffects", scope: "machine", apply: "immediate", keywords: ["磨砂", "模糊", "动画", "显卡", "gpu", "性能", "省电", "blur"] },
  { section: "appearance", anchor: "mode", title: "common.lightAndDark", scope: "machine", apply: "immediate", keywords: ["common.dark", "common.light", "common.followTheSystem", "common.scheduled", "时间段", "schedule", "dark", "light"] },
  { section: "appearance", anchor: "scheme", title: "prefsnav.palette", scope: "machine", apply: "immediate", keywords: ["common.theme", "theme", "色板"] },
];

export const SETTING_AT = (anchor: string) => SETTINGS.find((s) => s.anchor === anchor);

export const SECTION_NAME: Partial<Record<Section, string>> = Object.fromEntries(
  NAV.flatMap(([, items]) => items),
) as Partial<Record<Section, string>>;

// Title, aliases, and the page it is on. An alias is a way in and nothing
// more: it never becomes the setting's name and no judgement reads it.
export function settingMatches(e: SettingEntry, q: string): boolean {
  const reads = (name: string) => [t(name), zh(name)].some((text) => text.toLowerCase().includes(q));
  if ([e.title, SECTION_NAME[e.section] ?? ""].some(reads)) return true;
  return (e.keywords ?? []).some(reads);
}
