export type Locale = "en" | "zh" | "zh-TW";

export type MessageKey =
  | "appName"
  | "tab.sessions"
  | "tab.nodes"
  | "tab.providers"
  | "tab.settings"
  | "sessions.title"
  | "sessions.empty"
  | "sessions.emptyTitle"
  | "sessions.new"
  | "sessions.runtimeLocal"
  | "sessions.runtimeRemote"
  | "sessions.pickRuntime"
  | "sessions.pickRuntimeDesc"
  | "sessions.localDesc"
  | "sessions.remoteDesc"
  | "sessions.pickModel"
  | "sessions.statusIdle"
  | "sessions.statusRunning"
  | "sessions.statusPending"
  | "sessions.statusFailed"
  | "sessions.chatEmpty"
  | "sessions.chatEmptyHint"
  | "sessions.selectHint"
  | "sessions.nodeUrl"
  | "sessions.nodeUrlPlaceholder"
  | "sessions.create"
  | "sessions.cancel"
  | "sessions.createError"
  | "sessions.streaming"
  | "nodes.title"
  | "nodes.empty"
  | "nodes.emptyTitle"
  | "nodes.pairHint"
  | "nodes.pair"
  | "nodes.pairSheetDesc"
  | "nodes.pairPaste"
  | "nodes.pairPlaceholder"
  | "nodes.pairConfirm"
  | "nodes.pairing"
  | "nodes.pairInvalid"
  | "nodes.pairNeedInput"
  | "nodes.pairFailed"
  | "nodes.useDemo"
  | "nodes.copyDemo"
  | "nodes.qrDemoCaption"
  | "nodes.qrPairedPreview"
  | "nodes.online"
  | "nodes.offline"
  | "nodes.fingerprint"
  | "nodes.remove"
  | "nodes.useForSession"
  | "providers.title"
  | "providers.empty"
  | "providers.emptyTitle"
  | "providers.add"
  | "settings.title"
  | "settings.theme"
  | "settings.themeDark"
  | "settings.themeLight"
  | "settings.themeSystem"
  | "settings.language"
  | "settings.account"
  | "settings.accountOptional"
  | "settings.sectionAppearance"
  | "settings.sectionGeneral"
  | "settings.platform"
  | "composer.placeholder"
  | "composer.send"
  | "approval.title"
  | "approval.desc"
  | "approval.tool"
  | "approval.target"
  | "approval.reason"
  | "approval.command"
  | "approval.diff"
  | "approval.riskLow"
  | "approval.riskMedium"
  | "approval.riskHigh"
  | "approval.dangerous"
  | "approval.allow"
  | "approval.deny"
  | "approval.holdAllow"
  | "approval.holding"
  | "approval.holdHint"
  | "approval.banner"
  | "approval.review"
  | "common.online"
  | "common.offline"
  | "common.back"
  | "common.close"
  | "common.done"
  | "common.chevron";

type Catalog = Record<MessageKey, string>;

