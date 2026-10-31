import type { ReactNode } from "react";
import { MODS_SHOWN } from "./modsgate";
import { t } from "../i18n";

/** The settings table of contents: which sections exist, what each is called,
 *  what mark it carries and which question it answers. Kept out of Settings
 *  itself because it is a table, not a screen — the screen reads it. */
export type Section = "session" | "persona" | "model" | "providers" | "tools" | "peers" | "team" | "hooks" | "ext" | "network" | "remote" | "memory" | "usage" | "storage" | "account" | "versions" | "appearance" | "advanced";

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
  // Three heads: the agents of a team.
  team: (
    <>
      <circle cx="8" cy="5.2" r="2.2" />
      <circle cx="3.8" cy="9.6" r="1.8" />
      <circle cx="12.2" cy="9.6" r="1.8" />
      <path d="M4.6 13.6c.7-1.9 2-2.8 3.4-2.8s2.7.9 3.4 2.8" />
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
    "本轮执行",
    [
      ["session", "会话"],
      ["persona", "人设"],
      ["model", "模型偏好"],
      ["providers", "模型服务"],
      ["tools", "工具与权限"],
      ["peers", "会话间消息"],
      ["team", "团队"],
      ["hooks", "自动化"],
      ["ext", "扩展"],
    ],
  ],
  [
    "运行环境",
    [
      ["network", "网络"],
      ["remote", "远程"],
      ["storage", "存储"],
    ],
  ],
  [
    "记录与用量",
    [
      ["memory", "记忆"],
      ["usage", "用量"],
    ],
  ],
  [
    "这台机器",
    [
      ["account", "账号"],
      ["versions", "版本"],
      ["appearance", "外观"],
      ["advanced", "高级"],
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
  { section: "session", anchor: "preset", title: "完成标准", scope: "session", apply: "immediate", keywords: ["自动", "验证", "复核", "完成判定"] },
  { section: "session", anchor: "plan-mode", title: "计划模式", scope: "session", apply: "immediate", keywords: ["只读", "先规划"] },
  { section: "session", anchor: "default-mode", title: "新对话的起始模式", scope: "machine", apply: "on-open", keywords: ["plan", "默认模式", "协作方式", "default_mode", "新会话"] },
  { section: "session", anchor: "receipt", title: "交付验收卡片", scope: "machine", apply: "immediate", keywords: ["回执", "验收", "验证", "未验证", "receipt"] },
  // Written to the user file only, and read by the running turn at its next round.
  { section: "session", anchor: "ask-timeout", title: "提问等待超时", scope: "machine", apply: "immediate", keywords: ["ask", "跳过", "无人回答", "长任务", "超时", "自动继续"] },
  { section: "session", anchor: "session-ref", title: "引用历史会话的长度", scope: "machine", apply: "immediate", keywords: ["引用", "历史会话", "过往会话", "上下文", "长度", "token", "session"] },
  { section: "ext", anchor: "mods", title: "Claude Code mod", scope: "machine", apply: "on-open", hidden: !MODS_SHOWN, keywords: ["mod", "钩子", "hook", "Claude Code", "完全信任", "插件", "扩展"] },
  { section: "session", anchor: "progress-watch", title: "长时间无进展", scope: "machine", apply: "immediate", keywords: ["暂停", "无进展", "空转", "卡住", "循环", "轮次", "token"] },
  { section: "session", anchor: "changed-files", title: "文件改动", scope: "machine", apply: "immediate", keywords: ["改动", "变更", "文件", "已更改", "药丸", "输入框"] },
  { section: "session", anchor: "feedback-level-notices", title: "等级提示", scope: "machine", apply: "immediate", keywords: ["等级", "晋级", "升级", "反馈", "提示"] },
  { section: "session", anchor: "turn-narration", title: "回合间叙述", scope: "machine", apply: "immediate", keywords: ["叙述", "总结", "小轮", "工具之间", "详细", "简短", "思路", "监控", "narration"] },
  { section: "session", anchor: "fold-work", title: "折叠中间过程", scope: "machine", apply: "immediate", keywords: ["折叠", "中间过程", "工具调用", "思考", "最后一句", "收起", "摘要"] },
  // Written to the user file only; the kernel's background sweep reads it on each pass.
  { section: "session", anchor: "auto-archive", title: "自动归档不活跃的对话", scope: "machine", apply: "immediate", keywords: ["归档", "过期", "清理", "闲置", "天数"] },
  { section: "persona", anchor: "composition-picker", title: "显示模式选择器", scope: "machine", apply: "immediate", keywords: ["预设", "模式", "picker", "Agent"] },
  // Explicit selections bind immutable versions; catalog saves do not rebuild.
  { section: "persona", anchor: "agent-presets", title: "Agent 预设", scope: "machine", apply: "on-open", keywords: ["预设", "组合", "Agent"] },
  { section: "persona", anchor: "persona", title: "系统人设", scope: "machine", apply: "next-rebuild", keywords: ["人设", "persona", "身份", "提示词", "系统提示词", "前缀", "后缀"] },
  { section: "session", anchor: "session-dir", title: "会话写入位置", scope: "workspace", apply: "none", keywords: ["工作目录", "路径"] },

  { section: "model", anchor: "model", title: "按用途选择模型", scope: "machine", apply: "runtime-rebuild", keywords: ["主模型", "默认模型", "模型", "切换", "端点", "按任务指定模型", "角色分工", "子代理", "规划", "看图", "复核", "决策", "网页搜索", "web search", "system one", "typesafe", "laya", "自动命名", "思考强度", "推理", "reasoning", "档位"] },
  // Machine-wide despite sitting on the model page, and the clearest case for
  // not reading scope off a section: the economic threshold is one
  // [agent] key in the user config that every session on this computer reads.
  { section: "model", anchor: "context", title: "上下文维护", scope: "machine", apply: "runtime-rebuild", keywords: ["上下文窗口", "压缩", "compaction"] },
  // Adding a source does not rebuild; changing which protocol a source is
  // reached through switches the model, and that does. The stronger of the two
  // is what the row promises, because the weaker one would be a promise this
  // block cannot keep.
  { section: "providers", anchor: "providers", title: "模型服务", scope: "machine", apply: "runtime-rebuild", keywords: ["模型来源", "连接", "提供商", "供应商", "api key", "密钥", "协议", "地址"] },

  // Takes effect on this session at once and is also persisted as the default
  // every later session starts from. Machine is the wider of the two answers
  // and the one a reader cannot see from the screen.
  { section: "tools", anchor: "approval", title: "工具批准", scope: "machine", apply: "immediate", keywords: ["权限", "yolo", "询问", "放行"] },
  { section: "tools", anchor: "present-files", title: "AI 文件交付卡片", scope: "machine", apply: "on-open", keywords: ["文件交付", "交付卡片", "文件预览", "present_files", "tools.present_files", "PDF", "CSV", "HTML", "file delivery", "preview"] },
  { section: "tools", anchor: "external-origin", title: "外部文字", scope: "machine", apply: "runtime-rebuild", keywords: ["外部", "网页", "来源", "注入", "提示注入", "origin", "yolo", "auto", "询问"] },
  { section: "tools", anchor: "plan-approval", title: "计划批准", scope: "machine", apply: "immediate", keywords: ["plan", "自动批准", "长任务", "无人值守", "plan_approval", "放行", "询问"] },
  { section: "tools", anchor: "tool-list", title: "模型可用的工具", scope: "machine", apply: "on-open", keywords: ["禁用", "关闭工具", "tools.disabled", "省 token", "提示词开销", "web_fetch", "denylist"] },
  { section: "tools", anchor: "rules", title: "明确的规则", scope: "machine", apply: "runtime-rebuild", keywords: ["permissions", "允许", "拒绝", "配方"] },
  { section: "tools", anchor: "sandbox", title: "沙箱", scope: "machine", apply: "runtime-rebuild", keywords: ["隔离", "联网", "写权限", "ssh-agent"] },
  { section: "tools", anchor: "shell", title: "命令执行程序", scope: "machine", apply: "runtime-rebuild", keywords: ["bash", "powershell", "解释器"] },
  // The tools are bound while a runtime is assembled, and the save rebuilds this
  // one. The shared [browser] enabled key belongs to the 1.x command line.
  // The lease is built with the session and a rebuild adopts the one it holds, so
  // the save reaches only sessions opened afterwards; the handler rebuilds nothing.
  { section: "peers", anchor: "peers", title: "会话间消息（实验）", scope: "machine", apply: "next-turn", keywords: ["peer", "会话间", "消息", "发送", "批准", "挂起", "另一个会话"] },
  // Kept in this window's storage and read by the page at once.
  { section: "team", anchor: "team-motion", title: "Agent 动效", scope: "machine", apply: "immediate", keywords: ["动画", "动效", "安静", "活泼", "眨眼", "motion"] },
  // A roster write; the sidebar draws the row again on the change notice.
  { section: "team", anchor: "team-hidden", title: "已隐藏的 Agent 和群聊", scope: "machine", apply: "immediate", keywords: ["隐藏", "侧栏", "恢复", "显示"] },
  { section: "peers", anchor: "peer-projects", title: "放行的项目", scope: "machine", apply: "next-turn", keywords: ["放行", "项目", "不再询问", "长期", "撤销", "peer", "release", "会话间"] },
  { section: "tools", anchor: "write-lease", title: "多个会话同时写一个项目", scope: "machine", apply: "on-open", keywords: ["lease", "写锁", "并行", "多会话", "乐观", "optimistic", "write_lease", "等待"] },
  { section: "tools", anchor: "subagent-claims", title: "子代理的写入范围", scope: "machine", apply: "runtime-rebuild", keywords: ["subagent", "子代理", "并发", "write_paths", "乐观", "optimistic", "写锁"] },
  { section: "network", anchor: "trace-export", title: "追踪导出", scope: "machine", apply: "on-open", keywords: ["追踪", "opentelemetry", "otlp", "观测", "遥测", "trace", "collector", "收集器"] },
  { section: "tools", anchor: "subagent-steer", title: "给运行中子代理的指示", scope: "machine", apply: "immediate", keywords: ["subagent", "子代理", "指示", "补充", "steer", "上限", "频率", "字节"] },
  { section: "tools", anchor: "web-access", title: "联网：搜索与抓取", scope: "machine", apply: "on-open", keywords: ["搜索", "searxng", "deepseek", "抓取", "计费", "付费", "web search", "fetch"] },
  { section: "tools", anchor: "browser-tools", title: "内置浏览器", scope: "machine", apply: "runtime-rebuild", keywords: ["browser", "网页", "chrome", "浏览器工具"] },
  // The four blocks below carry a scope control of their own, and the kernel
  // takes that choice as a parameter — hook.Scope, McpInstallScope, the skill
  // activation scope, a memory's project/global flag. Naming one authority here
  // would be wrong whenever the reader picks the other.
  { section: "hooks", anchor: "scheduled-runs", title: "定时运行", scope: "machine", apply: "immediate", keywords: ["定时", "计划", "任务", "每天", "schedule", "cron", "unattended", "无人值守"] },
  { section: "hooks", anchor: "hooks", title: "自动化", scope: "chosen", apply: "immediate", keywords: ["钩子", "hook", "触发"] },

  { section: "ext", anchor: "ext-runtime", title: "运行时", scope: "session", apply: "immediate", keywords: ["扩展", "沙盒"] },
  { section: "ext", anchor: "market", title: "社区市场", scope: "machine", apply: "immediate", keywords: ["市场", "market", "社区", "发现", "下载", "发布", "publish", "主题"] },
  { section: "ext", anchor: "plugins", title: "插件包", scope: "machine", apply: "immediate", keywords: ["安装"] },
  { section: "ext", anchor: "mcp", title: "外部工具", scope: "chosen", apply: "immediate", keywords: ["mcp", "服务器", "连接外部"] },
  { section: "ext", anchor: "skills", title: "技能", scope: "chosen", apply: "immediate", keywords: ["skill", "技能包"] },

  { section: "network", anchor: "network", title: "网络", scope: "machine", apply: "immediate", keywords: ["代理", "proxy", "抓取", "超时"] },
  { section: "remote", anchor: "remote", title: "远程", scope: "machine", apply: "immediate", keywords: ["ssh", "机器", "远端工作区"] },
  // Machine rather than session: the door is this window's, and a phone that
  // walks through it reaches every pane. Nothing is written, so nothing outlives
  // the window either.
  { section: "remote", anchor: "phone", title: "手机访问", scope: "machine", apply: "immediate", keywords: ["手机", "扫码", "二维码", "局域网", "配对", "phone", "mobile", "qr"] },
  { section: "remote", anchor: "phone-notify", title: "手机通知", scope: "machine", apply: "immediate", keywords: ["推送", "提醒", "手机", "批准", "push", "notification", "phone"] },
  { section: "remote", anchor: "phone-device", title: "这台手机的提醒", scope: "machine", apply: "immediate", keywords: ["推送", "提醒", "手机", "push", "notification"] },
  { section: "account", anchor: "account", title: "账号", scope: "account", apply: "immediate", keywords: ["登录", "社区"] },
  // Shown only while signed in; restoring writes through the ordinary config paths and reloads the runtime.
  { section: "account", anchor: "backup-file", title: "导出与导入配置", scope: "machine", apply: "immediate", keywords: ["备份", "导出", "导入", "换机", "迁移", "文件"] },
  { section: "account", anchor: "backup", title: "云备份", scope: "account", apply: "immediate", keywords: ["备份", "恢复", "换机", "同步", "导出"] },
  { section: "versions", anchor: "versions", title: "版本", scope: "machine", apply: "immediate", keywords: ["更新", "升级"] },
  { section: "versions", anchor: "update-mode", title: "更新方式", scope: "machine", apply: "immediate", keywords: ["自动更新", "检查更新", "更新提醒", "后台", "update", "auto update", "notify", "自动下载", "download"] },
  { section: "versions", anchor: "logs", title: "日志与异常退出记录", scope: "machine", apply: "none", keywords: ["日志", "崩溃", "闪退", "异常退出", "log", "logs", "crash", "路径"] },
  { section: "versions", anchor: "community", title: "社区与贡献者", scope: "machine", apply: "none", keywords: ["社区", "交流群", "QQ", "加群", "二维码", "discord", "贡献者", "contributors", "community"] },
  { section: "versions", anchor: "sponsor", title: "赞助", scope: "machine", apply: "none", keywords: ["捐助", "支付宝", "微信支付", "收款码", "paypal", "donate", "sponsor", "support", "alipay", "wechat"] },
  { section: "memory", anchor: "memory", title: "记忆", scope: "chosen", apply: "immediate", keywords: ["记住", "忘记", "事实"] },
  { section: "usage", anchor: "currency", title: "费用显示币种", scope: "machine", apply: "immediate", keywords: ["币种", "货币", "人民币", "美元", "currency", "CNY", "USD", "汇率"] },
  { section: "usage", anchor: "upload-usage", title: "上传量", scope: "machine", apply: "none", keywords: ["流量", "上传", "带宽", "请求", "upload", "traffic", "bandwidth"] },
  { section: "usage", anchor: "usage", title: "用量与成本", scope: "machine", apply: "none", keywords: ["token", "花费", "缓存命中"] },
  { section: "storage", anchor: "storage", title: "存储", scope: "machine", apply: "restart", keywords: ["搬家", "迁移", "磁盘", "位置"] },
  { section: "advanced", anchor: "elsewhere", title: "本版本尚未提供", scope: "machine", apply: "none", keywords: ["配置文件"] },

  { section: "appearance", anchor: "language", title: "语言", scope: "machine", apply: "restart", keywords: ["中文", "english", "界面语言"] },
  { section: "appearance", anchor: "graphics", title: "图形渲染", scope: "machine", apply: "restart", keywords: ["硬件加速", "显卡", "gpu", "软件渲染", "占用", "卡顿"] },
  { section: "appearance", anchor: "navrail", title: "图标栏", scope: "machine", apply: "immediate", keywords: ["侧边栏", "导航", "图标", "sidebar", "nav"] },
  { section: "appearance", anchor: "naventries", title: "导航入口", scope: "machine", apply: "immediate", keywords: ["侧边栏", "导航", "隐藏", "排序", "自定义", "入口", "sidebar", "nav", "hide", "reorder", "customize"] },
  { section: "appearance", anchor: "session-quick-actions", title: "会话行快捷按钮", scope: "machine", apply: "immediate", keywords: ["会话", "归档", "删除", "快捷", "按钮", "hover", "archive", "delete", "session", "row", "sidebar"] },
  { section: "appearance", anchor: "new-answer-marker", title: "新回答分隔线", scope: "machine", apply: "immediate", keywords: ["未读", "新回答", "分隔线", "分割线", "unread", "new", "divider", "marker", "transcript"] },
  { section: "appearance", anchor: "open-session-limit", title: "同时保留的会话标签数", scope: "machine", apply: "immediate", keywords: ["标签", "窗格", "收起", "保留", "tabs", "panes", "warm", "内存", "memory"] },
  { section: "appearance", anchor: "live-stream-budget", title: "保持实时连接的会话标签数", scope: "machine", apply: "immediate", keywords: ["连接", "实时", "标签", "窗格", "卡死", "停止", "connection", "stream", "tabs", "stuck", "busy"] },
  { section: "appearance", anchor: "legacy-sessions", title: "1.x 旧会话", scope: "machine", apply: "immediate", keywords: ["1.x", "旧会话", "迁移", "隐藏", "legacy", "sessions", "sidebar"] },
  { section: "appearance", anchor: "live-grace", title: "进行中列表保留已完成会话", scope: "machine", apply: "immediate", keywords: ["进行中", "完成", "保留", "过滤", "running", "grace", "sidebar"] },
  { section: "appearance", anchor: "send-shortcut", title: "发送快捷键", scope: "machine", apply: "immediate", keywords: ["发送", "回车", "换行", "enter", "ctrl", "cmd"] },
  { section: "appearance", anchor: "running-message-intent", title: "运行中发送消息", scope: "machine", apply: "immediate", keywords: ["插话", "排队", "下一轮", "steer", "queue", "followup", "running"] },
  { section: "appearance", anchor: "global-shortcut", title: "全局显示 / 隐藏快捷键", scope: "machine", apply: "immediate", keywords: ["global", "show", "hide", "shortcut", "keyboard", "全局", "显示", "隐藏"] },
  { section: "appearance", anchor: "keys", title: "快捷键", scope: "machine", apply: "immediate", keywords: ["shortcut", "keyboard", "ctrl", "cmd", "esc", "home", "查找", "搜索", "运行分析", "工作台"] },
  { section: "appearance", anchor: "esc-stop", title: "Esc 停止任务", scope: "machine", apply: "immediate", keywords: ["esc", "停止", "中断", "确认", "误触", "stop"] },
  { section: "appearance", anchor: "page-sleep", title: "空闲网页休眠", scope: "machine", apply: "immediate", keywords: ["休眠", "睡眠", "空闲", "内存", "收起", "browser", "page", "sleep", "memory"] },
  { section: "appearance", anchor: "session-tabs", title: "会话标签栏", scope: "machine", apply: "immediate", keywords: ["标签", "会话切换", "tabs", "session"] },
  { section: "appearance", anchor: "folding", title: "会话折叠", scope: "machine", apply: "immediate", keywords: ["展开", "收起", "思考", "执行过程", "步骤", "输出", "简报", "fold"] },
  { section: "appearance", anchor: "plan-progress", title: "收起时显示计划进度", scope: "machine", apply: "immediate", keywords: ["计划", "进度条", "待办", "收起", "todo", "plan", "progress", "collapsed"] },
  { section: "appearance", anchor: "composer-hint", title: "输入框提示", scope: "machine", apply: "immediate", keywords: ["占位", "任务", "提示", "输入框", "placeholder", "composer", "hint"] },
  { section: "appearance", anchor: "window", title: "窗口", scope: "machine", apply: "immediate", keywords: ["托盘", "关闭行为", "休眠", "睡眠"] },
  // Immediate because the endpoint hands the holder every running sink reads,
  // not because the file was written: writing it is what a restart would need.
  { section: "appearance", anchor: "notify", title: "通知", scope: "machine", apply: "immediate", keywords: ["提醒", "notification", "系统通知", "结束", "批准", "提问"] },
  { section: "appearance", anchor: "size", title: "大小", scope: "machine", apply: "immediate", keywords: ["缩放", "字号", "行宽", "对话区域宽度", "比例", "width", "percentage"] },
  { section: "appearance", anchor: "font", title: "字体", scope: "machine", apply: "immediate", keywords: ["等宽", "mono"] },
  { section: "appearance", anchor: "wallpaper", title: "壁纸", scope: "machine", apply: "immediate", keywords: ["背景", "图片"] },
  { section: "appearance", anchor: "advanced-appearance", title: "高级外观", scope: "machine", apply: "immediate", keywords: ["高级", "advanced"] },
  { section: "appearance", anchor: "weight", title: "文字粗细", scope: "machine", apply: "immediate", keywords: ["加粗", "字重"] },
  { section: "appearance", anchor: "contrast", title: "文字对比度", scope: "machine", apply: "immediate", keywords: ["柔和", "对比"] },
  { section: "appearance", anchor: "effects", title: "视觉效果", scope: "machine", apply: "immediate", keywords: ["磨砂", "模糊", "动画", "显卡", "gpu", "性能", "省电", "blur"] },
  { section: "appearance", anchor: "mode", title: "明暗", scope: "machine", apply: "immediate", keywords: ["深色", "浅色", "跟随系统", "定时", "时间段", "schedule", "dark", "light"] },
  { section: "appearance", anchor: "scheme", title: "配色", scope: "machine", apply: "immediate", keywords: ["主题", "theme", "色板"] },
];

export const SETTING_AT = (anchor: string) => SETTINGS.find((s) => s.anchor === anchor);

export const SECTION_NAME: Partial<Record<Section, string>> = Object.fromEntries(
  NAV.flatMap(([, items]) => items),
) as Partial<Record<Section, string>>;

// Title, aliases, and the page it is on. An alias is a way in and nothing
// more: it never becomes the setting's name and no judgement reads it.
export function settingMatches(e: SettingEntry, q: string): boolean {
  if ([e.title, SECTION_NAME[e.section] ?? ""].some((name) =>
    name.toLowerCase().includes(q) || t(name).toLowerCase().includes(q),
  )) return true;
  return (e.keywords ?? []).some((k) => k.toLowerCase().includes(q));
}
