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
	StepChatDesc   string // interactive session step desc
	StepRunDesc    string // reasonix run step desc
	HelpFooter     string // dim footer linking to reasonix help

	// chat REPL
	ChatTip           string // tip line under the chat banner
	TurnCancelled     string // shown when Ctrl-C aborts the in-flight turn but the chat keeps running
	NoSessionToResume string // shown when --continue / --resume finds nothing
	ResumeRequiresTTY string // shown when --resume runs piped instead of on a terminal
	PickSessionLabel  string // header on the --resume picker

	// in-chat /resume command
	ResumeListHeader    string // header above the /resume session list
	ResumeBusy          string // shown when /resume is used mid-turn
	ResumeBadIndexFmt   string // shown when /resume gets an out-of-range index (one %d)
	ResumeAlreadyActive string // shown when /resume targets the current session
	ResumedTitle        string // banner title after a /resume switch

	RenameUsage     string // /rename with no args
	RenameNoSession string // /rename with no active session
	RenameDoneFmt   string // /rename succeeded (one %s = new title)
	ResumePickTitle string // header in the interactive resume picker
	ResumePickHint  string // keyboard hint in the interactive resume picker

	// chat TUI status line / approval banner.
	ChatThinking                           string // live reasoning marker label, e.g. "thinking…"
	ChatThoughtForFmt                      string // collapsed reasoning summary, "%d" = elapsed s
	ChatStatusThinkingFmt                  string // "%s thinking… (%ds · <cancel hint>)" — %s = spinner, %d = elapsed s
	ChatToolWorkingFmt                     string // "%s working · %ds" under a running tool — %s = spinner, %d = elapsed s
	ChatStatusRetryingFmt                  string // "%s retrying (%d/%d)…" — %s = spinner, %d/%d = attempt/max
	ChatStatusCancellingFmt                string // "%s stopping… (%ds · Ctrl+C exits)" — %s = spinner, %d = elapsed s
	ChatStatusIdle                         string // shortcuts hint when idle
	ChatStatusYoloIdle                     string // shortcuts hint when idle in YOLO/bypass mode
	ChatStatusCycleHint                    string // plan-toggle shortcut hint shown when no modal prompt owns the status row
	ChatStatusCacheNowFmt                  string // cache status tag, "%s" = latest-turn hit rate with percent sign
	ChatStatusCacheAvgFmt                  string // cache status tag, "%s" = session-average hit rate with percent sign
	ChatStatusPlanApproval                 string // shortcuts hint while a plan is pending
	PlanApprovalPrompt                     string // one-line "plan above is ready" banner shown above the input
	ChatStatusToolApproval                 string // shortcuts hint while a tool call awaits approval
	ToolApprovalPromptFmt                  string // approval banner — tool, subject suffix, source/intent detail, choices
	ToolApprovalChoices                    string // standard approval choice list
	BashPrefixChoices                      string // approval choice list when a bash prefix can be granted
	PlanModeReadOnlyCommandChoices         string // approval choice list for plan-mode read-only command trust
	FreshHumanApprovalChoices              string // approval choice list for prompts that cannot be remembered
	ToolApprovalSourceFmt                  string // "Source: %s" / "来源: %s"
	ToolApprovalBuiltIn                    string // built-in tool source label
	ToolApprovalImageUse                   string // image-understanding detail for understand_image-style tools
	PermissionSavedFmt                     string // permission rule saved notice: path, rule
	PermissionAlreadyAllowedFmt            string // permission rule already covered notice: path, rule
	PermissionSaveFailedFmt                string // permission rule save failure notice: rule, error
	MCPReadOnlyTrustSavedFmt               string // MCP trusted read-only saved notice: path, server, tool
	MCPReadOnlyTrustAlreadyFmt             string // MCP trusted read-only already covered notice: path, server, tool
	MCPReadOnlyTrustFailedFmt              string // MCP trusted read-only save failure notice: server, tool, error
	PlanModeReadOnlyCommandTrustSavedFmt   string // plan-mode bash read-only prefix saved notice: path, prefix
	PlanModeReadOnlyCommandTrustAlreadyFmt string // plan-mode bash read-only prefix already covered notice: path, prefix
	PlanModeReadOnlyCommandTrustFailedFmt  string // plan-mode bash read-only prefix save failure notice: prefix, error
	DiffFoldedFmt                          string // "… +%d more lines" footer when a writer diff is folded
	DiffFoldEnabledFmt                     string // notice when /diff-fold enables folding, %d = line limit
	DiffFoldDisabled                       string // notice when /diff-fold disables folding (shows all lines)

	// `ask` tool question card.
	AskTypeSomething   string // the "type your own answer" option label
	AskTypingHint      string // shown on that row while entering free text
	AskChatInstead     string // the "don't pick, just chat" option label
	ChatStatusQuestion string // shortcuts hint while a question card is open
	StatusResumePicker string // status tag while the resume picker is open (e.g. "select session")
	AskSubmitTitle     string // submit-tab title in the ask tool question card
	AskUnanswered      string // placeholder for an unanswered ask question
	AskSubmitHint      string // submit-tab keyboard hint

	// output style listing (/output-style).
	OutputStyleNone    string // no styles available
	OutputStyleHeader  string // header above the listing
	OutputStyleHint    string // how to select one
	ThemeHeader        string // header above the /theme listing
	ThemeHint          string // how to select a theme
	ThemeChangedFmt    string // "/theme <name>" succeeded
	ThemeUnknownFmt    string // "/theme <name>" unknown
	LanguageHeader     string // header above the /language listing
	LanguageHint       string // how to select a language
	LanguageChangedFmt string // "/language <tag>" succeeded, %s = saved tag, %s = resolved tag

	// context compaction card (CompactionStarted / CompactionDone events).
	CompactionWorking string // shown while the summarizer runs
	CompactionTitle   string // card header before "· N messages · <trigger>"
	CompactionUnit    string // the noun counted, e.g. "messages"
	CompactionAuto    string // trigger label: reached the window threshold
	CompactionManual  string // trigger label: user ran /compact

	// chat TUI slash commands.
	SlashCompactDone    string // "/compact" succeeded
	SlashCompactFailed  string // "/compact" errored, prefixed before the underlying error
	SlashNewDone        string // "/new" succeeded
	SlashNewFailed      string // "/new" errored
	SlashClearPrompt    string // "/clear" destructive confirmation prompt
	SlashClearDone      string // "/clear" succeeded
	SlashClearFailed    string // "/clear" errored
	SlashClsDone        string // "/cls" succeeded
	SlashTodoCleared    string // "/todo" dismissed the pinned task list
	SlashUnavailable    string // the command is configured off (no callback wired)
	SlashUnknown        string // shown when the user types an unrecognised "/cmd"
	SlashHelp           string // listed commands
	SlashPromptEmpty    string // an MCP prompt returned no text to send
	SlashMCPNone        string // /mcp when no MCP servers are connected
	CtrlCQuitHint       string // shown on first Ctrl+C while idle; second press exits
	CompHintSlash       string // key hint footer under the slash-command menu
	CompHintFile        string // key hint footer under the @ file/resource menu
	MouseCopiedHint     string // transient status-line hint after a mouse/Ctrl+C selection copy
	MouseCaptureOnHint  string // "/mouse" turned in-app mouse handling back on
	MouseCaptureOffHint string // "/mouse" released mouse capture to the terminal
	MouseCaptureTag     string // persistent status-line marker while mouse capture is off

	// shell execution (! prefix).
	ShellExecEmpty      string // bare "!" with no command
	ShellExecFailedFmt  string // "shell command failed: %v"
	ShellExecTimeoutFmt string // "shell command timed out (> %s)"
	ShellModeHint       string // status line hint when input starts with !

	// slash command + sub-command descriptions shown in the menu (CLI and desktop
	// share these via i18n.M, so both frontends localize identically).
	CmdNew              string // /new
	CmdClear            string // /clear
	CmdCls              string // /cls
	CmdCompact          string // /compact
	CmdRewind           string // /rewind
	CmdTree             string // /tree
	CmdBranch           string // /branch
	CmdSwitchBranch     string // /switch
	CmdResume           string // /resume
	CmdRename           string // /rename
	CmdModel            string // /model
	CmdMemory           string // /memory
	CmdMigrate          string // /migrate
	CmdGoal             string // /goal
	CmdRemember         string // /remember
	CmdForget           string // /forget
	CmdMcp              string // /mcp
	CmdHooks            string // /hooks
	CmdPlugins          string // /plugins
	CmdPasteImage       string // /paste-image
	CmdOutputStyle      string // /output-style
	CmdTheme            string // /theme
	CmdLanguage         string // /language
	CmdSkill            string // /skills
	CmdVerbose          string // /verbose
	CmdReloadCmd        string // /reload-cmd
	CmdDiffFold         string // /diff-fold
	CmdSandbox          string // /sandbox
	CmdEffort           string // /effort
	CmdMouse            string // /mouse
	CmdAutoPlan         string // /auto-plan
	CmdReasonLang       string // /reasoning-language
	CmdMemoryV5         string // /memory-v5
	CmdHelp             string // /help
	CmdTodo             string // /todo
	CmdQuit             string // /quit (also accepts /exit as hidden alias)
	CmdCopy             string // /copy
	CmdExport           string // /export
	SlashCopyDone       string // "/copy" succeeded
	SlashCopyEmpty      string // no assistant response to copy
	SlashCopyListHeader string // header shown before the numbered list
	SlashExportDoneFmt  string // "/export" succeeded, %s = file path
	SlashExportEmpty    string // no messages to export
	ArgSkillList        string // /skills list
	ArgSkillShow        string // /skills show
	ArgSkillNew         string // /skills new
	ArgSkillPaths       string // /skills paths
	ArgMcpAdd           string // /mcp add
	ArgMcpRemove        string // /mcp remove
	ArgMcpList          string // /mcp list
	ArgMcpConnected     string // /mcp remove <server> tag
	ArgHooksList        string // /hooks list
	ArgHooksTrust       string // /hooks trust
	ArgModelCurrent     string // /model <ref> active tag
	ArgEffortAuto       string // /effort auto
	ArgEffortLow        string // /effort low
	ArgEffortMedium     string // /effort medium
	ArgEffortHigh       string // /effort high
	ArgEffortXHigh      string // /effort xhigh
	ArgEffortMax        string // /effort max
	ArgThemeCurrent     string // /theme <style> active tag
	ArgLanguageAuto     string // /language auto
	ArgLanguageEn       string // /language en
	ArgLanguageZh       string // /language zh

	// management listing notices (the Submit path: desktop / HTTP frontends)
	ListModelsHeaderFmt string // "models (active: %s)"
	ListModelsHint      string // how to switch
	ListMemoryHeader    string // "memory files"
	ListMemorySaved     string // "saved memories"
	ListMemoryArchived  string // "archived memories"
	ListMemoryNone      string // no memory docs
	ListSkillsHeaderFmt string // "skills (%d)"
	ListSkillsNone      string // no skills
	ListHooksHeaderFmt  string // "hooks (%d active)"
	ListHooksNone       string // no hooks
	ListMcpHeader       string // "mcp servers"
	ListMcpNone         string // no mcp servers

	// in-chat memory/model/rewind notices.
	MemoryNone             string
	MemoryLoaded           string
	MemorySavedHeader      string
	MemoryStoredUnderFmt   string
	MemoryEditHint         string
	ForgetUsage            string
	ForgetDoneFmt          string
	QuickRememberEmpty     string
	QuickRememberDoneFmt   string
	GoalEmpty              string
	GoalCurrentFmt         string
	GoalSetFmt             string
	GoalCleared            string
	ModelSwitchUnavailable string
	ModelSwitchBusy        string
	ModelAlreadyOnFmt      string
	ModelSwitchingFmt      string
	ModelSwitchedFmt       string
	ModelListHeader        string
	RewindNone             string
	RewindCodeConversation string
	RewindConversationOnly string
	RewindCodeOnly         string
	RewindFork             string
	RewindSummarizeFrom    string
	RewindSummarizeUpto    string
	RewindPickTitle        string
	RewindPickHint         string
	RewindRestoreTitleFmt  string
	RewindApplyHint        string
	RewindEmpty            string

	// skill picker overlay (/skills interactive panel in CLI TUI)
	SkillPickerTitle             string
	SkillPickerAvailableFmt      string
	SkillPickerMatchingFmt       string // "%d matching · %d total" when searching
	SkillPickerHint              string
	SkillPickerDetailHint        string
	SkillPickerSearchEmpty       string
	SkillPickerSearchPrompt      string
	SkillPickerSearchPlaceholder string
	SkillPickerSourceTitle       string
	SkillPickerSourceActiveFmt   string
	SkillPickerSourceHint        string
	SkillPickerDiagHidden        string
	SkillPickerDiagShown         string
	SkillPickerBuiltinSource     string
	SkillPickerRescanned         string
	SkillPickerNoDescription     string
	SkillPickerScopeProject      string
	SkillPickerScopeCustom       string
	SkillPickerScopeGlobal       string
	SkillPickerScopeBuiltin      string
	SkillPickerSubagent          string
	SkillPickerAvailableLabel    string
	SkillPickerDisabledLabel     string
	SkillPickerNoChanges         string
	SkillPickerSourceSkillsHint  string
	SkillPickerSourceSkillsEmpty string
	SkillPickerActionToggle      string
	SkillPickerActionDelete      string
	SkillPickerDeleteTitleFmt    string // "Delete skill %s?"
	SkillPickerDeleteConfirm     string
	SkillPickerDeleteCancel      string
	SkillPickerDeleteHint        string
	SkillPickerDeletedFmt        string // "deleted skill %s"
	SkillPickerMoreAboveFmt      string // "↑ %d more above"
	SkillPickerMoreBelowFmt      string // "↓ %d more below"
	SkillPickerTokenFmt          string // "~%d tok"
	SkillPickerDetailMetaFmt     string // "Scope: %s  Run as: %s"
	SkillPickerSkillsUnit        string // "skills" (used as "%d skills")
	SkillPickerLinesUnit         string // "lines" (used as "+N more lines")
	SkillPickerStatusLabel       string // shown in the TUI status bar while picker is open
	SkillPickerStatusOK          string // "ok" path status label
	SkillPickerStatusMissing     string // "missing" path status label
	SkillPickerStatusNotDir      string // "not-directory" path status label
	SkillPickerStatusUnreadable  string // "unreadable" path status label

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

	// model fetching
	FetchingModelsFmt          string // "Fetching models for %s..."
	FetchModelsSuccessFmt      string // "Found %d models for %s"
	FetchModelsFailedFmt       string // "Failed to fetch models for %s: %v"
	FetchModelsUsingPresetsFmt string // "Live fetch unavailable for %s, using preset model list"
	FamilyKeyPromptFmt         string // "Enter your %s API key to list available models (Enter to skip):"
	SelectModelsLabel          string // "Select models to enable for %s"
	NoModelsAvailableFmt       string // "%s: no models available, skipping"
	CustomFetchEmpty           string // "/models returned an empty list — falling back to manual entry"
	AnthropicFetchEmpty        string // "/models returned an empty list — Anthropic-compatible providers usually don't expose one, falling back to manual entry"
	SkipStaleCustomEntryFmt    string // "skipping stale %q entry from reasonix.toml (pointing at %s) — please remove it"
	APIKeyAlreadySetFmt        string // "reusing existing value for %s"
	APIKeyResetPromptFmt       string // "Re-enter %s?"

	// custom provider
	CustomProviderLabel  string // "Custom Model"
	CustomProviderDesc   string // "Add third-party OpenAI compatible model"
	CustomAddMethodLabel string // "Select add method"
	CustomMethodManual   string // "Enter model name manually"
	CustomMethodURL      string // "Fetch models from URL"
	CustomPromptModel    string // "Enter model name"
	CustomPromptBaseURL  string // "Enter Base URL"
	CustomPromptKeyEnv   string // "Enter API Key env var name"
	CustomPromptAPIKey   string // "Enter API Key"
	CustomAddedFmt       string // "Added custom model: %s"

	// Anthropic compatible provider
	AnthropicProviderLabel         string // "Anthropic Compatible"
	AnthropicProviderDesc          string // "Add Anthropic API compatible model"
	AnthropicAddMethodLabel        string // "Select add method"
	AnthropicMethodManual          string // "Enter model name manually"
	AnthropicMethodURL             string // "Fetch models from URL"
	AnthropicPromptModel           string // "Enter model name"
	AnthropicPromptBaseURL         string // "Enter Base URL"
	AnthropicPromptKeyEnv          string // "Enter API Key env var name"
	AnthropicPromptAPIKey          string // "Enter API Key"
	AnthropicAddedFmt              string // "Added Anthropic compatible model: %s"
	AnthropicFetchingModelsFmt     string // "Fetching models for %s..."
	AnthropicFetchModelsSuccessFmt string // "Found %d models for %s"
	AnthropicFetchModelsFailedFmt  string // "Failed to fetch models for %s: %v"
	AnthropicSelectModelsLabel     string // "Select models to enable for %s"

	// top-level / runAgent
	UnknownCommandFmt         string // "unknown command %q"
	UsageRunHint              string // "usage: reasonix run [--model NAME] <task>"
	ErrorPrefix               string // "error:" — prefix for fatal-error output
	ReconfigureOnUnknownModel string // shown when the configured model no longer resolves and setup is re-run
	WriteConfigErr            string // "write config:" — prefix for write failure
	WriteEnvErr               string // "write .env:" — prefix for env-write failure

	// provider HTTP error explanations — actionable, reason + fix per status code
	ProviderErrBadRequest          string // 400
	ProviderErrAuth                string // 401 — no key configured / sent
	ProviderErrAuthRejected        string // 401 — a key was sent but the server rejected it
	ProviderErrInsufficientBalance string // 402
	ProviderErrUnprocessable       string // 422
	ProviderErrRateLimited         string // 429
	ProviderErrServer              string // 500
	ProviderErrServerBusy          string // 503

	// selection menus
	SelectOneHint      string // "(↑/↓ · Enter · q to cancel)"
	SelectManyHint     string // "(↑/↓ · Space · Enter · q)"
	SelectMoreAboveFmt string // "↑ %d more above"
	SelectMoreBelowFmt string // "↓ %d more below"
	SelectSearchHint   string // "/ to search · Esc to cancel"

	// /provider command
	CmdProvider          string // /provider
	ProviderListHeader   string // header for /provider list
	ProviderAlreadyOnFmt string // already on provider
	ProviderUnknownFmt   string // unknown provider
	ProviderPickLabel    string // label for provider model picker
	ProviderNoModelsFmt  string // provider has no models

	// `reasonix upgrade` / `reasonix update` — self-update
	UpgradeChecking            string // "Checking for updates…"
	UpgradeDevBuild            string // dev builds cannot self-update
	UpgradeFetchFailed         string // "failed to check for updates: %v"
	UpgradeInvalidVersion      string // remote version not valid semver
	UpgradeAlreadyLatest       string // already on the latest version
	UpgradeForcing             string // "Reinstalling the same version…"
	UpgradeAvailableFmt        string // "Current: %s → Latest: %s"
	UpgradeNoAssetFmt          string // "no binary found for %s"
	UpgradeDownloadingFmt      string // "Downloading %s (%s)…"
	UpgradeDownloadFailed      string // "download failed: %v"
	UpgradeVerifying           string // "Verifying checksum…"
	UpgradeChecksumFailed      string // "could not fetch checksum file: %v"
	UpgradeChecksumMismatchFmt string // SHA256 mismatch detail
	UpgradeChecksumNotFoundFmt string // asset not listed in SHA256SUMS
	UpgradeExtractFailed       string // "failed to extract binary: %v"
	UpgradeApplying            string // "Replacing binary…"
	UpgradeApplyFailed         string // "failed to apply update: %v"
	UpgradeSuccessFmt          string // "Updated %s → %s"

	// usage / help
	UsageBody string // full multi-line help text
}