const en: Catalog = {
  appName: "Reasonix",
  "tab.sessions": "Sessions",
  "tab.nodes": "Nodes",
  "tab.providers": "Providers",
  "tab.settings": "Settings",
  "sessions.title": "Sessions",
  "sessions.empty": "Start on this device with a local provider, or hand work to a paired node.",
  "sessions.emptyTitle": "No sessions yet",
  "sessions.new": "New Session",
  "sessions.runtimeLocal": "This Device",
  "sessions.runtimeRemote": "Node",
  "sessions.pickRuntime": "Where should this run?",
  "sessions.pickRuntimeDesc": "Runtime is fixed after creation. Move work later by copy or hand-off.",
  "sessions.localDesc": "Lightweight agent. Keys stay on device. No shell or git.",
  "sessions.remoteDesc": "Full Reasonix on your computer or server — shell, git, MCP, jobs.",
  "sessions.pickModel": "Choose a model",
  "sessions.statusIdle": "Idle",
  "sessions.statusRunning": "Running",
  "sessions.statusPending": "Needs approval",
  "sessions.statusFailed": "Failed",
  "sessions.chatEmpty": "Send a message to start. Tools and diffs appear in a compact timeline.",
  "sessions.chatEmptyHint": "Tip: try “delete tmp/log” to preview the approval sheet.",
  "sessions.selectHint": "Select a session",
  "sessions.nodeUrl": "Node address",
  "sessions.nodeUrlPlaceholder": "http://127.0.0.1:8790",
  "sessions.create": "Create",
  "sessions.cancel": "Cancel",
  "sessions.createError": "Could not create session",
  "sessions.streaming": "Streaming",
  "nodes.title": "Nodes",
  "nodes.empty": "Scan or paste a pairing code from `reasonix node`, or enter a LAN / Tailscale address.",
  "nodes.emptyTitle": "No paired nodes",
  "nodes.pairHint": "Prefer verified LAN or Tailscale. Official relay is the fallback.",
  "nodes.pair": "Pair node",
  "nodes.pairSheetDesc": "Paste a QR payload or URL. Fingerprint is fixed at pairing time.",
  "nodes.pairPaste": "Pairing payload",
  "nodes.pairPlaceholder": "reasonix://node-pair?v=1&url=…  or  http://192.168.x.x:8790",
  "nodes.pairConfirm": "Pair & verify",
  "nodes.pairing": "Verifying…",
  "nodes.pairInvalid": "Could not parse pairing payload",
  "nodes.pairNeedInput": "Paste a pairing code or URL first",
  "nodes.pairFailed": "Pairing failed",
  "nodes.useDemo": "Use local demo",
  "nodes.copyDemo": "Copy demo QR text",
  "nodes.qrDemoCaption": "Demo QR for local node (127.0.0.1:8790)",
  "nodes.qrPairedPreview": "Pairing identity for this node",
  "nodes.online": "Reachable",
  "nodes.offline": "Unreachable",
  "nodes.fingerprint": "Fingerprint",
  "nodes.remove": "Remove",
  "nodes.useForSession": "New remote session",
  "providers.title": "Providers",
  "providers.empty": "OpenAI-compatible or Anthropic-compatible endpoints. Keys use secure storage.",
  "providers.emptyTitle": "No providers",
  "providers.add": "Add provider",
  "settings.title": "Settings",
  "settings.theme": "Appearance",
  "settings.themeDark": "Dark",
  "settings.themeLight": "Light",
  "settings.themeSystem": "System",
  "settings.language": "Language",
  "settings.account": "Account",
  "settings.accountOptional": "Optional for local and LAN. Required for relay and push.",
  "settings.sectionAppearance": "Appearance",
  "settings.sectionGeneral": "General",
  "settings.platform": "UI chrome",
  "composer.placeholder": "Message",
  "composer.send": "Send",
  "approval.title": "Approval required",
  "approval.desc": "Review the tool, target, and risk before continuing.",
  "approval.tool": "Tool",
  "approval.target": "Target",
  "approval.reason": "Why",
  "approval.command": "Command",
  "approval.diff": "Diff",
  "approval.riskLow": "Low risk",
  "approval.riskMedium": "Medium risk",
  "approval.riskHigh": "High risk",
  "approval.dangerous": "Write",
  "approval.allow": "Allow",
  "approval.deny": "Deny",
  "approval.holdAllow": "Hold to allow",
  "approval.holding": "Keep holding…",
  "approval.holdHint": "Dangerous writes require a long-press. Deny is always one tap.",
  "approval.banner": "Waiting for your approval",
  "approval.review": "Review",
  "common.online": "Online",
  "common.offline": "Offline",
  "common.back": "Back",
  "common.close": "Close",
  "common.done": "Done",
  "common.chevron": "Details",
};

