import { StudioIcon } from "./StudioIcon";
import type { ReactNode } from "react";
import { MODS_SHOWN } from "./modsgate";
import { t } from "../i18n";
import { rankTerms, type SettingTerms } from "./settingRank";

/** The settings table of contents: which sections exist, what each is called,
 *  what mark it carries and which question it answers. Kept out of Settings
 *  itself because it is a table, not a screen — the screen reads it. */
export type Section = "common" | "display" | "controls" | "session" | "persona" | "model" | "providers" | "tools" | "peers" | "hooks" | "ext" | "network" | "remote" | "memory" | "usage" | "storage" | "account" | "versions" | "appearance" | "advanced";

export const ICON: Record<Section, ReactNode> = {
  display: <StudioIcon name="monitor" />,
  controls: <StudioIcon name="keyboard" />,
  common: <StudioIcon name="settings" />,
  session: <StudioIcon name="chat" />,
  persona: <StudioIcon name="user" />,
  model: <StudioIcon name="brain" />,
  providers: <StudioIcon name="server" />,
  peers: <StudioIcon name="network" />,
  tools: <StudioIcon name="shield" />,
  hooks: <StudioIcon name="bolt" />,
  ext: <StudioIcon name="plug" />,
  network: <StudioIcon name="globe" />,
  remote: <StudioIcon name="server" />,
  storage: <StudioIcon name="storage" />,
  memory: <StudioIcon name="bookmark" />,
  usage: <StudioIcon name="chart" />,
  account: <StudioIcon name="user" />,
  versions: <StudioIcon name="download" />,
  appearance: <StudioIcon name="palette" />,
  advanced: <StudioIcon name="sliders" />,
};

// Grouped by the question each answers, in the order they get asked: what this
// turn does, where it runs, what it keeps, what this machine is. Fourteen flat
// rows made the list something to scan; four short ones make it something to
// aim at.
export const NAV: [string, [Section, string][]][] = [
  [
    "",
    [
      ["common", "settings.common"],
      ["model", "common.models"],
      ["session", "settings.agent_behaviour"],
      ["tools", "settings.permissions_and_safety"],
      ["ext", "ext.extension"],
      ["display", "settings.conversation_display"],
      ["controls", "settings.controls_and_performance"],
      ["appearance", "settings.appearance"],
      ["storage", "settings.data"],
      ["network", "usage.connections"],
      ["account", "settings.account_and_about"],
      ["advanced", "settings.advanced"],
    ],
  ],
];

export const SECTION_DESC: Partial<Record<Section, string>> = {
  common: "settings.the_few_settings_changed_most",
  model: "settings.which_service_to_use_which",
  session: "settings.how_it_thinks_how_it",
  tools: "settings.what_it_can_touch_approval",
  ext: "settings.skills_external_tools_plugins_automation",
  display: "settings.what_the_conversation_shows_and",
  controls: "settings.shortcuts_tabs_sending_while_running",
  appearance: "settings.light_or_dark_colours_fonts",
  storage: "settings.memory_usage_storage_backup",
  network: "settings.network_remote_machines_phone",
  account: "settings.account_version_logs_community",
};

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
  /** A child region indexed under its containing block. */
  within?: string;
}

