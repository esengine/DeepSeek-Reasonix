package i18n

// English is the baseline catalogue. The drift-guard test reflects over its
// fields, so every other catalogue must populate the same set.
var English = Messages{
	Subtitle:        "config + plugin driven coding agent",
	WelcomeTitleFmt: "Welcome to %s",
	NoConfigYet:     "No configuration found yet — let's set it up.",
	StartingChatFmt: "Starting %s…",
	SetKeyHint:      "Set your API key, then run `reasonix chat`.",
	ConfigLabel:     "config",
	ModelsLabel:     "models",
	ConfigNotFound:  "not found — using built-in defaults",
	ConfigErrorFmt:  "%s — error: %v",
	NoKey:           "no key",
	Ready:           "ready",
	GetStarted:      "Get started",
	StepScaffold:    "scaffold reasonix.toml",
	StepSetKey:      "set API key",

	InitHint:       "Project memory (AGENTS.md) is generated in-session: run `reasonix chat`, then `/init` — the model analyzes the codebase and writes it. For configuration, use `reasonix setup`.",
	StepSetKeyHint: "export DEEPSEEK_API_KEY=… or add to .env",
	StepChatDesc:   "interactive session",
	StepRunDesc:    "one-shot task",
	HelpFooter:     "reasonix help · all commands",

	ChatTip:           "Context is kept across turns. Type 'exit' or Ctrl-D to quit.",
	TurnCancelled:     "cancelled — back to prompt",
	NoSessionToResume: "no saved session to resume — start a new one with `reasonix chat`",
	ResumeRequiresTTY: "--resume needs an interactive terminal; pass --continue for the most recent session",
	PickSessionLabel:  "Resume which session?",

	ResumeListHeader:    "sessions (/resume <n> to switch)",
	ResumeBusy:          "finish or cancel the current turn before resuming",
	ResumeBadIndexFmt:   "pick a session 1–%d (run /resume to list)",
	ResumeAlreadyActive: "already in that session",
	ResumedTitle:        "resumed session",

	ChatThinking:           "thinking…",
	ChatThoughtForFmt:      "thought for %ds",
	ChatStatusThinkingFmt:  "%s thinking… (%ds · Esc cancels)",
	ChatStatusIdle:         "Tab toggles plan · Ctrl-O toggles verbose thinking · Enter sends · Esc clears/exits state · PgUp/PgDn scrolls · Ctrl-D quits",
	ChatStatusPlanApproval: "Enter/y approves & executes · n/Esc keeps planning · PgUp/PgDn scrolls",
	PlanApprovalPrompt:     "Plan ready above — Enter/y to approve & execute, n/Esc to keep planning",
	ChatStatusToolApproval: "y/1 approve once · a/2 allow this session · n/3 deny · Ctrl-C cancels turn",
	AskTypeSomething:       "Type something else",
	AskTypingHint:          "type below, Enter to confirm",
	AskChatInstead:         "None — just chat",
	ChatStatusQuestion:     "↑/↓ move · number to pick · space multi · Enter confirm · ←/→ switch · Esc cancel",
	ToolApprovalPromptFmt:  "Permission required\n\nWill call tool %s%s.\n%s\n[y] allow once    [a] allow this session    [n] deny",
	ToolApprovalSourceFmt:  "Source: %s",
	ToolApprovalBuiltIn:    "built-in tool",
	ToolApprovalImageUse:   "It will read provided image input for image understanding.",

	OutputStyleNone:   "no output styles available",
	OutputStyleHeader: "output styles:",
	OutputStyleHint:   "set agent.output_style in reasonix.toml to apply one (takes effect next session)",

	CompactionWorking: "compacting conversation…",
	CompactionTitle:   "Context compacted",
	CompactionUnit:    "messages",
	CompactionAuto:    "auto",
	CompactionManual:  "manual",

	SlashCompactDone:   "session compacted — older middle replaced by a summary, recent turns kept",
	SlashCompactFailed: "compaction failed",
	SlashNewDone:       "fresh session started — previous transcript saved",
	SlashNewFailed:     "could not start a new session",
	SlashUnavailable:   "command unavailable in this build",
	SlashUnknown:       "unknown command",
	SlashTodoCleared:   "task list dismissed",
	SlashHelp:          "commands: /compact · /new · /resume · /rewind · /tree · /branch · /switch · /todo · /verbose · /model (switch model) · /mcp · /skill · /hooks · /paste-image · /memory · /help · plus skills (/init, /explore, …)",
	SlashPromptEmpty:   "the MCP prompt returned no content to send",
	SlashMCPNone:       "no MCP servers configured — add a [[plugins]] entry in reasonix.toml",
	CompHintSlash:      "↑/↓ move · Tab/Enter select · Esc close",
	CompHintFile:       "↑/↓ move · Tab/Enter open folder or pick file · Esc close",

	CmdNew:          "fork a fresh session",
	CmdCompact:      "compact context",
	CmdRewind:       "rewind to an earlier turn",
	CmdTree:         "show conversation branches",
	CmdBranch:       "create a conversation branch",
	CmdSwitchBranch: "switch conversation branch",
	CmdResume:       "resume a saved session",
	CmdModel:        "switch model",
	CmdMemory:       "show memory files",
	CmdMcp:          "MCP servers",
	CmdHooks:        "manage hooks",
	CmdPasteImage:   "paste clipboard image",
	CmdOutputStyle:  "list output styles",
	CmdSkill:        "manage skills",
	CmdVerbose:      "toggle thinking text",
	CmdHelp:         "list commands",
	CmdTodo:         "dismiss the task list",
	ArgSkillList:    "list skills",
	ArgSkillShow:    "show a skill's body",
	ArgSkillNew:     "scaffold a new skill",
	ArgSkillPaths:   "show discovery paths",
	ArgMcpAdd:       "connect a server",
	ArgMcpRemove:    "disconnect a server",
	ArgMcpList:      "show configured servers",
	ArgMcpConnected: "connected",
	ArgHooksList:    "list active hooks",
	ArgHooksTrust:   "trust this project's hooks",
	ArgModelCurrent: "current",

	ListModelsHeaderFmt: "models (active: %s)",
	ListModelsHint:      "switch with the model switcher, or type /model <provider/model>",
	ListMemoryHeader:    "memory files",
	ListMemoryNone:      "memory: none — add with “#<note>” or run /init to generate AGENTS.md",
	ListSkillsHeaderFmt: "skills (%d)",
	ListSkillsNone:      "skills: none defined — invoke a built-in like /init, or author one with install_skill",
	ListHooksHeaderFmt:  "hooks (%d active)",
	ListHooksNone:       "hooks: none active — configure in .reasonix/settings.json (project, after trust) or ~/.reasonix/settings.json (global)",
	ListMcpHeader:       "mcp servers",
	ListMcpNone:         "mcp: no servers connected — add one in reasonix.toml ([[plugins]]) or a project .mcp.json",

	SelectProvidersLabel:  "Select providers to enable",
	EnterAPIKeysHeader:    "Enter API keys (Enter to skip and set later in .env):",
	MissingKeyIntro:       "reasonix.toml is ready — just an API key away.",
	WroteFileFmt:          "Wrote %s",
	SetupComplete:         "Setup complete.",
	SetupCancelled:        "setup cancelled.",
	TryHintFmt:            "Try: %s",
	NextHint:              "Next: set your API key (export DEEPSEEK_API_KEY=... or add to .env), then run `reasonix run \"your task\"`.",
	ConfirmReconfigureFmt: "%s already exists. Reconfigure and overwrite?",
	KeepingExisting:       "Keeping existing config.",
	NotOverwritingFmt:     "%s already exists; not overwriting",

	UnknownCommandFmt: "unknown command %q",
	UsageRunHint:      "usage: reasonix run [--model NAME] <task>",
	ErrorPrefix:       "error:",
	WriteConfigErr:    "write config:",
	WriteEnvErr:       "write .env:",

	SelectOneHint:  "(↑/↓ · Enter · q to cancel)",
	SelectManyHint: "(↑/↓ · Space · Enter · q)",

	UsageBody: `reasonix — a config- and plugin-driven coding agent (multi-model)

Usage:
  reasonix chat [--model NAME] [-c|--continue] [--resume]   interactive session (multi-turn; -c resumes the latest, --resume picks one)
  reasonix run  [--model NAME] [--max-steps N] <task>   run one task and exit
  reasonix serve [--model NAME] [--addr HOST:PORT]      serve the session over HTTP+SSE (browser client at /)
  reasonix setup [path]                                 interactive config wizard; writes reasonix.toml (+ .env)
  reasonix mcp <add|remove|list>                        manage MCP servers in reasonix.toml
  reasonix version
  reasonix help

Examples:
  reasonix chat
  reasonix chat --continue
  reasonix run "implement the TODOs in main.go"
  reasonix run --model mimo-pro "add unit tests for this function"
  echo "explain this code" | reasonix run

Configuration:
  Resolution: flag > ./reasonix.toml > ~/.config/reasonix/config.toml > built-in defaults
  Secrets come from the environment via api_key_env (e.g. DEEPSEEK_API_KEY).
  Run 'reasonix setup' to scaffold a config; see docs/SPEC.md.
`,
}