const zh: Catalog = {
  appName: "Reasonix",
  "tab.sessions": "会话",
  "tab.nodes": "节点",
  "tab.providers": "供应商",
  "tab.settings": "设置",
  "sessions.title": "会话",
  "sessions.empty": "可在本机用本地供应商开始，或交给已配对的节点执行完整任务。",
  "sessions.emptyTitle": "还没有会话",
  "sessions.new": "新建会话",
  "sessions.runtimeLocal": "本机",
  "sessions.runtimeRemote": "节点",
  "sessions.pickRuntime": "在哪里运行？",
  "sessions.pickRuntimeDesc": "运行位置创建后不可更改。迁移请使用复制或交给节点。",
  "sessions.localDesc": "轻量 Agent。密钥仅存本机。不含 Shell / Git。",
  "sessions.remoteDesc": "连接电脑或服务器上的完整 Reasonix：Shell、Git、MCP、后台任务。",
  "sessions.pickModel": "选择模型",
  "sessions.statusIdle": "空闲",
  "sessions.statusRunning": "运行中",
  "sessions.statusPending": "待审批",
  "sessions.statusFailed": "失败",
  "sessions.chatEmpty": "发送消息开始对话。工具与 Diff 以紧凑时间线展示。",
  "sessions.chatEmptyHint": "提示：发送「删除 tmp/log」可预览审批面板。",
  "sessions.selectHint": "选择一个会话",
  "sessions.nodeUrl": "节点地址",
  "sessions.nodeUrlPlaceholder": "http://127.0.0.1:8790",
  "sessions.create": "创建",
  "sessions.cancel": "取消",
  "sessions.createError": "无法创建会话",
  "sessions.streaming": "生成中",
  "nodes.title": "节点",
  "nodes.empty": "扫描或粘贴 `reasonix node` 的配对码，或输入局域网 / Tailscale 地址。",
  "nodes.emptyTitle": "尚未配对节点",
  "nodes.pairHint": "优先已验证的局域网或 Tailscale，官方中继作为回退。",
  "nodes.pair": "配对节点",
  "nodes.pairSheetDesc": "粘贴二维码内容或 URL。身份指纹在配对时固定。",
  "nodes.pairPaste": "配对载荷",
  "nodes.pairPlaceholder": "reasonix://node-pair?v=1&url=…  或  http://192.168.x.x:8790",
  "nodes.pairConfirm": "配对并验证",
  "nodes.pairing": "验证中…",
  "nodes.pairInvalid": "无法解析配对内容",
  "nodes.pairNeedInput": "请先粘贴配对码或地址",
  "nodes.pairFailed": "配对失败",
  "nodes.useDemo": "使用本机演示",
  "nodes.copyDemo": "复制演示二维码文本",
  "nodes.qrDemoCaption": "本机节点演示二维码（127.0.0.1:8790）",
  "nodes.qrPairedPreview": "此节点的配对身份",
  "nodes.online": "可达",
  "nodes.offline": "不可达",
  "nodes.fingerprint": "指纹",
  "nodes.remove": "移除",
  "nodes.useForSession": "新建远程会话",
  "providers.title": "供应商",
  "providers.empty": "OpenAI-compatible 或 Anthropic-compatible 端点。密钥使用安全存储。",
  "providers.emptyTitle": "尚未添加供应商",
  "providers.add": "添加供应商",
  "settings.title": "设置",
  "settings.theme": "外观",
  "settings.themeDark": "深色",
  "settings.themeLight": "浅色",
  "settings.themeSystem": "系统",
  "settings.language": "语言",
  "settings.account": "账号",
  "settings.accountOptional": "本地与局域网可选。中继与推送需要 Reasonix 账号。",
  "settings.sectionAppearance": "外观",
  "settings.sectionGeneral": "通用",
  "settings.platform": "界面风格",
  "composer.placeholder": "消息",
  "composer.send": "发送",
  "approval.title": "需要审批",
  "approval.desc": "继续前请确认工具、目标与风险等级。",
  "approval.tool": "工具",
  "approval.target": "目标",
  "approval.reason": "原因",
  "approval.command": "命令",
  "approval.diff": "Diff",
  "approval.riskLow": "低风险",
  "approval.riskMedium": "中风险",
  "approval.riskHigh": "高风险",
  "approval.dangerous": "写入",
  "approval.allow": "允许",
  "approval.deny": "拒绝",
  "approval.holdAllow": "长按以允许",
  "approval.holding": "继续按住…",
  "approval.holdHint": "危险写操作需长按确认；拒绝始终单击即可。",
  "approval.banner": "等待你的审批",
  "approval.review": "查看",
  "common.online": "在线",
  "common.offline": "离线",
  "common.back": "返回",
  "common.close": "关闭",
  "common.done": "完成",
  "common.chevron": "详情",
};