// One row per block the settings screen renders, checked both ways against
// what it really renders — a block with no row here fails, and a row nothing
// renders fails too.
export const SETTINGS: SettingEntry[] = [
  // The kernel still accepts legacy preset values for old clients and saved
  // sessions, but Studio presents one adaptive completion standard. Asking
  // people to predict the amount of verification before writing the task made
  // an internal policy look like a required work mode.
  { section: "common", anchor: "default-mode", title: "safety.how_new_conversations_start", scope: "machine", apply: "on-open", keywords: ["plan", "默认模式", "协作方式", "default_mode", "新会话"] },
  // Written to the user file only, and read by the running turn at its next round.
  { section: "session", anchor: "ask-timeout", title: "safety.question_wait_time", scope: "machine", apply: "immediate", keywords: ["ask", "跳过", "无人回答", "长任务", "超时", "自动继续"] },
  { section: "session", anchor: "session-ref", title: "composer.size_of_a_referenced_conversation", scope: "machine", apply: "immediate", keywords: ["引用", "历史会话", "过往会话", "上下文", "长度", "token", "session"] },
  { section: "ext", anchor: "mods", title: "Claude Code mod", scope: "machine", apply: "on-open", hidden: !MODS_SHOWN, keywords: ["mod", "钩子", "hook", "Claude Code", "完全信任", "插件", "扩展"] },
  { section: "session", anchor: "progress-watch", title: "panels.long_stretches_without_progress", scope: "machine", apply: "immediate", keywords: ["暂停", "无进展", "空转", "卡住", "循环", "轮次", "token"] },
  { section: "display", anchor: "changed-files", title: "settings.file_changes", scope: "machine", apply: "immediate", keywords: ["改动", "变更", "文件", "已更改", "药丸", "输入框"] },
  { section: "display", anchor: "feedback-level-notices", title: "feedback.level_notices", scope: "machine", apply: "immediate", keywords: ["等级", "晋级", "升级", "反馈", "提示"] },
  { section: "session", anchor: "turn-narration", title: "settings.narration_between_tool_calls", scope: "machine", apply: "immediate", keywords: ["叙述", "总结", "小轮", "工具之间", "详细", "简短", "思路", "监控", "narration"] },
  { section: "display", anchor: "fold-work", title: "settings.fold_the_work_in_between", scope: "machine", apply: "immediate", keywords: ["折叠", "中间过程", "工具调用", "思考", "最后一句", "收起", "摘要"] },
  // Written to the user file only; the kernel's background sweep reads it on each pass.
  { section: "storage", anchor: "auto-archive", title: "data.auto_archive_inactive_conversations", scope: "machine", apply: "immediate", keywords: ["归档", "过期", "清理", "闲置", "天数"] },
  { section: "persona", anchor: "composition-picker", within: "agent-presets", title: "persona.show_mode_picker", scope: "machine", apply: "immediate", keywords: ["预设", "模式", "picker", "Agent"] },
  // Explicit selections bind immutable versions; catalog saves do not rebuild.
  { section: "persona", anchor: "agent-presets", title: "persona.agent_presets", scope: "machine", apply: "on-open", keywords: ["预设", "组合", "Agent"] },
  { section: "persona", anchor: "persona", within: "agent-presets", title: "persona.system_persona", scope: "machine", apply: "next-rebuild", keywords: ["人设", "persona", "身份", "提示词", "系统提示词", "前缀", "后缀"] },
  { section: "storage", anchor: "session-dir", title: "session.where_this_session_writes", scope: "workspace", apply: "none", keywords: ["工作目录", "路径"] },

  { section: "model", anchor: "model", title: "settings.models_by_use", scope: "machine", apply: "runtime-rebuild", keywords: ["主模型", "默认模型", "模型", "切换", "端点", "按任务指定模型", "角色分工", "子代理", "规划", "看图", "复核", "决策", "网页搜索", "web search", "system one", "typesafe", "laya", "自动命名", "思考强度", "推理", "reasoning", "档位"] },
  // Machine-wide despite sitting on the model page, and the clearest case for
  // not reading scope off a section: the economic threshold is one
  // [agent] key in the user config that every session on this computer reads.
  { section: "model", anchor: "context", title: "settings.context_maintenance", scope: "machine", apply: "runtime-rebuild", keywords: ["上下文窗口", "压缩", "compaction"] },
  // Adding a source does not rebuild; changing which protocol a source is
  // reached through switches the model, and that does. The stronger of the two
  // is what the row promises, because the weaker one would be a promise this
  // block cannot keep.
  { section: "providers", anchor: "providers", title: "common.model_services", scope: "machine", apply: "runtime-rebuild", keywords: ["模型来源", "连接", "提供商", "供应商", "api key", "密钥", "协议", "地址"] },
  { section: "providers", anchor: "provider-health", title: "models.automatically_check_connections_and_balances", scope: "machine", apply: "immediate", keywords: ["检测", "余额", "连接", "health", "balance"] },

  // Takes effect on this session at once and is also persisted as the default
  // every later session starts from. Machine is the wider of the two answers
  // and the one a reader cannot see from the screen.
  { section: "tools", anchor: "approval", title: "settings.tool_approvals", scope: "machine", apply: "immediate", keywords: ["权限", "yolo", "询问", "放行", "每次都问", "免确认", "自动同意", "确认", "不要问"] },
  { section: "display", anchor: "present-files", title: "settings.ai_file_delivery_cards", scope: "machine", apply: "on-open", keywords: ["文件交付", "交付卡片", "文件预览", "present_files", "tools.present_files", "PDF", "CSV", "HTML", "file delivery", "preview"] },
  { section: "tools", anchor: "external-origin", title: "settings.outside_text", scope: "machine", apply: "runtime-rebuild", keywords: ["外部", "网页", "来源", "注入", "提示注入", "origin", "yolo", "auto", "询问"] },
  { section: "session", anchor: "plan-approval", title: "settings.plan_approval", scope: "machine", apply: "immediate", keywords: ["plan", "自动批准", "长任务", "无人值守", "plan_approval", "放行", "询问", "每次都问", "免确认", "自动同意"] },
  { section: "tools", anchor: "tool-list", title: "ext.tools_the_model_can_use", scope: "machine", apply: "on-open", keywords: ["禁用", "关闭工具", "tools.disabled", "省 token", "提示词开销", "web_fetch", "denylist"] },
  { section: "tools", anchor: "rules", title: "settings.rules_you_spell_out", scope: "machine", apply: "runtime-rebuild", keywords: ["permissions", "允许", "拒绝", "配方"] },
  { section: "tools", anchor: "sandbox", title: "settings.sandbox", scope: "machine", apply: "runtime-rebuild", keywords: ["隔离", "联网", "写权限", "ssh-agent"] },
  { section: "tools", anchor: "shell", title: "common.which_program_runs_commands", scope: "machine", apply: "runtime-rebuild", keywords: ["bash", "powershell", "解释器"] },
  // The tools are bound while a runtime is assembled, and the save rebuilds this
  // one. The shared [browser] enabled key belongs to the 1.x command line.
  // The lease is built with the session and a rebuild adopts the one it holds, so
  // the save reaches only sessions opened afterwards; the handler rebuilds nothing.
  { section: "peers", anchor: "peers", title: "remote.messages_between_sessions_experimental", scope: "machine", apply: "next-turn", keywords: ["peer", "会话间", "消息", "发送", "批准", "挂起", "另一个会话"] },
  { section: "peers", anchor: "peer-projects", within: "peers", title: "remote.projects_allowed_through", scope: "machine", apply: "next-turn", keywords: ["放行", "项目", "不再询问", "长期", "撤销", "peer", "release", "会话间"] },
  { section: "tools", anchor: "write-lease", title: "session.several_sessions_writing_one_project", scope: "machine", apply: "on-open", keywords: ["lease", "写锁", "并行", "多会话", "乐观", "optimistic", "write_lease", "等待"] },
  { section: "session", anchor: "subagent-claims", title: "session.sub_agent_write_scope", scope: "machine", apply: "runtime-rebuild", keywords: ["subagent", "子代理", "并发", "write_paths", "乐观", "optimistic", "写锁"] },
  { section: "account", anchor: "trace-export", title: "data.trace_export", scope: "machine", apply: "on-open", keywords: ["追踪", "opentelemetry", "otlp", "观测", "遥测", "trace", "collector", "收集器"] },
  { section: "tools", anchor: "web-access", title: "models.web_search_and_fetch", scope: "machine", apply: "on-open", keywords: ["搜索", "searxng", "deepseek", "抓取", "计费", "付费", "web search", "fetch"] },
  { section: "tools", anchor: "browser-tools", title: "common.built_in_browser", scope: "machine", apply: "runtime-rebuild", keywords: ["browser", "网页", "chrome", "浏览器工具"] },
  // The four blocks below carry a scope control of their own, and the kernel
  // takes that choice as a parameter — hook.Scope, McpInstallScope, the skill
  // activation scope, a memory's project/global flag. Naming one authority here
  // would be wrong whenever the reader picks the other.
  { section: "session", anchor: "scheduled-runs", title: "panels.scheduled_runs", scope: "machine", apply: "immediate", keywords: ["定时", "计划", "任务", "每天", "schedule", "cron", "unattended", "无人值守"] },
  { section: "session", anchor: "hooks", title: "settings.hooks", scope: "chosen", apply: "immediate", keywords: ["钩子", "hook", "触发"] },

  { section: "ext", anchor: "market", title: "ext.community_market", scope: "machine", apply: "immediate", keywords: ["市场", "market", "社区", "发现", "下载", "发布", "publish", "主题"] },
  { section: "ext", anchor: "plugins", title: "settings.plugin_packages", scope: "machine", apply: "immediate", keywords: ["安装"] },
  { section: "ext", anchor: "mcp", title: "ext.external_services", scope: "chosen", apply: "immediate", keywords: ["mcp", "服务器", "连接外部"] },
  { section: "ext", anchor: "skills", title: "settings.skills", scope: "chosen", apply: "immediate", keywords: ["skill", "技能包"] },

  { section: "network", anchor: "network", title: "common.network", scope: "machine", apply: "immediate", keywords: ["代理", "proxy", "抓取", "超时"] },
  { section: "remote", anchor: "remote", title: "settings.remote", scope: "machine", apply: "immediate", keywords: ["ssh", "机器", "远端工作区"] },
  // Machine rather than session: the door is this window's, and a phone that
  // walks through it reaches every pane. Nothing is written, so nothing outlives
  // the window either.
  { section: "remote", anchor: "phone", title: "remote.phone_access", scope: "machine", apply: "immediate", keywords: ["手机", "扫码", "二维码", "局域网", "配对", "phone", "mobile", "qr"] },
  { section: "remote", anchor: "phone-notify", within: "phone", title: "remote.phone_notifications", scope: "machine", apply: "immediate", keywords: ["推送", "提醒", "手机", "批准", "push", "notification", "phone"] },
  { section: "remote", anchor: "phone-device", within: "phone", title: "remote.this_phones_reminders", scope: "machine", apply: "immediate", keywords: ["推送", "提醒", "手机", "push", "notification"] },
  { section: "account", anchor: "account", title: "settings.account", scope: "account", apply: "immediate", keywords: ["登录", "社区"] },
  // Shown only while signed in; restoring writes through the ordinary config paths and reloads the runtime.
  { section: "storage", anchor: "backup-file", title: "data.export_and_import_config", scope: "machine", apply: "immediate", keywords: ["备份", "导出", "导入", "换机", "迁移", "文件"] },
  { section: "storage", anchor: "backup", title: "data.cloud_backup", scope: "account", apply: "immediate", keywords: ["备份", "恢复", "换机", "同步", "导出"] },
  { section: "versions", anchor: "versions", title: "common.version", scope: "machine", apply: "immediate", keywords: ["更新", "升级"] },
  { section: "common", anchor: "update-mode", title: "about.update_mode", scope: "machine", apply: "immediate", keywords: ["自动更新", "检查更新", "更新提醒", "后台", "update", "auto update", "notify", "自动下载", "download"] },
  { section: "versions", anchor: "logs", title: "about.logs_and_abnormal_exit_records", scope: "machine", apply: "none", keywords: ["日志", "崩溃", "闪退", "异常退出", "log", "logs", "crash", "路径"] },
  { section: "versions", anchor: "community", title: "common.community_and_contributors", scope: "machine", apply: "none", keywords: ["社区", "交流群", "QQ", "加群", "二维码", "discord", "贡献者", "contributors", "community"] },
  { section: "versions", anchor: "sponsor", title: "about.support_the_project", scope: "machine", apply: "none", keywords: ["捐助", "支付宝", "微信支付", "收款码", "paypal", "donate", "sponsor", "support", "alipay", "wechat"] },
  { section: "memory", anchor: "memory", title: "data.memory", scope: "chosen", apply: "immediate", keywords: ["记住", "忘记", "事实"] },
  { section: "usage", anchor: "currency", title: "usage.cost_display_currency", scope: "machine", apply: "immediate", keywords: ["币种", "货币", "人民币", "美元", "currency", "CNY", "USD", "汇率"] },
  { section: "usage", anchor: "upload-usage", title: "usage.upload_volume", scope: "machine", apply: "none", keywords: ["流量", "上传", "带宽", "请求", "upload", "traffic", "bandwidth"] },
  { section: "usage", anchor: "usage", title: "usage.usage_cost", scope: "machine", apply: "none", keywords: ["token", "花费", "缓存命中", "多少钱", "花钱", "费用", "成本", "账单"] },
  { section: "storage", anchor: "storage", title: "common.storage", scope: "machine", apply: "restart", keywords: ["搬家", "迁移", "磁盘", "位置"] },
  { section: "advanced", anchor: "elsewhere", title: "settings.not_in_this_version_yet", scope: "machine", apply: "none", keywords: ["配置文件"] },

  { section: "common", anchor: "language", title: "common.language", scope: "machine", apply: "restart", keywords: ["中文", "english", "界面语言", "英文", "英语"] },
  { section: "appearance", anchor: "graphics", title: "look.graphics", scope: "machine", apply: "restart", keywords: ["硬件加速", "显卡", "gpu", "软件渲染", "占用", "卡顿"] },
  { section: "controls", anchor: "navrail", title: "settings.icon_rail", scope: "machine", apply: "immediate", keywords: ["侧边栏", "导航", "图标", "sidebar", "nav"] },
  { section: "controls", anchor: "naventries", title: "settings.navigation_entries", scope: "machine", apply: "immediate", keywords: ["侧边栏", "导航", "隐藏", "排序", "自定义", "入口", "sidebar", "nav", "hide", "reorder", "customize"] },
  { section: "controls", anchor: "session-quick-actions", title: "session.session_row_buttons", scope: "machine", apply: "immediate", keywords: ["会话", "归档", "删除", "快捷", "按钮", "hover", "archive", "delete", "session", "row", "sidebar"] },
  { section: "display", anchor: "new-answer-marker", title: "settings.new_answer_divider", scope: "machine", apply: "immediate", keywords: ["未读", "新回答", "分隔线", "分割线", "unread", "new", "divider", "marker", "transcript"] },
  { section: "controls", anchor: "open-session-limit", title: "settings.session_tabs_kept_open", scope: "machine", apply: "immediate", keywords: ["标签", "窗格", "收起", "保留", "tabs", "panes", "warm", "内存", "memory"] },
  { section: "controls", anchor: "live-stream-budget", title: "settings.session_tabs_kept_live", scope: "machine", apply: "immediate", keywords: ["连接", "实时", "标签", "窗格", "卡死", "停止", "connection", "stream", "tabs", "stuck", "busy"] },
  { section: "storage", anchor: "legacy-sessions", title: "data.sessions_from_1_x", scope: "machine", apply: "immediate", keywords: ["1.x", "旧会话", "迁移", "隐藏", "legacy", "sessions", "sidebar"] },
  { section: "controls", anchor: "live-grace", title: "settings.keep_finished_sessions_in_running", scope: "machine", apply: "immediate", keywords: ["进行中", "完成", "保留", "过滤", "running", "grace", "sidebar"] },
  { section: "common", anchor: "send-shortcut", title: "composer.send_shortcut", scope: "machine", apply: "immediate", keywords: ["发送", "回车", "换行", "enter", "ctrl", "cmd"] },
  { section: "controls", anchor: "running-message-intent", title: "settings.messages_sent_while_running", scope: "machine", apply: "immediate", keywords: ["插话", "排队", "下一轮", "steer", "queue", "followup", "running"] },
  { section: "controls", anchor: "global-shortcut", title: "settings.global_show_hide_shortcut", scope: "machine", apply: "immediate", keywords: ["global", "show", "hide", "shortcut", "keyboard", "全局", "显示", "隐藏"] },
  { section: "controls", anchor: "keys", title: "settings.keyboard_shortcuts", scope: "machine", apply: "immediate", keywords: ["shortcut", "keyboard", "ctrl", "cmd", "esc", "home", "查找", "搜索", "运行分析", "工作台"] },
  { section: "controls", anchor: "esc-stop", title: "safety.esc_stops_a_task", scope: "machine", apply: "immediate", keywords: ["esc", "停止", "中断", "确认", "误触", "stop"] },
  { section: "controls", anchor: "page-sleep", title: "settings.idle_page_sleep", scope: "machine", apply: "immediate", keywords: ["休眠", "睡眠", "空闲", "内存", "收起", "browser", "page", "sleep", "memory"] },
  { section: "controls", anchor: "session-tabs", title: "session.session_tabs", scope: "machine", apply: "immediate", keywords: ["标签", "会话切换", "tabs", "session"] },
  { section: "display", anchor: "folding", title: "chat.transcript_folding", scope: "machine", apply: "immediate", keywords: ["展开", "收起", "思考", "执行过程", "步骤", "输出", "简报", "fold"] },
  { section: "display", anchor: "plan-progress", title: "plan.show_collapsed_plan_progress", scope: "machine", apply: "immediate", keywords: ["计划", "进度条", "待办", "收起", "todo", "plan", "progress", "collapsed"] },
  { section: "controls", anchor: "composer-hint", title: "composer.composer_hint", scope: "machine", apply: "immediate", keywords: ["占位", "任务", "提示", "输入框", "placeholder", "composer", "hint"] },
  { section: "display", anchor: "office-preview", title: "common.office_document_preview", scope: "machine", apply: "immediate", keywords: ["office", "word", "excel", "docx", "xlsx", "工作台", "预览", "文档", "表格"] },
  { section: "appearance", anchor: "window", title: "common.window", scope: "machine", apply: "immediate", keywords: ["托盘", "关闭行为", "休眠", "睡眠"] },
  // Immediate because the endpoint hands the holder every running sink reads,
  // not because the file was written: writing it is what a restart would need.
  { section: "common", anchor: "notify", title: "common.notifications", scope: "machine", apply: "immediate", keywords: ["提醒", "notification", "系统通知", "结束", "批准", "提问", "提示音", "声音", "静音", "铃声", "sound"] },
  { section: "appearance", anchor: "size", title: "look.size", scope: "machine", apply: "immediate", keywords: ["缩放", "字号", "行宽", "对话区域宽度", "比例", "width", "percentage"] },
  { section: "appearance", anchor: "font", title: "look.typeface", scope: "machine", apply: "immediate", keywords: ["等宽", "mono"] },
  { section: "appearance", anchor: "wallpaper", title: "look.wallpaper", scope: "machine", apply: "immediate", keywords: ["背景", "图片"] },
  { section: "appearance", anchor: "weight", title: "look.text_weight", scope: "machine", apply: "immediate", keywords: ["加粗", "字重"] },
  { section: "appearance", anchor: "contrast", title: "look.text_contrast", scope: "machine", apply: "immediate", keywords: ["柔和", "对比"] },
  { section: "appearance", anchor: "effects", title: "look.visual_effects", scope: "machine", apply: "immediate", keywords: ["磨砂", "模糊", "动画", "显卡", "gpu", "性能", "省电", "blur"] },
  { section: "common", anchor: "mode", title: "settings.light_and_dark", scope: "machine", apply: "immediate", keywords: ["深色", "浅色", "跟随系统", "定时", "时间段", "schedule", "dark", "light", "夜间", "夜晚", "暗色", "亮色", "黑色模式", "夜间模式", "深色模式", "浅色模式", "暗色模式"] },
  { section: "appearance", anchor: "scheme", title: "settings.palette", scope: "machine", apply: "immediate", keywords: ["主题", "theme", "色板", "高级外观", "导入主题", "自定义颜色", "colours"] },
];

