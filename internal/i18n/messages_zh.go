package i18n

// Chinese is the zh-Hans catalogue. Keep the %s placeholders in the same order
// as English unless a phrase genuinely demands re-ordering — call sites pass
// arguments positionally and won't reshuffle.
var Chinese = Messages{
	Subtitle:        "配置与插件驱动的 coding agent",
	WelcomeTitleFmt: "欢迎使用 %s",
	NoConfigYet:     "还没有配置 — 现在来设置一下吧。",
	StartingChatFmt: "正在启动 %s…",
	SetKeyHint:      "设置好 API key 后运行 `reasonix chat`。",
	ConfigLabel:     "配置",
	ModelsLabel:     "模型",
	ConfigNotFound:  "未找到 — 使用内置默认值",
	ConfigErrorFmt:  "%s — 错误：%v",
	NoKey:           "未设置 key",
	Ready:           "已就绪",
	GetStarted:      "开始使用",
	StepScaffold:    "生成 reasonix.toml",
	StepSetKey:      "设置 API key",

	InitHint:       "项目记忆（AGENTS.md）在会话内由模型生成：运行 `reasonix chat`，然后 `/init` —— 模型会分析代码库并写入。配置请用 `reasonix setup`。",
	StepSetKeyHint: "执行 export DEEPSEEK_API_KEY=… 或写入 .env",
	StepChatDesc:   "交互式会话",
	StepRunDesc:    "执行单次任务",
	HelpFooter:     "reasonix help · 查看全部命令",

	ChatTip:           "对话上下文将跨轮保留。输入 'exit' 或按 Ctrl-D 退出。",
	TurnCancelled:     "已取消 — 回到提示符",
	NoSessionToResume: "没有可恢复的会话 — 用 `reasonix chat` 开一个新的",
	ResumeRequiresTTY: "--resume 需要交互式终端；用 --continue 直接恢复最近一次",
	PickSessionLabel:  "恢复哪个会话？",

	ChatStatusThinkingFmt:  "%s 思考中… (%d 秒 · Esc 取消)",
	ChatStatusIdle:         "Tab 切换 plan · Enter 发送 · Esc 退出当前状态 · PgUp/PgDn 滚动 · Ctrl-D 退出",
	ChatStatusPlanApproval: "Enter/y 批准并执行 · n/Esc 继续规划 · PgUp/PgDn 滚动",
	PlanApprovalPrompt:     "计划已生成（见上方）— Enter/y 批准执行,n/Esc 继续规划",
	ChatStatusToolApproval: "y 同意一次 · a 本会话允许 · n 拒绝 · Ctrl-C 取消本轮",
	AskTypeSomething:       "其它(自己输入)",
	AskTypingHint:          "在下方输入框输入,回车确认",
	AskChatInstead:         "都不选,直接聊聊",
	ChatStatusQuestion:     "↑/↓ 选 · 数字快选 · 空格多选 · Enter 确认 · ←/→ 切换问题 · Esc 取消",
	ToolApprovalPromptFmt:  "允许 %s%s？— [y] 本次 · [a] 本会话 · [n] 拒绝",

	OutputStyleNone:   "没有可用的输出风格",
	OutputStyleHeader: "输出风格：",
	OutputStyleHint:   "在 reasonix.toml 设置 agent.output_style 即可启用（下次会话生效）",

	CompactionWorking: "正在压缩对话…",
	CompactionTitle:   "上下文已压缩",
	CompactionUnit:    "条消息",
	CompactionAuto:    "自动",
	CompactionManual:  "手动",

	SlashCompactDone:   "已压缩 — 旧的中段换成一段摘要，最近几轮保留原样",
	SlashCompactFailed: "压缩失败",
	SlashNewDone:       "已新建会话 — 之前的对话已存档",
	SlashNewFailed:     "新建会话失败",
	SlashUnavailable:   "当前构建不支持该命令",
	SlashUnknown:       "未知命令",
	SlashTodoCleared:   "已清除任务清单",
	SlashHelp:          "命令：/compact · /new · /todo · /model（切换模型）· /mcp · /skill · /hooks · /memory · /help · 以及 skills（/init、/explore …）",
	SlashPromptEmpty:   "该 MCP prompt 没有返回可发送的内容",
	SlashMCPNone:       "没有配置 MCP 服务器 — 在 reasonix.toml 加一个 [[plugins]] 条目",
	CompHintSlash:      "↑/↓ 移动 · Tab/Enter 选中 · Esc 关闭",
	CompHintFile:       "↑/↓ 移动 · Tab/Enter 进入文件夹或选中文件 · Esc 关闭",

	CmdNew:          "开启新会话",
	CmdCompact:      "压缩上下文",
	CmdRewind:       "回滚到更早的一轮",
	CmdModel:        "切换模型",
	CmdMemory:       "查看记忆文件",
	CmdMcp:          "MCP 服务器",
	CmdHooks:        "管理 hooks",
	CmdOutputStyle:  "列出输出风格",
	CmdSkill:        "管理 skills",
	CmdHelp:         "查看命令列表",
	CmdTodo:         "清除任务清单",
	ArgSkillList:    "列出 skills",
	ArgSkillShow:    "查看 skill 内容",
	ArgSkillNew:     "新建一个 skill",
	ArgSkillPaths:   "显示发现路径",
	ArgMcpAdd:       "连接一个服务器",
	ArgMcpRemove:    "断开一个服务器",
	ArgMcpList:      "显示已配置的服务器",
	ArgMcpConnected: "已连接",
	ArgHooksList:    "列出生效的 hooks",
	ArgHooksTrust:   "信任本项目的 hooks",
	ArgModelCurrent: "当前",

	ListModelsHeaderFmt: "模型（当前：%s）",
	ListModelsHint:      "用底部的模型切换器，或输入 /model <provider/model>",
	ListMemoryHeader:    "记忆文件",
	ListMemoryNone:      "暂无记忆 — 用 “#<内容>” 添加，或运行 /init 生成 AGENTS.md",
	ListSkillsHeaderFmt: "skills（%d 个）",
	ListSkillsNone:      "暂无 skill — 调用内置的（如 /init），或用 install_skill 创建一个",
	ListHooksHeaderFmt:  "hooks（生效 %d 个）",
	ListHooksNone:       "无生效 hooks — 在 .reasonix/settings.json（项目，需信任后）或 ~/.reasonix/settings.json（全局）配置",
	ListMcpHeader:       "MCP 服务器",
	ListMcpNone:         "未连接 MCP 服务器 — 在 reasonix.toml（[[plugins]]）或项目 .mcp.json 中添加",

	SelectProvidersLabel:  "选择要启用的 provider",
	EnterAPIKeysHeader:    "输入 API key（回车跳过、稍后写入 .env）：",
	MissingKeyIntro:       "reasonix.toml 已配置好 — 只差一个 API key 就可以开始。",
	WroteFileFmt:          "已写入 %s",
	SetupComplete:         "设置完成。",
	SetupCancelled:        "设置已取消。",
	TryHintFmt:            "试试: %s",
	NextHint:              "下一步：设置 API key（export DEEPSEEK_API_KEY=... 或写入 .env），然后运行 `reasonix run \"你的任务\"`。",
	ConfirmReconfigureFmt: "%s 已存在。重新配置并覆盖？",
	KeepingExisting:       "保留原配置不变。",
	NotOverwritingFmt:     "%s 已存在，不覆盖",

	UnknownCommandFmt: "未知命令 %q",
	UsageRunHint:      "用法：reasonix run [--model NAME] <task>",
	ErrorPrefix:       "错误：",
	WriteConfigErr:    "写入配置失败：",
	WriteEnvErr:       "写入 .env 失败：",

	SelectOneHint:  "(↑/↓ · Enter · q 取消)",
	SelectManyHint: "(↑/↓ · Space · Enter · q)",

	UsageBody: `reasonix — 由配置和插件驱动的 coding agent（多模型）

用法：
  reasonix chat [--model NAME]                          交互式会话（多轮）
  reasonix run  [--model NAME] [--max-steps N] <task>   执行单次任务后退出
  reasonix serve [--model NAME] [--addr HOST:PORT]      通过 HTTP+SSE 提供会话（浏览器客户端在 /）
  reasonix setup [path]                                 交互式配置向导；生成 reasonix.toml（及 .env）
  reasonix mcp <add|remove|list>                        管理 reasonix.toml 里的 MCP 服务器
  reasonix version
  reasonix help

示例：
  reasonix chat
  reasonix run "把 main.go 里的 TODO 实现掉"
  reasonix run --model mimo-pro "给这个函数补单元测试"
  echo "解释这段代码" | reasonix run

配置：
  优先级：flag > ./reasonix.toml > ~/.config/reasonix/config.toml > 内置默认值
  密钥通过 api_key_env 从环境变量注入（如 DEEPSEEK_API_KEY）。
  运行 'reasonix setup' 生成配置；详见 docs/SPEC.md。
`,
}
