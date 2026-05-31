// Package i18n holds the CLI's translatable strings and a small detection
// helper. Architecture: a single Messages struct of exported string fields
// (plain text or fmt format strings, suffix *Fmt flags the latter). Each
// language declares one Messages value in its own file. Call sites read
// i18n.M.SomeField; for parameterised messages they pass it to fmt.Sprintf.
//
// Adding a field requires updating every messages_*.go file — drift is caught
// at test time by TestCatalogsComplete via reflection, so a missing translation
// fails CI instead of surfacing as a blank line at runtime.
//
// Scope (v1): CLI surface only — welcome, init wizard, chat REPL banner, usage,
// user-facing CLI errors. System prompts, internal error wrappers, and agent
// runtime telemetry stay English so model behaviour and developer logs are
// language-stable.
package i18n

import (
	"os"
	"strings"
)

// Messages is the catalogue of translatable CLI strings. Plain fields are
// printed verbatim; *Fmt fields are fmt format strings the caller passes to
// fmt.Sprintf. Catalogue values do not include trailing newlines — call sites
// add framing whitespace, so the same field works wherever it appears.
type Messages struct {
	// welcome / status screen
	Subtitle        string // tagline under the product name in the welcome box
	WelcomeTitleFmt string // first-run box title — %s = product name (styled)
	NoConfigYet     string // first-run cue under the welcome box
	StartingChatFmt string // "Starting %s…" before dropping into chat
	SetKeyHint      string // shown when key is missing after init
	ConfigLabel     string // "config" status row label
	ModelsLabel     string // "models" status row label
	ConfigNotFound  string // shown when no config file exists
	ConfigErrorFmt  string // "%s — error: %v" — config path + parse error
	NoKey           string // status dot — no API key set
	Ready           string // status dot — provider ready
	GetStarted      string // section title above numbered steps
	StepScaffold    string // step 1 desc — reasonix setup
	StepSetKey      string // step 2 command label

	// `reasonix init` — points to the in-session /init skill + setup
	InitHint       string
	StepSetKeyHint string // step 2 desc — env var hint
	StepChatDesc   string // reasonix chat step desc
	StepRunDesc    string // reasonix run step desc
	HelpFooter     string // dim footer linking to reasonix help

	// chat REPL
	ChatTip           string // tip line under the chat banner
	TurnCancelled     string // shown when Ctrl-C aborts the in-flight turn but the chat keeps running
	NoSessionToResume string // shown when --continue / --resume finds nothing
	ResumeRequiresTTY string // shown when --resume runs piped instead of on a terminal
	PickSessionLabel  string // header on the --resume picker

	// chat TUI status line / approval banner.
	ChatStatusThinkingFmt  string // "%s thinking… (%ds · <cancel hint>)" — %s = spinner, %d = elapsed s
	ChatStatusIdle         string // shortcuts hint when idle
	ChatStatusPlanApproval string // shortcuts hint while a plan is pending
	PlanApprovalPrompt     string // one-line "plan above is ready" banner shown above the input
	ChatStatusToolApproval string // shortcuts hint while a tool call awaits approval
	ToolApprovalPromptFmt  string // "Allow %s%s?" banner — %s = tool name, %s = subject (leading space, or empty)

	// `ask` tool question card.
	AskTypeSomething   string // the "type your own answer" option label
	AskTypingHint      string // shown on that row while entering free text
	AskChatInstead     string // the "don't pick, just chat" option label
	ChatStatusQuestion string // shortcuts hint while a question card is open

	// output style listing (/output-style).
	OutputStyleNone   string // no styles available
	OutputStyleHeader string // header above the listing
	OutputStyleHint   string // how to select one

	// context compaction card (CompactionStarted / CompactionDone events).
	CompactionWorking string // shown while the summarizer runs
	CompactionTitle   string // card header before "· N messages · <trigger>"
	CompactionUnit    string // the noun counted, e.g. "messages"
	CompactionAuto    string // trigger label: reached the window threshold
	CompactionManual  string // trigger label: user ran /compact

	// chat TUI slash commands.
	SlashCompactDone   string // "/compact" succeeded
	SlashCompactFailed string // "/compact" errored, prefixed before the underlying error
	SlashNewDone       string // "/new" succeeded
	SlashNewFailed     string // "/new" errored
	SlashTodoCleared   string // "/todo" dismissed the pinned task list
	SlashUnavailable   string // the command is configured off (no callback wired)
	SlashUnknown       string // shown when the user types an unrecognised "/cmd"
	SlashHelp          string // listed commands
	SlashPromptEmpty   string // an MCP prompt returned no text to send
	SlashMCPNone       string // /mcp when no MCP servers are connected
	CompHintSlash      string // key hint footer under the slash-command menu
	CompHintFile       string // key hint footer under the @ file/resource menu

	// slash command + sub-command descriptions shown in the menu (CLI and desktop
	// share these via i18n.M, so both frontends localize identically).
	CmdNew          string // /new
	CmdCompact      string // /compact
	CmdRewind       string // /rewind
	CmdModel        string // /model
	CmdMemory       string // /memory
	CmdMcp          string // /mcp
	CmdHooks        string // /hooks
	CmdOutputStyle  string // /output-style
	CmdSkill        string // /skill
	CmdHelp         string // /help
	CmdTodo         string // /todo
	ArgSkillList    string // /skill list
	ArgSkillShow    string // /skill show
	ArgSkillNew     string // /skill new
	ArgSkillPaths   string // /skill paths
	ArgMcpAdd       string // /mcp add
	ArgMcpRemove    string // /mcp remove
	ArgMcpList      string // /mcp list
	ArgMcpConnected string // /mcp remove <server> tag
	ArgHooksList    string // /hooks list
	ArgHooksTrust   string // /hooks trust
	ArgModelCurrent string // /model <ref> active tag

	// management listing notices (the Submit path: desktop / HTTP frontends)
	ListModelsHeaderFmt string // "models (active: %s)"
	ListModelsHint      string // how to switch
	ListMemoryHeader    string // "memory files"
	ListMemoryNone      string // no memory docs
	ListSkillsHeaderFmt string // "skills (%d)"
	ListSkillsNone      string // no skills
	ListHooksHeaderFmt  string // "hooks (%d active)"
	ListHooksNone       string // no hooks
	ListMcpHeader       string // "mcp servers"
	ListMcpNone         string // no mcp servers

	// init wizard
	SelectProvidersLabel  string // multi-select label
	EnterAPIKeysHeader    string // header before the per-env-var prompts
	MissingKeyIntro       string // shown when re-running the key step on a configured setup
	WroteFileFmt          string // "Wrote %s" — used for reasonix.toml and .env both
	SetupComplete         string // success line at end of init
	SetupCancelled        string // shown when the user aborts the wizard
	TryHintFmt            string // "Try: %s" — %s = command to try (styled)
	NextHint              string // non-interactive post-write hint
	ConfirmReconfigureFmt string // "%s already exists. Reconfigure and overwrite?"
	KeepingExisting       string // when the user declines to overwrite
	NotOverwritingFmt     string // non-interactive overwrite refusal

	// top-level / runAgent
	UnknownCommandFmt string // "unknown command %q"
	UsageRunHint      string // "usage: reasonix run [--model NAME] <task>"
	ErrorPrefix       string // "error:" — prefix for fatal-error output
	WriteConfigErr    string // "write config:" — prefix for write failure
	WriteEnvErr       string // "write .env:" — prefix for env-write failure

	// selection menus
	SelectOneHint  string // "(↑/↓ · Enter · q to cancel)"
	SelectManyHint string // "(↑/↓ · Space · Enter · q)"

	// usage / help
	UsageBody string // full multi-line help text
}