export const SETTING_AT = (anchor: string) => SETTINGS.find((s) => s.anchor === anchor);

export const FOLDS_INTO: Partial<Record<Section, Section>> = { providers: "model", remote: "network", memory: "storage", persona: "session", hooks: "ext", peers: "session", usage: "storage", versions: "account" };

export const pageHome = (section: Section): Section => FOLDS_INTO[section] ?? section;

export const LOOK_FAMILY: readonly Section[] = ["appearance", "display", "controls"];

export const SECTION_NAME: Partial<Record<Section, string>> = {
  ...(Object.fromEntries(NAV.flatMap(([, items]) => items)) as Partial<Record<Section, string>>),
  providers: "common.model_services",
  remote: "settings.remote",
  hooks: "settings.automation",
  persona: "settings.persona",
  peers: "settings.session_messages",
  memory: "data.memory",
  usage: "app.usage",
  versions: "common.version",
};

// Title, aliases, and the page it is on. An alias is a way in and nothing
// more: it never becomes the setting's name and no judgement reads it.
export function settingMatches(e: SettingEntry, q: string): boolean {
  return rankTerms(termsOf(e), q) > 0;
}

/** Every name a setting can be found by, in both the key's wording and the
 *  reader's language. */
export function termsOf(e: SettingEntry): SettingTerms {
  const names = (key: string | undefined) => (key ? [key, t(key)] : []);
  return {
    titles: names(e.title),
    sections: [...names(SECTION_NAME[e.section]), ...names(SECTION_NAME[pageHome(e.section)])],
    keywords: e.keywords ?? [],
  };
}
