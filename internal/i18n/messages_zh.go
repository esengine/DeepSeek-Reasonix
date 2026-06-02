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

	ResumeListHeader:    "会话（/resume <n> 切换）",
	ResumeBusy:          "请先完成或取消当前这一轮再恢复会话",
	ResumeBadIndexFmt:   "请选择 1–%d 的会话（用 /resume 查看列表）",
	ResumeAlreadyActive: "已在该会话中",
	ResumedTitle:        "已恢复会话",

	ChatThinking:           "思考中…",
	ChatThoughtForFmt:      "思考了 %d 秒",
	ChatStatusThinkingFmt:  "%s 思考中… (%d 秒 · Esc 取消)",
	ChatStatusIdle:         "就绪",
	ChatStatusYoloIdle:     "已跳过批准",
	ChatStatusPlanApproval: "Enter/y 批准并执行 · n/Esc 继续规划 · PgUp/PgDn 滚动",
	PlanApprovalPrompt:     "计划已生成（见上方）— Enter/y 批准执行,n/Esc 继续规划",
	ChatStatusToolApproval: "1 本次允许 · 2 本会话允许 · 3 拒绝 · y/a/n 兼容 · Ctrl-C 取消本轮",
	AskTypeSomething:       "自己输入",
	AskTypingHint:          "输入后按 Enter 确认",
	AskChatInstead:         "先不选择，直接回复",
	ChatStatusQuestion:     "↑/↓ 选 · 数字快选 · 空格多选 · Enter 确认 · ←/→ 切换问题 · Esc 取消",
	AskSubmitTitle:         "提交答案",
	AskUnanswered:          "(未答)",
	AskSubmitHint:          "Enter 提交 · ← 返回修改",
	ToolApprovalPromptFmt:  "需要你的许可\n\n将调用工具 %s%s。\n%s\n1. 本次允许\n2. 本会话允许同类调用\n3. 拒绝\n选择 [1/2/3]（兼容 y/a/n）",
	ToolApprovalSourceFmt:  "来源: %s",
	ToolApprovalBuiltIn:    "内置工具",
	ToolApprovalImageUse:   "将读取提供的图片用于图像理解。",
	DiffFoldedFmt:          "… 还有 %d 行",

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
	SlashHelp:          "命令：/compact · /new · /resume · /rewind · /tree · /branch · /switch · /todo · /verbose · /model（切换模型）· /effort · /mcp · /skill · /hooks · /paste-image · /memory · /quit · /help · 以及 skills（/init、/explore …）",
	SlashPromptEmpty:   "该 MCP prompt 没有返回可发送的内容",
	SlashMCPNone:       "没有配置 MCP 服务器 — 在 reasonix.toml 加一个 [[plugins]] 条目",
	CtrlCQuitHint:      "再按一次 Ctrl+C 退出",
	CompHintSlash:      "↑/↓ 移动 · Tab/Enter 选中 · Esc 关闭",
	CompHintFile:       "↑/↓ 移动 · Tab/Enter 进入文件夹或选中文件 · Esc 关闭",

	CmdNew:          "开启新会话",
	CmdCompact:      "压缩上下文",
	CmdRewind:       "回滚到更早的一轮",
	CmdTree:         "查看对话分支树",
	CmdBranch:       "创建对话分支",
	CmdSwitchBranch: "切换对话分支",
	CmdResume:       "恢复已保存的会话",
	CmdModel:        "切换模型",
	CmdMemory:       "查看记忆文件",
	CmdForget:       "删除一条已存记忆",
	CmdMcp:          "MCP 服务器",
	CmdHooks:        "管理 hooks",
	CmdPasteImage:   "粘贴剪贴板图片",
	CmdOutputStyle:  "列出输出风格",
	CmdSkill:        "管理 skills",
	CmdVerbose:      "切换 thinking 原文显示",
	CmdEffort:       "设置推理强度",
	CmdHelp:         "查看命令列表",
	CmdTodo:         "清除任务清单",
	CmdQuit:         "退出会话",
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
	ArgEffortAuto:   "使用模型默认值",
	ArgEffortLow:    "较轻推理",
	ArgEffortMedium: "均衡推理",
	ArgEffortHigh:   "较深推理",
	ArgEffortXHigh:  "超高推理",
	ArgEffortMax:    "最高推理",

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

	MemoryNone:             "还没有加载任何记忆 — 输入 “#内容” 可快速记录，也可以在项目根目录创建 REASONIX.md",
	MemoryLoaded:           "当前已加载的记忆：",
	MemorySavedHeader:      "  已记录的条目（用 “/forget <name>” 删除）：",
	MemoryStoredUnderFmt:   "  存放于 %s",
	MemoryEditHint:         "可直接编辑记忆文档，或输入 “#内容” 快速记录；文档改动会在下次会话生效",
	ForgetUsage:            "用法：/forget <name> — name 是 /memory 中显示的条目标识",
	ForgetDoneFmt:          "已删除记忆：%s",
	QuickRememberEmpty:     "没有要记录的内容",
	QuickRememberDoneFmt:   "已记住 → %s",
	ModelSwitchUnavailable: "本会话不支持切换模型",
	ModelSwitchBusy:        "请先完成或取消当前这一轮再切换模型",
	ModelAlreadyOnFmt:      "已经在使用 %s",
	ModelSwitchingFmt:      "正在切换到 %s…",
	ModelSwitchedFmt:       "已切换到 %s（会保留当前对话，但提示词缓存会重新计算）",
	ModelListHeader:        "模型（/model <provider/model> 切换）",
	RewindNone:             "暂无可回滚的内容",
	RewindCodeConversation: "代码 + 对话",
	RewindConversationOnly: "仅对话",
	RewindCodeOnly:         "仅代码",
	RewindFork:             "从这里分叉（保留当前代码）",
	RewindSummarizeFrom:    "总结这一轮之后的内容",
	RewindSummarizeUpto:    "总结到这一轮为止",
	RewindPickTitle:        "⟲ 回滚 — 选择一轮",
	RewindPickHint:         "↑/↓ 移动 · Enter 选择 · Esc 关闭",
	RewindRestoreTitleFmt:  "⟲ 恢复到第 %d 轮 ",
	RewindApplyHint:        "↑/↓ · Enter 应用 · Esc 返回",
	RewindEmpty:            "(空)",

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
  reasonix chat [--model NAME] [-c|--continue] [--resume]   交互式会话（多轮；-c 恢复最近一次，--resume 选择一个）
  reasonix run  [--model NAME] [--max-steps N] <task>   执行单次任务后退出
  reasonix serve [--model NAME] [--addr HOST:PORT]      通过 HTTP+SSE 提供会话（浏览器客户端在 /）
  reasonix setup [path]                                 交互式配置向导；生成 reasonix.toml（及 .env）
  reasonix mcp <add|remove|list>                        管理 reasonix.toml 里的 MCP 服务器
  reasonix doctor [--json]                              输出脱敏的本地诊断信息
  reasonix version
  reasonix help

示例：
  reasonix chat
  reasonix chat --continue
  reasonix run "把 main.go 里的 TODO 实现掉"
  reasonix run --model mimo-pro "给这个函数补单元测试"
  echo "解释这段代码" | reasonix run

配置：
  优先级：flag > ./reasonix.toml > ~/.config/reasonix/config.toml > 内置默认值
  密钥通过 api_key_env 从环境变量注入（如 DEEPSEEK_API_KEY）。
  运行 'reasonix setup' 生成配置；详见 docs/SPEC.md。
`,
}