// M is the active catalogue. DetectLanguage replaces it; English is the
// default so any code path that runs before detection still has text.
var M = English

// DetectLanguage selects a catalogue from override (e.g. cfg.Language) or the
// environment and installs it as M. Returns the resolved tag ("en", "zh") so
// callers can log or expose it.
//
// Priority: override > REASONIX_LANG > LC_ALL > LC_MESSAGES > LANG > "en".
func DetectLanguage(override string) string {
	for _, c := range append([]string{override}, envCandidates()...) {
		if tag := normalize(c); tag != "" {
			return setLanguage(tag)
		}
	}
	return setLanguage("en")
}

func envCandidates() []string {
	keys := []string{"REASONIX_LANG", "LC_ALL", "LC_MESSAGES", "LANG"}
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = os.Getenv(k)
	}
	return out
}

func setLanguage(tag string) string {
	switch tag {
	case "zh":
		M = Chinese
		return "zh"
	default:
		M = English
		return "en"
	}
}

// normalize maps a locale string (e.g. "zh_CN.UTF-8", "zh-Hans-CN", "Chinese
// (China)") to a short tag this package knows about. Returns "" for empty or
// unrecognised input so DetectLanguage can fall through to the next candidate.
func normalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "zh") || strings.Contains(s, "chinese") || strings.Contains(s, "中文") {
		return "zh"
	}
	if strings.HasPrefix(s, "en") || strings.Contains(s, "english") {
		return "en"
	}
	return ""
}
