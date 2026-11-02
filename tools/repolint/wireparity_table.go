package main

// The page's port files this half of the table reads. The other half's constants
// live in wireparity.go.
const (
	tsAdjudicationFile       = "desktop/frontend-next/src/port/adjudication.ts"
	tsAttachmentFile         = "desktop/frontend-next/src/port/attachment.ts"
	tsBackupFile             = "desktop/frontend-next/src/port/backup.ts"
	tsCompositionFile        = "desktop/frontend-next/src/port/composition.ts"
	tsContextCompositionFile = "desktop/frontend-next/src/port/context_composition.ts"
	tsHookFile               = "desktop/frontend-next/src/port/hook.ts"
	tsMarketFile             = "desktop/frontend-next/src/port/market.ts"
	tsMemoryFile             = "desktop/frontend-next/src/port/memory.ts"
	tsNetworkFile            = "desktop/frontend-next/src/port/network.ts"
	tsSkillFile              = "desktop/frontend-next/src/port/skill.ts"
	tsStorageFile            = "desktop/frontend-next/src/port/storage.ts"
	tsUsageFile              = "desktop/frontend-next/src/port/usage.ts"
)

// mirroredWireTypesTail is the second half of the registry whose first half is
// wireparity.go. It is a second file only because one table would take that
// file past the repository's line ceiling; the checker reads the two as one
// list, and every row is a page copy that agrees field for field with the Go
// struct its handler actually writes.
var mirroredWireTypesTail = []wireMirror{
	// account.ts: DeviceGrant is absent. /account/login writes a map literal
	// assembled from the grant, so the struct is not what the page reads, and a
	// guard over it would not see that response drift.

	// adjudication
	{"internal/frontend/serve/adjudications.go", "adjudicationItem", tsAdjudicationFile, "AdjudicationEntry"},
	{"internal/frontend/serve/adjudications.go", "adjudicationView", tsAdjudicationFile, "Adjudications"},
	// attachment
	{"internal/frontend/serve/drop.go", "droppedRef", tsAttachmentFile, "DroppedRef"},
	// backup
	{"internal/ext/configbackup/snapshot.go", "CategoryInfo", tsBackupFile, "BackupCategoryInfo"},
	{"internal/platform/account/backup.go", "Backup", tsBackupFile, "BackupEntry"},
	{"internal/ext/configbackup/snapshot.go", "Omission", tsBackupFile, "BackupOmission"},
	{"internal/frontend/serve/backup.go", "createBackupRequest", tsBackupFile, "BackupCreateRequest"},
	{"internal/ext/configbackup/paths.go", "PathRef", tsBackupFile, "BackupPathRef"},
	{"internal/ext/configbackup/plan.go", "PlanItem", tsBackupFile, "BackupPlanItem"},
	{"internal/ext/configbackup/plan.go", "Plan", tsBackupFile, "BackupPlan"},
	{"internal/ext/configbackup/apply.go", "PluginRef", tsBackupFile, "BackupPluginRef"},
	// boundary
	{"internal/session/control/config_problem.go", "ConfigProblem", tsBoundaryFile, "ConfigProblem"},
	{"internal/session/control/subagent_steer_settings.go", "SubagentSteerSettings", tsBoundaryFile, "SubagentSteerSettings"},
	{"internal/session/control/peer_mail_send.go", "PeerView", tsBoundaryFile, "PeerView"},
	{"internal/session/control/peer_mail.go", "PeerHeld", tsBoundaryFile, "PeerHeld"},
	{"internal/session/control/peer_threads.go", "PeerThread", tsBoundaryFile, "PeerThread"},
	{"internal/session/control/peer_mail_send.go", "PeerReceipt", tsBoundaryFile, "PeerReceipt"},
	{"internal/runtime/persona/presets.go", "Preset", tsBoundaryFile, "PersonaPreset"},
	{"internal/session/control/persona_settings.go", "PersonaSettings", tsBoundaryFile, "PersonaSettings"},
	{"internal/contract/config/unconverted_prices.go", "UnconvertedPrice", tsBoundaryFile, "UnconvertedPrice"},
	{"internal/session/control/turn_narration_settings.go", "TurnNarrationSettings", tsBoundaryFile, "TurnNarrationSettings"},
	{"internal/session/control/web_access_settings.go", "WebAccessSettings", tsBoundaryFile, "WebAccessSettings"},
	{"internal/tools/websearch/searxng_probe.go", "SearxngProbe", tsBoundaryFile, "SearxngProbe"},
	{"internal/session/control/feedback_level_notice.go", "FeedbackLevelNoticeSettings", tsBoundaryFile, "FeedbackLevelNotices"},
	// composition
	{"internal/contract/agentcomposition/composition.go", "Definition", tsCompositionFile, "AgentComposition"},
	{"internal/state/agentpreset/catalog.go", "Catalog", tsCompositionFile, "AgentPresetCatalog"},
	// context_composition
	{"internal/contract/event/notice_payload.go", "ContextComposition", tsContextCompositionFile, "ContextComposition"},
	// feedback
	{"internal/platform/feedback/link.go", "LinkedInstall", tsFeedbackFile, "FeedbackLinkedInstall"},
	// hook
	{"internal/frontend/serve/hooks.go", "hookEntry", tsHookFile, "HookEntry"},
	{"internal/frontend/serve/hooks.go", "hookSource", tsHookFile, "HookSource"},
	{"internal/frontend/serve/hooks.go", "hookEventInfo", tsHookFile, "HookEventInfo"},
	{"internal/ext/hook/edit.go", "DryRunResult", tsHookFile, "HookDryRun"},
	// host.ts is the Electron preload bridge, not kernel wire: its Refusal and
	// ViewRect are answered by the shell, so neither is a mirror of a Go struct.
	// hub
	{"internal/frontend/serve/hub_contract.go", "RuntimeView", tsHubFile, "RuntimeView"},
	{"internal/platform/openwith/openwith.go", "Status", tsHubFile, "OpenWithTarget"},
	{"internal/frontend/serve/history_search.go", "historySearchHit", tsHubFile, "HistoryHit"},
	// look
	{"internal/frontend/serve/appearance.go", "wallpaperLayerView", tsLookFile, "WallpaperLayer"},
	{"internal/frontend/serve/appearance.go", "wallpaperAudioView", tsLookFile, "WallpaperAudioTrack"},
	// market
	// MarketList is absent: /market/packages assembles a map, so market.Page is not the writer.
	{"internal/frontend/serve/market.go", "marketInstalled", tsMarketFile, "MarketInstalled"},
	{"internal/ext/market/client.go", "Version", tsMarketFile, "MarketVersion"},
	{"internal/ext/market/cache.go", "CacheNote", tsMarketFile, "MarketCache"},
	{"internal/frontend/serve/market.go", "marketDetailView", tsMarketFile, "MarketDetail"},
	{"internal/ext/market/install.go", "Request", tsMarketFile, "MarketRequest"},
	{"internal/ext/market/publish.go", "Submission", tsMarketFile, "MarketSubmission"},
	{"internal/ext/market/publish.go", "Published", tsMarketFile, "MarketPublished"},
	// mcp
	// McpDraft is absent: /mcp/parse assembles a map of servers and risks.
	{"internal/frontend/serve/catalog.go", "mcpTool", tsMcpFile, "McpTool"},
	{"internal/session/control/capability_scope.go", "CapabilityScope", tsMcpFile, "CapabilityScope"},
	{"internal/frontend/serve/catalog.go", "draftServer", tsMcpFile, "McpDraftServer"},
	{"internal/frontend/serve/catalog.go", "draftRisk", tsMcpFile, "McpRisk"},
	{"internal/ext/plugin/install.go", "MCPInstallResult", tsMcpFile, "McpInstallResult"},
	// memory
	{"internal/frontend/serve/memory.go", "memoryEntry", tsMemoryFile, "MemoryEntry"},
	{"internal/frontend/serve/memory.go", "unreadableMemory", tsMemoryFile, "UnreadableMemory"},
	{"internal/frontend/serve/memory.go", "editable", tsMemoryFile, "MemoryEdit"},
	// model
	{"internal/session/control/slash.go", "SlashItem", tsModelFile, "CompletionItem"},
	{"internal/session/control/slash.go", "SlashArg", tsModelFile, "CompletionArg"},
	{"internal/session/control/complete.go", "Completion", tsModelFile, "Completion"},
	{"internal/frontend/serve/modelcaps.go", "modelPrice", tsModelFile, "ModelPrice"},
	// network
	{"internal/session/control/network.go", "NetworkSettings", tsNetworkFile, "NetworkSettings"},
	{"internal/base/netclient/diagnose.go", "Probe", tsNetworkFile, "NetworkProbe"},
	// plugin
	{"internal/frontend/serve/plugins.go", "pluginItem", tsPluginFile, "PluginItem"},
	{"internal/frontend/serve/plugins.go", "pluginHook", tsPluginFile, "PluginHook"},
	{"internal/frontend/serve/plugins.go", "pluginServer", tsPluginFile, "PluginServer"},
	{"internal/frontend/serve/plugins.go", "pluginRuntime", tsPluginFile, "PluginRuntime"},
	{"internal/ext/pluginpkg/pluginpkg.go", "CompatibilityIssue", tsPluginFile, "PluginSkipped"},
	{"internal/frontend/serve/plugins.go", "pluginView", tsPluginFile, "PluginPackage"},
	// port
	{"internal/session/control/input.go", "InvocationRequest", tsPortFile, "InvocationRequest"},
	{"internal/frontend/serve/tray.go", "TrayPrefs", tsPortFile, "TrayPrefs"},
	// provider
	{"internal/frontend/serve/provider_check.go", "providerModelCheckRequest", tsProviderFile, "ProviderModelCheckRequest"},
	{"internal/frontend/serve/provider_edit.go", "providerEditBody", tsProviderFile, "ProviderEdit"},
	{"internal/frontend/serve/provider_model_prices.go", "modelPriceView", tsProviderFile, "ModelRate"},
	{"internal/frontend/serve/provider_model_limits.go", "modelLimitsView", tsProviderFile, "ModelLimit"},
	{"internal/frontend/serve/provider_model_effort.go", "modelEffortView", tsProviderFile, "ModelEffort"},
	{"internal/frontend/serve/provider_setup.go", "providerSetupState", tsProviderFile, "ProviderSetup"},
	// remote
	{"internal/frontend/serve/hub_remote.go", "RemoteProbeRoute", tsRemoteFile, "RemoteRoute"},
	{"internal/frontend/serve/hub_remote.go", "RemoteProbe", tsRemoteFile, "RemoteProbe"},
	// session
	{"internal/state/checkpoint/types.go", "FileStage", tsSessionFile, "RewindFileStage"},
	{"internal/session/control/approval_orchestration.go", "PostureReport", tsSessionFile, "ApprovalDefault"},
	{"internal/frontend/serve/balance.go", "walletLine", tsSessionFile, "WalletLine"},
	{"internal/frontend/serve/balance.go", "walletBody", tsSessionFile, "WalletReading"},
	// share
	{"internal/frontend/serve/cloud_presence.go", "CloudControllerView", tsShareFile, "CloudDevice"},
	{"internal/frontend/serve/device_share.go", "CloudShareOffer", tsShareFile, "CloudShareOffer"},
	// shell
	{"internal/session/control/shell.go", "ShellOption", tsShellFile, "ShellOption"},
	{"internal/session/control/shell.go", "ShellSettings", tsShellFile, "ShellSettings"},
	// skill
	{"internal/frontend/serve/catalog.go", "skillEntry", tsSkillFile, "SkillEntry"},
	{"internal/ext/skillupdate/skillupdate.go", "Change", tsSkillFile, "SkillUpdateChange"},
	{"internal/ext/skillupdate/skillupdate.go", "Plan", tsSkillFile, "SkillUpdatePlan"},
	{"internal/frontend/serve/skill_source.go", "skillSource", tsSkillFile, "SkillSource"},
	// storage
	{"internal/frontend/serve/storage.go", "storageRoot", tsStorageFile, "StorageRoot"},
	{"internal/frontend/serve/storage.go", "storageMove", tsStorageFile, "StorageMove"},
	{"internal/frontend/serve/storage.go", "storageRefusal", tsStorageFile, "StorageRefusal"},
	{"internal/frontend/serve/storage.go", "storagePlan", tsStorageFile, "StoragePlan"},
	// usage
	{"internal/contract/pricing/money.go", "Money", tsUsageFile, "Money"},
	{"internal/state/stats/query.go", "DailyTokens", tsUsageFile, "UsageDay"},
	{"internal/state/stats/query.go", "HourlyTokens", tsUsageFile, "UsageHour"},
	{"internal/state/stats/query.go", "ModelUsage", tsUsageFile, "UsageModel"},
	{"internal/state/stats/query.go", "ProviderUsage", tsUsageFile, "UsageProvider"},
	{"internal/state/stats/query.go", "RangeStats", tsUsageFile, "UsageReport"},
	{"internal/state/stats/upload.go", "UploadDay", tsUsageFile, "UploadDay"},
	{"internal/state/stats/upload.go", "UploadProvider", tsUsageFile, "UploadProvider"},
	{"internal/state/stats/upload.go", "UploadStats", tsUsageFile, "UploadReport"},
	// wire
	{"internal/contract/eventwire/wire.go", "Bound", tsWireFile, "Bound"},
	{"internal/contract/provider/argument_normalization.go", "ArgumentNormalization", tsWireFile, "ArgumentNormalization"},
	{"internal/contract/pricing/money.go", "Money", tsWireFile, "Money"},
	{"internal/contract/eventwire/wire.go", "Via", tsWireFile, "Via"},
	{"internal/contract/eventwire/wire.go", "AskOption", tsWireFile, "AskOption"},
	{"internal/contract/eventwire/wire.go", "Ask", tsWireFile, "Ask"},
	{"internal/contract/eventwire/extension.go", "ExtensionStatus", tsWireFile, "ExtensionStatus"},
	{"internal/contract/eventwire/extension.go", "ExtensionKeyValue", tsWireFile, "ExtensionKeyValue"},
	{"internal/contract/eventwire/extension.go", "ExtensionActionRef", tsWireFile, "ExtensionActionRef"},
	{"internal/contract/eventwire/extension.go", "ExtensionCard", tsWireFile, "ExtensionCard"},
	{"internal/contract/eventwire/extension.go", "ExtensionFormField", tsWireFile, "ExtensionFormField"},
	{"internal/contract/eventwire/extension.go", "ExtensionForm", tsWireFile, "ExtensionForm"},
	{"internal/contract/eventwire/extension.go", "ExtensionPanel", tsWireFile, "ExtensionPanel"},
	{"internal/contract/eventwire/extension.go", "ExtensionNotification", tsWireFile, "ExtensionNotification"},
	{"internal/contract/eventwire/extension.go", "ExtensionView", tsWireFile, "ExtensionView"},
	{"internal/contract/eventwire/wire.go", "StreamAttempt", tsWireFile, "StreamAttempt"},
	{"internal/contract/eventwire/wire.go", "MemoryCitation", tsWireFile, "MemoryCitation"},
	{"internal/contract/eventwire/receipt.go", "CompletionReceipt", tsWireFile, "Receipt"},
	{"internal/contract/eventwire/receipt.go", "ReceiptChange", tsWireFile, "ReceiptChange"},
	{"internal/contract/eventwire/receipt.go", "ReceiptVerification", tsWireFile, "ReceiptVerification"},
	{"internal/contract/eventwire/receipt.go", "ReceiptGap", tsWireFile, "ReceiptGap"},
	// workspace
	{"internal/platform/gitstatus/gitstatus.go", "Change", tsWorkspaceFile, "WorkspaceChange"},
	{"internal/frontend/serve/workspace_listing.go", "workspaceListing", tsWorkspaceFile, "WorkspaceFiles"},
	// WorkspaceEntry is absent: no Go writer sends that pair; its fields only coincide with unrelated folder types.
}
