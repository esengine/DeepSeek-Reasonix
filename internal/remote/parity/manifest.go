// Package parity records how the current Desktop bridge maps onto Remote V1.
//
// The manifest is deliberately independent of the Remote protocol package. It
// is an audit gate over the user-reachable Desktop bridge, not a second method
// registry and not a promise that legacy Wails signatures become Remote wire
// signatures unchanged.
package parity

import (
	"fmt"
	"sort"
	"strings"
)

// Category is one of the frozen RuntimeParityManifest classifications from
// REMOTE_ARCHITECTURE.zh-CN.md section 14.2.
type Category string

const (
	SharedRuntime Category = "shared-runtime"
	DesktopLocal  Category = "desktop-local"
	HostReadonly  Category = "host-readonly"
	DeferredV1    Category = "deferred-v1"
	OutOfScope    Category = "out-of-scope"
)

// Entry classifies one exported method on the Wails-bound desktop App and its
// matching desktop/frontend/src/lib/bridge.ts AppBindings declaration.
type Entry struct {
	Method   string
	Category Category
}

type methodGroup struct {
	category Category
	methods  []string
}

// AppBindings is the current name of the user-reachable frontend bridge
// interface. Tests also derive the production Go method set directly from the
// Wails-bound App, so either side drifting requires an explicit classification.
// Some entries combine UI plumbing with Host work today. Those entries stay in
// SharedRuntime whenever dropping the Host part would lose a Remote V1 feature;
// later extraction may leave their tab/window portion in DesktopLocal.
var groups = []methodGroup{
	{
		category: SharedRuntime,
		methods: []string{
			// These Remote-named bridge methods are temporary Desktop
			// integration points over the target-neutral Workspace and
			// SessionRecord domains. Classification does not assert that the
			// corresponding Remote V1 implementation is complete.
			"BrowseRemoteWorkspace",
			"CreateRemoteWorkspaceSession",
			"Submit",
			"SubmitToTab",
			"SubmitDisplay",
			"SubmitDisplayToTab",
			"SubmitDeliveryRecoveryToTab",
			"SubmitInvocationsToTab",
			"SubmitEditedDisplayToTab",
			"RunShell",
			"RunShellForTab",
			"Steer",
			"SteerForTab",
			"Cancel",
			"CancelTab",
			"Approve",
			"ApproveTab",
			"AnswerQuestion",
			"AnswerQuestionForTab",
			"ReplayPendingPrompts",
			"SetPlanMode",
			"SetMode",
			"SetModeForTab",
			"SetAutoApproveTools",
			"SetCollaborationMode",
			"SetCollaborationModeForTab",
			"SetToolApprovalMode",
			"SetToolApprovalModeForTab",
			"SetGoal",
			"SetGoalForTab",
			"ResumeGoalForTab",
			"ClearGoal",
			"ClearGoalForTab",
			"Compact",
			"CompactForTab",
			"NewSession",
			"NewSessionForTab",
			"ClearSession",
			"ClearSessionForTab",
			"History",
			"HistoryForTab",
			"HistoryPage",
			"HistoryPageForTab",
			"HistoryCheckpointTurnsForTab",
			"Checkpoints",
			"CheckpointsForTab",
			"Rewind",
			"RewindForTab",
			"Fork",
			"ForkForTab",
			"SummarizeFrom",
			"SummarizeFromForTab",
			"SummarizeUpTo",
			"SummarizeUpToForTab",
			"ListSessions",
			"ListTrashedSessions",
			"ResumeSession",
			"ResumeSessionForTab",
			"ResumeSessionPage",
			"ResumeSessionPageForTab",
			"PreviewSession",
			"DeleteSession",
			"DeleteRecoveryCopy",
			"RestoreSession",
			"PurgeTrashedSession",
			"PurgeRecoveryCopy",
			"RenameSession",
			"ScanPromptHistory",
			"ListWorkspaces",
			"SwitchWorkspace",
			"RemoveWorkspace",
			"ContextUsage",
			"ContextUsageForTab",
			"Balance",
			"BalanceForTab",
			"Jobs",
			"JobsForTab",
			"ToolResultForTab",
			"Meta",
			"MetaForTab",
			"AutoResearchCurrent",
			"AutoResearchStatus",
			"AutoResearchList",
			"AutoResearchFindings",
			"AutoResearchRecordEvidence",
			"SlashArgs",
			"ListDir",
			"ListDirForTab",
			"SearchFileRefs",
			"SearchFileRefsForTab",
			"ReadFile",
			"ReadFileForTab",
			"WorkspaceChanges",
			"WorkspaceGitHistory",
			"WorkspaceGitCommitDetail",
			"SetModel",
			"SetModelForTab",
			"SetEffort",
			"SetEffortForTab",
			"SetTokenMode",
			"SetTokenModeForTab",
			"Memory",
			"MemorySuggestions",
			"AcceptMemorySuggestion",
			"AcceptSkillSuggestion",
			"MemoryForTab",
			"MemorySuggestionsForTab",
			"AcceptMemorySuggestionForTab",
			"AcceptSkillSuggestionForTab",
			"Remember",
			"RememberForTab",
			"Forget",
			"ForgetForTab",
			"SaveDoc",
			"SaveDocForTab",
			"SetBypass",
			"OpenProjectTab",
			"OpenGlobalTab",
			"OpenTopicSession",
			"EnsureBlankTab",
			"ActivateTopic",
			"EnsureBlankSurface",
			"ListProjectTree",
			"CreateTopic",
			"RenameTopic",
			"DeleteTopic",
			"TrashTopic",
			"ContextPanel",
		},
	},
	{
		category: HostReadonly,
		methods: []string{
			"RemoteHostRuntimeSummary",
			"Commands",
			"Capabilities",
			"MCPServers",
			"InspectMCPTrust",
			"RefreshMCPCatalog",
			"SkillsSettings",
			"Plugins",
			"AvailableSubagentTools",
			"Models",
			"ModelsForTab",
			"Effort",
			"EffortForTab",
			// Remote must replace the current broad SettingsView with the
			// frozen, secret-free host/configSummary projection.
			"Settings",
		},
	},
	{
		category: DesktopLocal,
		methods: []string{
			"Platform",
			// Connection records, target lifecycle/diagnostics and AskPass are
			// owned by the Desktop SSH client. They are not Host RuntimeAPI
			// business methods. RemoteWorkbenchStatus is likewise the local
			// binding from an opaque Runtime target to a Desktop tab.
			"RemoteHosts",
			"SaveRemoteHost",
			"DeleteRemoteHost",
			"RemoteTargetStatus",
			"RemoteConnectionLogs",
			"ConnectRemoteHost",
			"ReconnectRemoteTarget",
			"SwitchToLocalTarget",
			"RespondRemoteAskPass",
			"RemoteWorkbenchStatus",
			"MinimiseMainWindow",
			"ToggleMaximiseMainWindow",
			"IsMainWindowMaximised",
			"CloseMainWindow",
			// Remote workspace selection gets a Host browser; this native
			// file-dialog binding remains a Local target overlay.
			"PickWorkspace",
			"AutoResearchOpenTask",
			"OpenWorkspacePath",
			"OpenWorkspacePathForTab",
			"ExternalOpeners",
			"SetPreferredExternalOpener",
			"OpenWorkspaceInExternalOpener",
			"OpenWorkspaceInExternalOpenerForTab",
			"RevealWorkspacePath",
			"RevealWorkspacePathForTab",
			"RevealPath",
			"PickExportFile",
			"SaveExportFile",
			"DesktopStartupSettings",
			"SetCloseBehavior",
			"SetDisplayMode",
			"SetStatusBarStyle",
			"SetStatusBarItems",
			"SetDesktopHistoryPageTurns",
			"SetDesktopLanguage",
			"SetDesktopAppearance",
			"SetDesktopLayoutStyle",
			"SetDesktopZoomFactor",
			"GetDesktopZoomFactor",
			"RestartApplication",
			"SetDesktopCheckUpdates",
			"SetDesktopTelemetry",
			"SetDesktopMetrics",
			"SetExpandThinking",
			"MigrateDesktopPreferences",
			"SetTrayLocale",
			"Version",
			"CheckUpdate",
			"DownloadUpdate",
			"InstallUpdate",
			"ApplyUpdate",
			"OpenDownloadPage",
			"NeedsOnboarding",
			"ReportCrash",
			"ListTabs",
			"SetActiveTab",
			"ReorderTabs",
			"CloseTab",
			"RenameProject",
			"SetProjectColor",
			"SetProjectPinned",
			"ReorderProjects",
			"SetTopicPinned",
			"ConfirmAction",
			"SaveWindowState",
		},
	},
	{
		category: DeferredV1,
		methods: []string{
			"GitBranches",
			"GitCheckout",
			"SavePastedImage",
			"SaveClipboardImage",
			"SavePastedFile",
			"AttachDropped",
			"AttachmentDataURL",
			"DeliveryWorktreeAvailability",
			"CreateDeliveryWorktree",
		},
	},
	{
		category: OutOfScope,
		methods: []string{
			"HeartbeatListTasks",
			"HeartbeatReloadTasks",
			"HeartbeatSaveTasks",
			"HeartbeatTriggerNow",
			"HeartbeatGenerateID",
			"OpenChannelSessionForTab",
			"OpenChannelSessionPageForTab",
			"SetMCPTrust",
			"CapabilityDiagnostics",
			"PlanPluginInstall",
			"InstallPlugin",
			"RemovePlugin",
			"SetPluginEnabled",
			"UpdatePlugin",
			"PluginDoctor",
			"AddMCPServer",
			"UpdateMCPServer",
			"RemoveMCPServer",
			"ReconnectMCPServer",
			"ClearMCPServerAuthentication",
			"PickSkillFolder",
			"PickPluginFolder",
			"AddSkillPath",
			"RemoveSkillPath",
			"RefreshSkills",
			"ReloadCommands",
			"SetSkillEnabled",
			"CreateSubagentProfile",
			"UpdateSubagentProfile",
			"DeleteSubagentProfile",
			"SetSubagentProfileModel",
			"SetSubagentProfileEffort",
			"TrySubagentProfile",
			"CancelTrySubagentProfile",
			"SetMCPServerEnabled",
			"SetMCPServerTier",
			"HooksSettings",
			"SaveHooksSettings",
			"SaveHooksSettingsForRoot",
			"TrustProjectHooks",
			"TrustProjectHooksForRoot",
			"SetDefaultModel",
			"SetPlannerModel",
			"SetSubagentModel",
			"SetSubagentEffort",
			"SetMaxSubagentDepth",
			"SetAutoPlan",
			"SetDefaultToolApprovalMode",
			"SaveProvider",
			"SaveProviderWithKey",
			"AddOfficialProviderAccess",
			"AddProviderPresetAccess",
			"ResetProviderPresetAccess",
			"FetchProviderModels",
			"DeleteProvider",
			"RemoveProviderAccess",
			"SaveProviderKey",
			"SetProviderKey",
			"ClearProviderKey",
			"SetPermissionMode",
			"AddPermissionRule",
			"RemovePermissionRule",
			"ReloadSettings",
			"SetSandbox",
			"SetNetwork",
			"SetBotSettings",
			"SetBotConnectionToolApprovalMode",
			"SetBotSecret",
			"ClearBotSecret",
			"StartBotConnectionInstall",
			"PollBotConnectionInstall",
			"BotRuntimeStatus",
			"DiagnoseBotConnection",
			"TestBotConnection",
			"SetMemoryCompilerEnabled",
			"SetAgentParams",
			"SetColdResumePrune",
			"SetReasoningLanguage",
			"ConnectKey",
		},
	},
}