// ProviderStatusMessage returns an actionable explanation for a known provider
// HTTP status, or "" when the status has no specific guidance.
func (m Messages) ProviderStatusMessage(status int) string {
	switch status {
	case 400:
		return m.ProviderErrBadRequest
	case 401, 403:
		return m.ProviderErrAuth
	case 402:
		return m.ProviderErrInsufficientBalance
	case 422:
		return m.ProviderErrUnprocessable
	case 429:
		return m.ProviderErrRateLimited
	case 500:
		return m.ProviderErrServer
	case 503:
		return m.ProviderErrServerBusy
	}
	return ""
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
	case "zh-tw", "zh-TW":
		M = ChineseTraditional
		return "zh-TW"
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
	s = strings.ReplaceAll(s, "_", "-") // zh_TW.UTF-8 → zh-tw.utf-8 (POSIX locales use underscores)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "zh-tw") || strings.HasPrefix(s, "zh-hant") || strings.Contains(s, "chinese traditional") || strings.Contains(s, "繁體") {
		return "zh-TW"
	}
	if strings.HasPrefix(s, "zh") || strings.Contains(s, "chinese") || strings.Contains(s, "中文") {
		return "zh"
	}
	if strings.HasPrefix(s, "en") || strings.Contains(s, "english") {
		return "en"
	}
	return ""
}
