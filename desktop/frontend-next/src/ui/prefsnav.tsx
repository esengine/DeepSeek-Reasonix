import type { ReactNode } from "react";

/** The settings table of contents: which sections exist, what each is called,
 *  what mark it carries and which question it answers. Kept out of Settings
 *  itself because it is a table, not a screen — the screen reads it. */
export type Section = "session" | "model" | "providers" | "tools" | "hooks" | "ext" | "network" | "remote" | "memory" | "usage" | "storage" | "account" | "versions" | "appearance" | "advanced";

// Drawn on the same 16-unit grid at 1.45 stroke as the rest of this screen's
// marks. The rail in Nav.tsx keeps its own set on purpose: those name panes to
// open, these name sections of one page, and the two lists barely overlap.
export const ICON: Record<Section, ReactNode> = {
  session: <path d="M2.6 8h10.8M8 2.6v10.8" />,
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
  memory: <path d="M8 3.2c-1.4-1.3-4.6-1-4.6 1.9 0 2.4 2.6 4.4 4.6 6 2-1.6 4.6-3.6 4.6-6 0-2.9-3.2-3.2-4.6-1.9Z" />,
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
      ["model", "模型偏好"],
      ["providers", "模型服务"],
      ["tools", "工具与权限"],
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
 *  none            nothing here changes anything: the block only reports. */
export type ApplySemantics = "immediate" | "runtime-rebuild" | "restart" | "none";

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
  { section: "session", anchor: "session-dir", title: "会话写入位置", scope: "workspace", apply: "none", keywords: ["工作目录", "路径"] },

  { section: "model", anchor: "model", title: "按用途选择模型", scope: "machine", apply: "runtime-rebuild", keywords: ["主模型", "默认模型", "模型", "切换", "端点", "按任务指定模型", "角色分工", "子代理", "规划", "看图", "复核", "决策", "system one", "typesafe", "laya"] },
  // Written onto the provider entry, not the model row: SetProviderEffort keys
  // by provider name, so every model reached through that source shares it.
  { section: "model", anchor: "effort", title: "推理强度", scope: "model", apply: "runtime-rebuild", keywords: ["思考", "reasoning", "档位"] },
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
  { section: "tools", anchor: "rules", title: "明确的规则", scope: "machine", apply: "runtime-rebuild", keywords: ["permissions", "允许", "拒绝", "配方"] },
  { section: "tools", anchor: "sandbox", title: "沙箱", scope: "machine", apply: "runtime-rebuild", keywords: ["隔离", "联网", "写权限", "ssh-agent"] },
  { section: "tools", anchor: "shell", title: "命令执行程序", scope: "machine", apply: "runtime-rebuild", keywords: ["bash", "powershell", "解释器"] },
  // The tools are bound while a runtime is assembled, and the save rebuilds this
  // one. The shared [browser] enabled key belongs to the 1.x command line.
  { section: "tools", anchor: "browser-tools", title: "内置浏览器", scope: "machine", apply: "runtime-rebuild", keywords: ["browser", "网页", "chrome", "浏览器工具"] },

  // The four blocks below carry a scope control of their own, and the kernel
  // takes that choice as a parameter — hook.Scope, McpInstallScope, the skill
  // activation scope, a memory's project/global flag. Naming one authority here
  // would be wrong whenever the reader picks the other.
  { section: "hooks", anchor: "hooks", title: "自动化", scope: "chosen", apply: "immediate", keywords: ["钩子", "hook", "触发"] },

  { section: "ext", anchor: "ext-runtime", title: "运行时", scope: "session", apply: "immediate", keywords: ["扩展", "沙盒"] },
  { section: "ext", anchor: "plugins", title: "插件包", scope: "machine", apply: "immediate", keywords: ["安装", "市场"] },
  { section: "ext", anchor: "mcp", title: "外部工具", scope: "chosen", apply: "immediate", keywords: ["mcp", "服务器", "连接外部"] },
  { section: "ext", anchor: "skills", title: "技能", scope: "chosen", apply: "immediate", keywords: ["skill", "技能包"] },

  { section: "network", anchor: "network", title: "网络", scope: "machine", apply: "immediate", keywords: ["代理", "proxy", "抓取", "超时"] },
  { section: "remote", anchor: "remote", title: "远程", scope: "machine", apply: "immediate", keywords: ["ssh", "机器", "远端工作区"] },
  // Machine rather than session: the door is this window's, and a phone that
  // walks through it reaches every pane. Nothing is written, so nothing outlives
  // the window either.
  { section: "remote", anchor: "phone", title: "手机访问", scope: "machine", apply: "immediate", keywords: ["手机", "扫码", "二维码", "局域网", "配对", "phone", "mobile", "qr"] },
  { section: "account", anchor: "account", title: "账号", scope: "account", apply: "immediate", keywords: ["登录", "社区"] },
  { section: "versions", anchor: "versions", title: "版本", scope: "machine", apply: "immediate", keywords: ["更新", "升级"] },
  { section: "memory", anchor: "memory", title: "记忆", scope: "chosen", apply: "immediate", keywords: ["记住", "忘记", "事实"] },
  { section: "usage", anchor: "usage", title: "用量与成本", scope: "machine", apply: "none", keywords: ["token", "花费", "缓存命中"] },
  { section: "storage", anchor: "storage", title: "存储", scope: "machine", apply: "restart", keywords: ["搬家", "迁移", "磁盘", "位置"] },
  { section: "advanced", anchor: "elsewhere", title: "本版本尚未提供", scope: "machine", apply: "none", keywords: ["配置文件"] },

  { section: "appearance", anchor: "language", title: "语言", scope: "machine", apply: "restart", keywords: ["中文", "english", "界面语言"] },
  { section: "appearance", anchor: "folding", title: "会话折叠", scope: "machine", apply: "immediate", keywords: ["展开", "收起", "思考", "执行过程", "步骤", "输出", "简报", "fold"] },
  { section: "appearance", anchor: "window", title: "窗口", scope: "machine", apply: "immediate", keywords: ["托盘", "关闭行为"] },
  // Immediate because the endpoint hands the holder every running sink reads,
  // not because the file was written: writing it is what a restart would need.
  { section: "appearance", anchor: "notify", title: "通知", scope: "machine", apply: "immediate", keywords: ["提醒", "notification", "系统通知", "结束", "批准", "提问"] },
  { section: "appearance", anchor: "size", title: "大小", scope: "machine", apply: "immediate", keywords: ["缩放", "字号"] },
  { section: "appearance", anchor: "font", title: "字体", scope: "machine", apply: "immediate", keywords: ["等宽", "mono"] },
  { section: "appearance", anchor: "wallpaper", title: "壁纸", scope: "machine", apply: "immediate", keywords: ["背景", "图片"] },
  { section: "appearance", anchor: "weight", title: "文字粗细", scope: "machine", apply: "immediate", keywords: ["加粗", "字重"] },
  { section: "appearance", anchor: "contrast", title: "文字对比度", scope: "machine", apply: "immediate", keywords: ["柔和", "对比"] },
  { section: "appearance", anchor: "mode", title: "明暗", scope: "machine", apply: "immediate", keywords: ["深色", "浅色", "跟随系统"] },
  { section: "appearance", anchor: "scheme", title: "配色", scope: "machine", apply: "immediate", keywords: ["主题", "theme", "色板"] },
];

export const SETTING_AT = (anchor: string) => SETTINGS.find((s) => s.anchor === anchor);

export const SECTION_NAME: Partial<Record<Section, string>> = Object.fromEntries(
  NAV.flatMap(([, items]) => items),
) as Partial<Record<Section, string>>;

// Title, aliases, and the page it is on. An alias is a way in and nothing
// more: it never becomes the setting's name and no judgement reads it.
export function settingMatches(e: SettingEntry, q: string): boolean {
  if (e.title.toLowerCase().includes(q)) return true;
  if ((SECTION_NAME[e.section] ?? "").toLowerCase().includes(q)) return true;
  return (e.keywords ?? []).some((k) => k.toLowerCase().includes(q));
}