// Entries returns an independent, method-sorted copy of the manifest.
func Entries() []Entry {
	var entries []Entry
	for _, group := range groups {
		for _, method := range group.methods {
			entries = append(entries, Entry{Method: method, Category: group.category})
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Method < entries[j].Method })
	return entries
}

// Validate checks that methods and manifest entries form an exact one-to-one
// mapping and that every entry uses a frozen category.
func Validate(entries []Entry, methods []string) error {
	validCategories := map[Category]bool{
		SharedRuntime: true,
		DesktopLocal:  true,
		HostReadonly:  true,
		DeferredV1:    true,
		OutOfScope:    true,
	}

	var problems []string
	classified := make(map[string]Category, len(entries))
	for _, entry := range entries {
		if strings.TrimSpace(entry.Method) == "" {
			problems = append(problems, "manifest contains an empty method")
			continue
		}
		if !validCategories[entry.Category] {
			problems = append(problems, fmt.Sprintf("%s has invalid category %q", entry.Method, entry.Category))
		}
		if previous, exists := classified[entry.Method]; exists {
			problems = append(problems, fmt.Sprintf("%s is classified more than once (%s, %s)", entry.Method, previous, entry.Category))
			continue
		}
		classified[entry.Method] = entry.Category
	}

	actual := make(map[string]bool, len(methods))
	for _, method := range methods {
		if strings.TrimSpace(method) == "" {
			problems = append(problems, "bridge contains an empty method")
			continue
		}
		if actual[method] {
			problems = append(problems, fmt.Sprintf("bridge method %s appears more than once", method))
		}
		actual[method] = true
		if _, exists := classified[method]; !exists {
			problems = append(problems, fmt.Sprintf("bridge method %s is unclassified", method))
		}
	}
	for method := range classified {
		if !actual[method] {
			problems = append(problems, fmt.Sprintf("manifest method %s is absent from the bridge", method))
		}
	}

	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	return fmt.Errorf("runtime parity manifest is invalid: %s", strings.Join(problems, "; "))
}