const zhTW: Catalog = {
  appName: "Reasonix",
  "tab.sessions": "會話",
  "tab.nodes": "節點",
  "tab.providers": "供應商",
  "tab.settings": "設定",
  "sessions.title": "會話",
  "sessions.empty": "可在本機用本地供應商開始，或交給已配對的節點執行完整任務。",
  "sessions.emptyTitle": "還沒有會話",
  "sessions.new": "新建會話",
  "sessions.runtimeLocal": "本機",
  "sessions.runtimeRemote": "節點",
  "sessions.pickRuntime": "在哪裡執行？",
  "sessions.pickRuntimeDesc": "執行位置建立後不可更改。遷移請使用複製或交給節點。",
  "sessions.localDesc": "輕量 Agent。金鑰僅存本機。不含 Shell / Git。",
  "sessions.remoteDesc": "連線電腦或伺服器上的完整 Reasonix：Shell、Git、MCP、背景工作。",
  "sessions.pickModel": "選擇模型",
  "sessions.statusIdle": "閒置",
  "sessions.statusRunning": "執行中",
  "sessions.statusPending": "待核准",
  "sessions.statusFailed": "失敗",
  "sessions.chatEmpty": "傳送訊息開始對話。工具與 Diff 以緊湊時間線呈現。",
  "sessions.chatEmptyHint": "提示：傳送「刪除 tmp/log」可預覽核准面板。",
  "sessions.selectHint": "選擇一個會話",
  "sessions.nodeUrl": "節點位址",
  "sessions.nodeUrlPlaceholder": "http://127.0.0.1:8790",
  "sessions.create": "建立",
  "sessions.cancel": "取消",
  "sessions.createError": "無法建立會話",
  "sessions.streaming": "產生中",
  "nodes.title": "節點",
  "nodes.empty": "掃描或貼上 `reasonix node` 的配對碼，或輸入區域網路 / Tailscale 位址。",
  "nodes.emptyTitle": "尚未配對節點",
  "nodes.pairHint": "優先已驗證的區域網路或 Tailscale，官方中繼作為後備。",
  "nodes.pair": "配對節點",
  "nodes.pairSheetDesc": "貼上 QR 內容或 URL。身分指紋在配對時固定。",
  "nodes.pairPaste": "配對內容",
  "nodes.pairPlaceholder": "reasonix://node-pair?v=1&url=…  或  http://192.168.x.x:8790",
  "nodes.pairConfirm": "配對並驗證",
  "nodes.pairing": "驗證中…",
  "nodes.pairInvalid": "無法解析配對內容",
  "nodes.pairNeedInput": "請先貼上配對碼或位址",
  "nodes.pairFailed": "配對失敗",
  "nodes.useDemo": "使用本機示範",
  "nodes.copyDemo": "複製示範 QR 文字",
  "nodes.qrDemoCaption": "本機節點示範 QR（127.0.0.1:8790）",
  "nodes.qrPairedPreview": "此節點的配對身分",
  "nodes.online": "可連線",
  "nodes.offline": "無法連線",
  "nodes.fingerprint": "指紋",
  "nodes.remove": "移除",
  "nodes.useForSession": "新建遠端會話",
  "providers.title": "供應商",
  "providers.empty": "OpenAI-compatible 或 Anthropic-compatible 端點。金鑰使用安全儲存。",
  "providers.emptyTitle": "尚未新增供應商",
  "providers.add": "新增供應商",
  "settings.title": "設定",
  "settings.theme": "外觀",
  "settings.themeDark": "深色",
  "settings.themeLight": "淺色",
  "settings.themeSystem": "系統",
  "settings.language": "語言",
  "settings.account": "帳號",
  "settings.accountOptional": "本機與區域網路可選。中繼與推播需要 Reasonix 帳號。",
  "settings.sectionAppearance": "外觀",
  "settings.sectionGeneral": "一般",
  "settings.platform": "介面風格",
  "composer.placeholder": "訊息",
  "composer.send": "傳送",
  "approval.title": "需要核准",
  "approval.desc": "繼續前請確認工具、目標與風險等級。",
  "approval.tool": "工具",
  "approval.target": "目標",
  "approval.reason": "原因",
  "approval.command": "命令",
  "approval.diff": "Diff",
  "approval.riskLow": "低風險",
  "approval.riskMedium": "中風險",
  "approval.riskHigh": "高風險",
  "approval.dangerous": "寫入",
  "approval.allow": "允許",
  "approval.deny": "拒絕",
  "approval.holdAllow": "長按以允許",
  "approval.holding": "繼續按住…",
  "approval.holdHint": "危險寫入需長按確認；拒絕始終一鍵即可。",
  "approval.banner": "等待你的核准",
  "approval.review": "檢視",
  "common.online": "線上",
  "common.offline": "離線",
  "common.back": "返回",
  "common.close": "關閉",
  "common.done": "完成",
  "common.chevron": "詳情",
};

const catalogs: Record<Locale, Catalog> = { en, zh, "zh-TW": zhTW };

export function resolveLocale(raw?: string | null): Locale {
  if (!raw) return "en";
  const v = raw.toLowerCase();
  if (v === "zh-tw" || v === "zh-hant" || v.startsWith("zh-tw")) return "zh-TW";
  if (v === "zh" || v.startsWith("zh-cn") || v.startsWith("zh-hans")) return "zh";
  if (v.startsWith("en")) return "en";
  return "en";
}

export function t(locale: Locale, key: MessageKey): string {
  return catalogs[locale][key] ?? catalogs.en[key] ?? key;
}

export function statusLabel(locale: Locale, status?: string): string {
  switch (status) {
    case "running":
      return t(locale, "sessions.statusRunning");
    case "pending_approval":
      return t(locale, "sessions.statusPending");
    case "failed":
      return t(locale, "sessions.statusFailed");
    default:
      return t(locale, "sessions.statusIdle");
  }
}
