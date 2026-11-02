package main

import (
	"fmt"
	"go/ast"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
)

// Where the desktop re-declares the kernel's wire types by hand.
const (
	tsWireFile      = "desktop/frontend-next/src/port/wire.ts"
	tsPortFile      = "desktop/frontend-next/src/port/port.ts"
	tsLeaseFile     = "desktop/frontend-next/src/port/workspace_lease.ts"
	tsBoundaryFile  = "desktop/frontend-next/src/port/boundary.ts"
	tsRemoteFile    = "desktop/frontend-next/src/port/remote.ts"
	tsSessionFile   = "desktop/frontend-next/src/port/session.ts"
	tsSubagentFile  = "desktop/frontend-next/src/port/subagent.ts"
	tsVersionFile   = "desktop/frontend-next/src/port/version.ts"
	tsModelFile     = "desktop/frontend-next/src/port/model.ts"
	tsMcpFile       = "desktop/frontend-next/src/port/mcp.ts"
	tsScheduleFile  = "desktop/frontend-next/src/port/schedule.ts"
	tsReplayFile    = "desktop/frontend-next/src/port/surface_replay.ts"
	tsShellFile     = "desktop/frontend-next/src/port/shell.ts"
	tsShareFile     = "desktop/frontend-next/src/port/share.ts"
	tsProviderFile  = "desktop/frontend-next/src/port/provider.ts"
	tsFeedbackFile  = "desktop/frontend-next/src/port/feedback.ts"
	tsWorkspaceFile = "desktop/frontend-next/src/port/workspace.ts"
	tsLookFile      = "desktop/frontend-next/src/port/look.ts"
	tsHubFile       = "desktop/frontend-next/src/port/hub.ts"
	tsMuxFile       = "desktop/frontend-next/src/port/mux.ts"
	tsPluginFile    = "desktop/frontend-next/src/port/plugin.ts"
	tsChartFile     = "desktop/frontend-next/src/ui/chart/spec.ts"
	tsSurfaceFile   = "desktop/frontend-next/src/ui/surface/spec.ts"
)

// tuiInboxFile is where the terminal re-declares the kernel's inbox wire types
// by hand. One file holds both the listing and its entries.
const tuiInboxFile = "internal/frontend/tui/client_inbox.go"

// mirroredWireTypes are the Go types the desktop keeps a second, hand-written
// copy of: the graph, whose consumers all fold the same deltas, and the sandbox
// editor, which draws a security posture a field it cannot read would get
// wrong. Declared and not inferred: no spelling tells a mirrored contract from
// a struct with json tags. Two files only so neither outgrows the line ceiling.
var mirroredWireTypes = slices.Concat(mirroredWireTypesHead, mirroredWireTypesTail)

// mirroredWireTypesHead is the table's first half; wireparity_table.go holds
// the rest.
var mirroredWireTypesHead = []wireMirror{
	{"internal/frontend/serve/workspace_files.go", "workspaceFile", tsWorkspaceFile, "WorkspaceFile"},
	{"internal/base/workspacefile/file.go", "Ref", tsWorkspaceFile, "FileRef"},
	{"internal/base/workspacefile/file.go", "Info", tsWorkspaceFile, "PreviewInfo"},
	{"internal/contract/delivery/delivery.go", "File", tsWorkspaceFile, "DeliveredFile"},
	{"internal/frontend/serve/side.go", "SideProgress", "desktop/frontend-next/src/port/side.ts", "SideProgress"},
	{"internal/platform/worktree/source.go", "Source", tsWorkspaceFile, "WorktreeSource"},
	{"internal/frontend/serve/workspace_source.go", "worktreeSourcesView", tsWorkspaceFile, "WorktreeSources"},
	{"internal/frontend/serve/workspace_source.go", "worktreeSelection", tsWorkspaceFile, "WorktreeSelection"},
	{"internal/session/control/workspace_preparation.go", "WorkspacePreparationStatus", tsWorkspaceFile, "WorktreePreparation"},
	{"internal/contract/surfacespec/action_message.go", "Action", "desktop/frontend-next/src/port/surface.ts", "SurfaceAction"},
	{"internal/session/control/surface_actions.go", "SurfaceActionRequest", "desktop/frontend-next/src/port/surface.ts", "SurfaceActionRequest"},
	{"internal/session/control/surface_actions.go", "SurfaceActionRecord", "desktop/frontend-next/src/port/surface.ts", "SurfaceActionRecord"},
	{"internal/contract/event/event.go", "AskAnswer", tsWireFile, "AskAnswer"},
	{"internal/contract/eventwire/ask.go", "AskQuestion", tsWireFile, "AskQuestion"},
	{"internal/contract/surfacespec/spec.go", "Spec", tsSurfaceFile, "SurfaceSpec"},
	{"internal/contract/surfacespec/spec.go", "Node", tsWireFile, "ExtensionViewNode"},
	{"internal/contract/surfacespec/spec.go", "Field", tsWireFile, "SurfaceField"},
	{"internal/contract/surfacespec/spec.go", "Diff", tsWireFile, "SurfaceDiff"},
	{"internal/contract/surfacespec/spec.go", "Option", tsWireFile, "SurfaceOption"},
	{"internal/contract/surfacespec/image.go", "Image", "desktop/frontend-next/src/port/surface-rich.ts", "SurfaceImage"},
	{"internal/contract/surfacespec/rich.go", "Location", "desktop/frontend-next/src/port/surface-rich.ts", "SurfaceLocation"},
	{"internal/contract/surfacespec/rich.go", "Fact", "desktop/frontend-next/src/port/surface-rich.ts", "SurfaceFact"},
	{"internal/contract/surfacespec/rich.go", "Dataset", "desktop/frontend-next/src/port/surface-rich.ts", "SurfaceDataset"},
	{"internal/contract/surfacespec/rich.go", "Series", "desktop/frontend-next/src/port/surface-rich.ts", "SurfaceSeries"},

	{"internal/contract/eventwire/extension.go", "ExtensionViewNode", tsWireFile, "ExtensionViewNode"},
	{"internal/contract/eventwire/extension.go", "ExtensionSurface", tsWireFile, "ExtensionSurface"},
	{"internal/frontend/serve/surface_history.go", "surfaceReplay", tsReplayFile, "ExtensionSurfaceReplay"},
	{"internal/contract/event/notice_payload.go", "DormantPermissionRule", tsBoundaryFile, "DormantPermissionRule"},
	{"internal/frontend/serve/branch.go", "workspaceBranchesView", tsWorkspaceFile, "WorkspaceBranches"},
	{"internal/platform/gitstatus/branches.go", "Branch", tsWorkspaceFile, "WorkspaceBranch"},
	{"internal/frontend/serve/changes.go", "workspaceGitView", tsWorkspaceFile, "WorkspaceGit"},
	{"internal/frontend/serve/changes.go", "gitHistoryView", tsWorkspaceFile, "GitHistory"},
	{"internal/platform/gitstatus/history.go", "Commit", tsWorkspaceFile, "GitCommit"},
	{"internal/contract/eventwire/wire.go", "AskOrigin", tsWireFile, "AskOrigin"},
	{"internal/session/control/goal_runtime_view.go", "GoalRuntimeView", tsSessionFile, "GoalRuntime"},
	{"internal/contract/agentgraph/graph.go", "Node", tsWireFile, "GraphNode"},
	{"internal/contract/agentgraph/graph.go", "Edge", tsWireFile, "GraphEdge"},
	{"internal/contract/agentgraph/graph.go", "Delta", tsWireFile, "GraphDelta"},
	// The snapshot is the execution model's whole authority: no delta carries an
	// interruption or an unrecorded identity, and a watermark the page cannot
	// find is a bootstrap that resumes from zero.
	{"internal/contract/agentgraph/graph.go", "Graph", tsWireFile, "ExecutionGraph"},
	{"internal/session/control/execution_graph.go", "ExecutionGraphSnapshot", tsWireFile, "ExecutionGraphSnapshot"},
	{"internal/session/control/execution_graph.go", "ExecutionInterruption", tsWireFile, "ExecutionInterruption"},
	{"internal/frontend/serve/executiongraph.go", "executionGraphView", tsWireFile, "ExecutionGraphView"},
	// Whether a replayed trajectory covers the session or only a prefix of it.
	// A page that cannot read the coverage is back to the empty array that meant
	// three different things.
	{"internal/frontend/serve/wirelog.go", "trajectoryView", tsWireFile, "TrajectoryRead"},
	// The tool frame itself. Who issued a call is the one thing separating the
	// model's work from the host's own bookkeeping, and a page that cannot read
	// it folds them together.
	{"internal/contract/eventwire/wire.go", "Tool", tsWireFile, "Tool"},
	// The stored files a message references. The host resolved them; a page
	// that cannot read one has to guess a preview from the reference's spelling.
	{"internal/contract/eventwire/attachment.go", "Attachment", tsWireFile, "StoredAttachment"},
	// The canonical task list, which the panel reads rather than derives. A
	// status the page cannot read is a signed-off step drawn as the current one.
	{"internal/frontend/serve/todos.go", "todoItem", tsSessionFile, "HostTodo"},
	{"internal/frontend/serve/session_usage.go", "SessionUsage", tsSessionFile, "SessionUsage"},
	{"internal/frontend/serve/jobs.go", "JobOutput", tsSessionFile, "JobOutput"},
	{"internal/frontend/serve/history.go", "ToolOutputPage", tsSessionFile, "ToolOutputPage"},
	// The background-job list. A name or full command the page cannot read is
	// a task it can only call by its short preview.
	{"internal/tools/jobs/jobs_view.go", "View", tsSessionFile, "JobEntry"},
	{"internal/frontend/serve/subagents.go", "subagentRunView", tsSubagentFile, "SubagentRun"},
	{"internal/frontend/serve/subagents.go", "subagentTranscriptView", tsSubagentFile, "SubagentTranscript"},
	{"internal/frontend/serve/session_usage.go", "SourceUsage", tsSessionFile, "SourceUsage"},
	// The agent's tabs. The target id is what the window draws a view by, so a
	// tab the page cannot read one for is a tab it cannot show.
	{"internal/platform/browser/session.go", "TabInfo", tsSessionFile, "BrowserTab"},
	// A rebuilt transcript's tool calls. The proxy's resolved identity was on
	// the kernel side and not on this one, so a reopened session drew
	// use_capability where the live one drew what it reached.
	{"internal/frontend/serve/history.go", "historyToolCall", tsSessionFile, "HistoryToolCall"},
	{"internal/frontend/serve/history.go", "historyMessage", tsSessionFile, "HistoryMessage"},
	{"internal/frontend/serve/history_page.go", "historyPageResponse", tsSessionFile, "HistoryPage"},
	{"internal/frontend/serve/history_page.go", "historyPageToolContext", tsSessionFile, "HistoryToolContext"},
	// Who can reach this window from another device. A device the page cannot
	// list is one the person at the window cannot see to disconnect.
	{"internal/frontend/serve/device_share.go", "ShareStatus", tsShareFile, "ShareStatus"},
	{"internal/frontend/serve/device_share.go", "CloudRemoteStatus", tsShareFile, "CloudRemoteStatus"},
	{"internal/frontend/serve/device_share.go", "ShareAddress", tsShareFile, "ShareAddress"},
	{"internal/frontend/serve/device_share.go", "ShareOffer", tsShareFile, "ShareOffer"},
	{"internal/frontend/serve/device_registry.go", "DeviceView", tsShareFile, "PairedDevice"},
	{"internal/frontend/serve/device_gate.go", "DeviceSelf", tsShareFile, "DeviceSelf"},
	{"internal/session/control/boundary.go", "PermissionLists", tsBoundaryFile, "PermissionLists"},
	{"internal/session/control/boundary.go", "PermissionRules", tsBoundaryFile, "PermissionRules"},
	{"internal/session/control/boundary.go", "SandboxSettings", tsBoundaryFile, "SandboxSettings"},
	{"internal/frontend/serve/workspace_roots.go", "workspaceRootsView", tsBoundaryFile, "WorkspaceRoots"},
	{"internal/session/control/browser_settings.go", "BrowserToolsSettings", tsBoundaryFile, "BrowserToolsSettings"},
	{"internal/session/control/subagent_claims.go", "SubagentClaimsSettings", tsBoundaryFile, "SubagentClaimsSettings"},
	{"internal/session/control/write_lease_settings.go", "WriteLeaseSettings", tsBoundaryFile, "WriteLeaseSettings"},
	{"internal/session/control/plan_approval_settings.go", "PlanApprovalSettings", tsBoundaryFile, "PlanApprovalSettings"},
	{"internal/session/control/default_mode_settings.go", "DefaultModeSettings", tsBoundaryFile, "DefaultModeSettings"},
	{"internal/session/control/present_files_settings.go", "PresentFilesSettings", tsBoundaryFile, "PresentFilesSettings"},
	{"internal/session/control/tool_settings.go", "ToolSettings", tsBoundaryFile, "ToolSettings"},
	{"internal/session/control/tool_settings.go", "ToolSetting", tsBoundaryFile, "ToolSetting"},
	{"internal/session/control/peer_settings.go", "PeersSettings", tsBoundaryFile, "PeersSettings"},
	{"internal/session/control/peer_settings.go", "PeerReleased", tsBoundaryFile, "PeerProject"},
	{"internal/session/control/mods.go", "ModStatus", tsBoundaryFile, "ModStatus"},
	{"internal/session/control/mods.go", "ModRuntime", tsBoundaryFile, "ModRuntime"},
	{"internal/session/control/mods.go", "ModsSettings", tsBoundaryFile, "ModsSettings"},
	{"internal/ext/modpreview/types.go", "Preview", tsPluginFile, "ModPreview"},
	{"internal/ext/modpreview/types.go", "Event", tsPluginFile, "ModPreviewEvent"},
	{"internal/ext/modpreview/types.go", "Call", tsPluginFile, "ModPreviewCall"},
	{"internal/ext/modpreview/types.go", "Import", tsPluginFile, "ModPreviewImport"},
	{"internal/ext/modpreview/types.go", "Finding", tsPluginFile, "ModPreviewFinding"},
	// The MCP row: a status the host answered with and the page cannot read is a
	// row that goes back to reading the server's own prose for it.
	{"internal/frontend/serve/catalog.go", "mcpEntry", tsMcpFile, "McpEntry"},
	{"internal/frontend/serve/catalog.go", "mcpHeld", tsMcpFile, "McpHeld"},
	// The fold bounds and the one in force: a field the panel cannot read is a
	// threshold a user sets and never sees applied.
	{"internal/session/control/compaction_settings.go", "CompactionSettings", tsModelFile, "CompactionSettings"},
	{"internal/session/control/model_modes.go", "ModelModeView", tsModelFile, "ModelMode"},
	// The completion summary is the turn's own verdict on itself; a gap kind the
	// desktop cannot read is a turn it shows as clean.
	{"internal/contract/eventwire/wire.go", "CompletionSummary", tsWireFile, "CompletionSummary"},
	// A maintenance verdict the desktop cannot read is a session that shows no
	// attempt to bound its context — which is what its trajectory export said
	// about the run that motivated the second boundary.
	{"internal/contract/eventwire/wire.go", "ContextMaintenance", tsWireFile, "ContextMaintenance"},
	// The gauge and the two ceilings behind it. A panel that cannot read which
	// bound fires draws a fold point at 16% of the declared window with nothing
	// on the screen that explains it, which is what it read as.
	{"internal/frontend/serve/context_window.go", "contextView", tsShellFile, "ContextBreakdown"},
	// The holders a class can be opened to; a field the panel cannot read is a
	// heavy class it cannot explain.
	{"internal/runtime/agent/context_breakdown.go", "ContextHolder", tsShellFile, "ContextHolder"},
	// What sent one fold. Without the boundary and its size the card falls back
	// to "a threshold was reached", and the reader supplies their own reason.
	{"internal/contract/eventwire/wire.go", "Compaction", tsWireFile, "Compaction"},
	// Plan rewriting and plan advancement are told apart by three counters; a
	// desktop that can read only some of them reads churn as work.
	{"internal/contract/eventwire/wire.go", "TodoProgress", tsWireFile, "TodoProgress"},
	// A stall report a desktop reads only half of says "stalled" without the
	// count or the cause, and the reader invents one.
	{"internal/contract/eventwire/progress_watch.go", "ProgressWatch", tsWireFile, "ProgressWatch"},
	{"internal/session/control/display_currency.go", "DisplayCurrencySettings", tsBoundaryFile, "DisplayCurrencySettings"},
	{"internal/session/control/progress_watch_settings.go", "ProgressWatchSettings", tsBoundaryFile, "ProgressWatchSettings"},
	{"internal/session/control/changed_files_settings.go", "ChangedFilesSettings", tsBoundaryFile, "ChangedFilesSettings"},
	{"internal/session/control/observability_settings.go", "ObservabilitySettings", tsBoundaryFile, "ObservabilitySettings"},
	{"internal/session/control/schedules.go", "ScheduleDraft", tsScheduleFile, "ScheduleDraft"},
	{"internal/session/control/schedules.go", "ScheduleView", tsScheduleFile, "ScheduleView"},
	{"internal/session/control/schedules.go", "ScheduleRunView", tsScheduleFile, "ScheduleRunView"},
	{"internal/session/control/schedules.go", "SchedulePolicyView", tsScheduleFile, "SchedulePolicyView"},
	{"internal/session/control/schedules.go", "ScheduleBoard", tsScheduleFile, "ScheduleBoard"},
	{"internal/session/control/schedules.go", "ScheduleFolder", tsScheduleFile, "ScheduleFolder"},
	{"internal/session/control/schedules.go", "ScheduleCurrent", tsScheduleFile, "ScheduleCurrent"},
	{"internal/session/control/schedules.go", "SchedulePreview", tsScheduleFile, "SchedulePreview"},
	{"internal/session/control/schedules.go", "SchedulePendingView", tsScheduleFile, "SchedulePendingView"},
	{"internal/session/control/schedules.go", "ScheduleResultView", tsScheduleFile, "ScheduleResultView"},
	{"internal/session/control/fold_work_settings.go", "FoldWorkSettings", tsBoundaryFile, "FoldWorkSettings"},
	{"internal/session/control/ask_timeout.go", "AskTimeoutSettings", tsBoundaryFile, "AskTimeoutSettings"},
	{"internal/session/control/session_ref_settings.go", "SessionRefSettings", tsBoundaryFile, "SessionRefSettings"},
	{"internal/session/control/auto_archive.go", "AutoArchiveSettings", tsBoundaryFile, "AutoArchiveSettings"},
	{"internal/session/control/receipt_settings.go", "ReceiptSettings", tsBoundaryFile, "ReceiptSettings"},
	{"internal/frontend/serve/notifications.go", "NotifyPrefs", tsPortFile, "NotifyPrefs"},
	{"internal/frontend/serve/notifications.go", "PhoneNotifyPrefs", tsPortFile, "PhoneNotifyPrefs"},
	{"internal/frontend/serve/share_phones.go", "PhoneView", tsShareFile, "PhoneView"},
	{"internal/frontend/serve/share_phones.go", "PhoneList", tsShareFile, "PhoneList"},
	// RemoteHostEdit is left out on purpose: the kernel still takes the single
	// `workspace` an old row was saved with, which the page deliberately does
	// not send. An under-filled request is not a picture that cannot be read.
	{"internal/frontend/serve/hub_remote.go", "RemoteHostView", tsRemoteFile, "RemoteHost"},
	{"internal/frontend/serve/remote_browse.go", "RemoteListing", tsRemoteFile, "RemoteListing"},
	{"internal/frontend/serve/remote_browse.go", "RemoteFolder", tsRemoteFile, "RemoteFolder"},
	{"internal/frontend/serve/remote_book.go", "WSLDistros", tsRemoteFile, "WSLDistros"},
	{"internal/frontend/serve/remote_book.go", "WSLDistro", tsRemoteFile, "WSLDistro"},
	// The sidebar row. The unread mark is derived by the kernel from two stored
	// timestamps; a row the page cannot read it from shows a finished turn as seen.
	{"internal/frontend/serve/hub_mux.go", "muxOpened", tsMuxFile, "MuxOpened"},
	{"internal/frontend/serve/hub_mux.go", "muxAttachRequest", tsMuxFile, "MuxAttachRequest"},
	{"internal/frontend/serve/hub_mux.go", "muxEnvelopeView", tsMuxFile, "MuxEnvelope"},
	{"internal/frontend/serve/hub_tree_types.go", "treeSession", tsHubFile, "TreeSession"},
	// The folder's true size beside the rows loaded. A window that cannot read
	// the total labels a truncated list as the whole history.
	{"internal/frontend/serve/hub_tree_types.go", "treeWorkspace", tsHubFile, "TreeWorkspace"},
	// A session the import left in place. The reason code is what the window
	// words the explanation from; a code it cannot read is a skipped session
	// reported as nothing more than a count.
	{"internal/frontend/serve/hub_tree.go", "legacySkipView", tsHubFile, "LegacySkip"},
	{"internal/frontend/serve/hub_tree.go", "legacyImportView", tsHubFile, "LegacyImport"},
	// What waits on the user, and which call answers it. The desktop reads this
	// list as the whole set of open prompts — one it cannot read is a card it
	// seals as decided while the run stays blocked on it.
	{"internal/session/control/decisions.go", "Decision", tsSessionFile, "Decision"},
	{"internal/session/control/decisions.go", "DecisionQuestion", tsSessionFile, "DecisionQuestion"},
	{"internal/session/control/decisions.go", "DecisionOption", tsSessionFile, "DecisionOption"},
	// How a window learns a prompt was answered in another one. A field it
	// cannot read is a card left answerable over a decision already made.
	{"internal/contract/eventwire/wire.go", "DecisionReceipt", tsWireFile, "DecisionReceipt"},
	// What an install in flight is doing. The page polls it and renders one
	// sentence per phase; a phase it cannot read renders as nothing, which on
	// the long pause after the last byte is indistinguishable from a hang.
	{"internal/platform/update/progress.go", "Progress", tsVersionFile, "UpdateProgress"},
	// The panel lists these and offers a notes view only for a row that says it
	// has them; a field it cannot read is a row that never offers one.
	{"internal/platform/update/hub.go", "VersionEntry", tsVersionFile, "VersionEntry"},
	{"internal/platform/update/hub.go", "VersionHub", tsVersionFile, "VersionHub"},
	{"internal/platform/update/hub.go", "WithdrawnNote", tsVersionFile, "WithdrawnNote"},
	{"internal/platform/update/notes.go", "VersionNotes", tsVersionFile, "VersionNotes"},
	// Where the logs and crash files are. A field the page cannot read is a path
	// it cannot show to someone asked where to look.
	{"internal/frontend/serve/studio_logs.go", "studioLogs", tsVersionFile, "StudioLogs"},
	{"internal/frontend/serve/studio_logs.go", "studioCrashes", tsVersionFile, "StudioCrash"},
	{"internal/state/checkpoint/types.go", "RewindResult", tsSessionFile, "RewindResult"},
	{"internal/state/checkpoint/types.go", "RewindUndo", tsSessionFile, "RewindUndo"},
	{"internal/contract/eventwire/wire.go", "ShellExecution", tsWireFile, "Execution"},
	{"internal/contract/eventwire/workspace_lease.go", "WorkspaceLease", tsLeaseFile, "WorkspaceLease"},
	// A source and a model as the pickers name them. A label the page cannot
	// read puts the config name back on screen after the user renamed it.
	{"internal/frontend/serve/providers.go", "providerView", tsProviderFile, "ProviderEntry"},
	{"internal/frontend/serve/providers.go", "protocolView", tsProviderFile, "Protocol"},
	{"internal/frontend/serve/provider_presets.go", "providerPresetView", tsProviderFile, "ProviderPreset"},
	{"internal/frontend/serve/provider_check.go", "providerCheck", tsProviderFile, "ProviderCheck"},
	{"internal/model/providerhealth/check.go", "Reading", tsProviderFile, "ProviderHealth"},
	{"internal/session/control/provider_health.go", "ProviderHealthSettings", tsProviderFile, "ProviderHealthSettings"},
	{"internal/frontend/serve/providers.go", "providerRemovalView", tsProviderFile, "ProviderRemoval"},
	{"internal/frontend/serve/provider_check.go", "providerModelCheck", tsProviderFile, "ProviderModelCheck"},
	{"internal/frontend/serve/settings.go", "modelEntry", tsModelFile, "ModelEntry"},
	{"internal/frontend/serve/roles.go", "rolesResponse", tsModelFile, "RoleAssignments"},
	// A per-profile entry that outranks a role's global model. A scope the page
	// cannot read offers a clear the kernel will refuse.
	{"internal/frontend/serve/roles.go", "roleOverride", tsModelFile, "RoleOverride"},
	// The report form draws what the kernel will send and reads back what
	// became of it; a field the page cannot read is a status it cannot show.
	{"internal/platform/feedback/types.go", "Env", tsFeedbackFile, "FeedbackEnvInfo"},
	{"internal/platform/feedback/env_memory.go", "ProcessMemory", tsFeedbackFile, "FeedbackProcessMemory"},
	{"internal/platform/feedback/types.go", "Limits", tsFeedbackFile, "FeedbackLimits"},
	{"internal/platform/feedback/types.go", "Receipt", tsFeedbackFile, "FeedbackReceipt"},
	{"internal/platform/feedback/types.go", "Item", tsFeedbackFile, "FeedbackItem"},
	{"internal/platform/feedback/types.go", "Reply", tsFeedbackFile, "FeedbackReply"},
	{"internal/platform/feedback/types.go", "ReplyReceipt", tsFeedbackFile, "FeedbackReplyReceipt"},
	{"internal/platform/feedback/types.go", "Mine", tsFeedbackFile, "FeedbackMine"},
	{"internal/platform/feedback/types.go", "Promotion", tsFeedbackFile, "FeedbackPromotion"},
	{"internal/platform/feedback/board.go", "Board", tsFeedbackFile, "FeedbackBoard"},
	{"internal/platform/feedback/board.go", "BoardEntry", tsFeedbackFile, "FeedbackBoardEntry"},
	{"internal/platform/feedback/board.go", "BoardRelease", tsFeedbackFile, "FeedbackBoardRelease"},
	{"internal/platform/feedback/board.go", "BoardMe", tsFeedbackFile, "FeedbackBoardMe"},
	{"internal/platform/feedback/types.go", "Profile", tsFeedbackFile, "FeedbackProfile"},
	{"internal/platform/feedback/types.go", "EffectiveLimits", tsFeedbackFile, "FeedbackEffectiveLimits"},
	{"internal/frontend/serve/feedback.go", "feedbackEnvView", tsFeedbackFile, "FeedbackEnv"},
	{"internal/frontend/serve/feedback.go", "feedbackSubmitBody", tsFeedbackFile, "FeedbackRequest"},
	{"internal/frontend/serve/feedback.go", "feedbackImageBody", tsFeedbackFile, "FeedbackImage"},
	// The staged set a commit message was drafted for. The fingerprint is what
	// the confirmation is checked against, so a page that cannot read it cannot
	// confirm anything.
	{"internal/session/control/commit.go", "CommitProposal", tsWorkspaceFile, "CommitProposal"},
	{"internal/session/control/commit.go", "CommitRequest", tsWorkspaceFile, "CommitRequest"},
	{"internal/session/control/commit.go", "CommitResult", tsWorkspaceFile, "CommitResult"},
	{"internal/session/control/commit.go", "StageRequest", tsWorkspaceFile, "StageRequest"},
	{"internal/platform/gitcommit/gitcommit.go", "File", tsWorkspaceFile, "CommitFile"},
	{"internal/session/control/discard.go", "DiscardRequest", tsWorkspaceFile, "DiscardRequest"},
	{"internal/session/control/git_push.go", "PushRequest", tsWorkspaceFile, "PushRequest"},
	{"internal/platform/gitpush/plan.go", "Plan", tsWorkspaceFile, "PushPlan"},
	{"internal/platform/gitpush/push.go", "Result", tsWorkspaceFile, "PushResult"},
	{"internal/frontend/serve/changes.go", "gitRepoRow", tsWorkspaceFile, "GitRepoRow"},
	{"internal/frontend/serve/changes.go", "gitReposView", tsWorkspaceFile, "GitRepos"},
	{"internal/platform/gitstatus/commitdetail.go", "CommitFile", tsWorkspaceFile, "GitCommitFile"},
	{"internal/frontend/serve/git_commit_detail.go", "commitFilesView", tsWorkspaceFile, "GitCommitFiles"},
	// The scale range the slider is drawn from: a bound the page cannot read
	// would leave it a range of its own.
	{"internal/frontend/serve/appearance.go", "zoomRangeView", tsLookFile, "ZoomRange"},
	// The chart spec a stored render_chart call carries. The page re-validates it
	// on load, so a field it cannot read is one the card silently drops.
	{"internal/contract/chartspec/spec.go", "Spec", tsChartFile, "ChartSpec"},
	{"internal/contract/chartspec/spec.go", "Data", tsChartFile, "ChartData"},
	{"internal/contract/chartspec/spec.go", "Column", tsChartFile, "ChartColumn"},
	{"internal/contract/chartspec/spec.go", "Mark", tsChartFile, "ChartMark"},
	{"internal/contract/chartspec/spec.go", "Axis", tsChartFile, "ChartAxis"},
	// The theme a person is looking at and the picture behind it: the pack
	// listing with its shape blocks and recipes, the import receipt, and the
	// reader's own size and wallpaper. A field one side cannot read is absent.
	{"internal/frontend/serve/themes.go", "themeView", tsLookFile, "ThemePack"},
	{"internal/ext/theme/theme.go", "Background", tsLookFile, "ThemeBackground"},
	{"internal/ext/theme/theme.go", "SurfaceOpacities", tsLookFile, "ThemeSurfaceOpacities"},
	{"internal/ext/theme/theme.go", "SurfaceOpacity", tsLookFile, "ThemeSurfaceOpacity"},
	{"internal/ext/theme/theme.go", "Sky", tsLookFile, "ThemeSky"},
	{"internal/ext/theme/theme.go", "Recipes", tsLookFile, "ThemeRecipes"},
	{"internal/ext/theme/install.go", "Installed", tsLookFile, "ThemeImport"},
	{"internal/frontend/serve/appearance.go", "appearanceView", tsLookFile, "Appearance"},
	{"internal/frontend/serve/appearance.go", "wallpaperView", tsLookFile, "Wallpaper"},
}

// partialWireMirrors are hand-written copies honestly smaller than the kernel's
// struct: a terminal draws one row per entry and the page's outbox shows the
// person's lines. Each pair names the fields it must carry and is compared both
// ways over those only; a subset's failure mode is a field quietly dropped, and
// the full-mirror list above is not relaxed for it.
var partialWireMirrors = []partialWireMirror{
	// What a surface reads to decide whose a queued line is, whether it may act
	// on it, and whether the queue can be changed at all.
	{"internal/state/sessioninbox/types.go", "InboxItemMeta", tuiInboxFile, "InboxItem",
		[]string{"id", "intent", "state", "preview", "origin", "images", "blockCode", "blockReason"}},
	{"internal/state/sessioninbox/types.go", "InboxSnapshot", tuiInboxFile, "InboxSnapshot",
		[]string{"revision", "paused", "readonly", "recovered", "recoveredCount", "unrecovered", "items", "capacity"}},
	{"internal/state/sessioninbox/types.go", "Capacity", tuiInboxFile, "InboxCapacity",
		[]string{"items", "maxItems", "bytes", "maxBytes", "maxItemBytes"}},
	// The page reads the same two types through its own hand-written copy. It
	// omits the manifest fields neither surface draws, and must keep the ones
	// its outbox decides from.
	{"internal/state/sessioninbox/types.go", "InboxItemMeta", tsPortFile, "QueueItem",
		[]string{"id", "intent", "origin", "state", "preview", "createdAt", "blockReason", "blockCode", "refs", "images"}},
	{"internal/state/sessioninbox/types.go", "InboxSnapshot", tsPortFile, "Queue",
		[]string{"revision", "paused", "readonly", "recoveredCount", "items", "capacity"}},
	{"internal/state/sessioninbox/types.go", "Capacity", tsPortFile, "QueueCapacity",
		[]string{"items", "maxItems", "bytes", "maxBytes"}},
	// The terminal's copy of /history's tool call. Its card draws the arguments
	// the proxy rewrote, so the record must carry the evidence too, or a
	// reopened session shows the rewrite as the model's own.
	{"internal/frontend/serve/history.go", "historyToolCall", "internal/frontend/tui/client.go", "HistoryToolCall",
		[]string{"id", "name", "arguments", "argumentNormalization", "resolvedName", "capabilityId"}},
	// The terminal's copy of /history's message. It draws no picture, but the
	// count tells an image-only turn from host chrome, and via/modelRef are
	// what a reload can no longer say once the reader drops them.
	{"internal/frontend/serve/history.go", "historyMessage", "internal/frontend/tui/client.go", "HistoryMessage",
		[]string{"images", "via", "modelRef"}},
}

// partialWireMirror is one declared subset: readerFile's readerType must carry
// every name in fields, the writer must still send each, and the reader must
// choose no name the writer never sends. What is left out is the point.
type partialWireMirror struct {
	writerFile, writerType string
	readerFile, readerType string
	fields                 []string
}

type wireMirror struct {
	goFile, goType, tsFile, tsType string
}

// readMirrors are Go types that read another Go type's JSON by hand. Unlike the
// desktop's pictures these are read-only and deliberately partial — a harness
// charts the few numbers it compares — so only one direction is a defect: a
// name the reader declares and the writer never sends reads as zero forever,
// which is what a rename on the writing side looks like from here.
var readMirrors = []readMirror{
	{"cmd/e2ebench/main.go", "runMetrics", "internal/frontend/cli/run_metrics.go", "RunMetrics"},
}

type readMirror struct {
	readerFile, readerType, writerFile, writerType string
}

// wireScan collects the declared types' wire field names while the tree is
// walked, so the comparison costs no second parse.
type wireScan struct{ fields map[string][]string }

func newWireScan() *wireScan { return &wireScan{fields: map[string][]string{}} }

func (w *wireScan) observe(src *sourceFile) {
	if src.file == nil {
		return
	}
	for _, m := range mirroredWireTypes {
		if m.goFile != src.rel {
			continue
		}
		if names, ok := wireFieldNames(src.file, m.goType); ok {
			w.fields[m.goFile+"."+m.goType] = names
		}
	}
	for _, m := range readMirrors {
		for _, side := range [][2]string{{m.readerFile, m.readerType}, {m.writerFile, m.writerType}} {
			if side[0] != src.rel {
				continue
			}
			if names, ok := wireFieldNames(src.file, side[1]); ok {
				w.fields[side[0]+"."+side[1]] = names
			}
		}
	}
	for _, m := range partialWireMirrors {
		for _, side := range [][2]string{{m.writerFile, m.writerType}, {m.readerFile, m.readerType}} {
			if side[0] != src.rel {
				continue
			}
			if names, ok := wireFieldNames(src.file, side[1]); ok {
				w.fields[side[0]+"."+side[1]] = names
			}
		}
	}
}

// findings compares each declared pair both ways. Either direction is a defect:
// a picture cannot show what it was never told, and it cannot expect what
// nothing sends.
func (w *wireScan) findings(root string) []Finding {
	bodies := map[string]string{}
	for _, m := range mirroredWireTypes {
		if _, read := bodies[m.tsFile]; read {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(root, m.tsFile))
		if err != nil {
			return nil
		}
		bodies[m.tsFile] = string(raw)
	}
	for _, m := range partialWireMirrors {
		if !strings.HasSuffix(m.readerFile, ".ts") {
			continue
		}
		if _, read := bodies[m.readerFile]; read {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(root, m.readerFile))
		if err != nil {
			return nil
		}
		bodies[m.readerFile] = string(raw)
	}
	out := append(wireParityFindings(mirroredWireTypes, w.fields, bodies), readParityFindings(readMirrors, w.fields)...)
	out = append(out, partialWireFindings(partialWireMirrors, w.fields, bodies)...)
	return append(out, wireConstantFindings(root, mirroredWireConstants)...)
}

// readParityFindings checks the one direction a hand-written reader owes: every
// name it declares must be one the writer actually sends. What the writer sends
// and this reader ignores is the point of a partial mirror, so it is not a
// finding.
func readParityFindings(mirrors []readMirror, goFields map[string][]string) []Finding {
	var out []Finding
	for _, m := range mirrors {
		reader, declared := goFields[m.readerFile+"."+m.readerType]
		writer, sends := goFields[m.writerFile+"."+m.writerType]
		if !declared || !sends {
			missing, at := m.writerType, m.writerFile
			if !declared {
				missing, at = m.readerType, m.readerFile
			}
			out = append(out, Finding{at, 1, ruleWireParity,
				fmt.Sprintf("%s is declared a mirrored read contract and is not a struct here", missing), 1})
			continue
		}
		for _, name := range missingFrom(reader, writer) {
			out = append(out, Finding{m.readerFile, 1, ruleWireParity,
				fmt.Sprintf("%s reads %q and %s.%s never sends it", m.readerType, name, m.writerFile, m.writerType), 1})
		}
	}
	return out
}

// partialWireFindings checks a declared subset in the three directions that
// matter: the writer must still send each named field, the reader must still
// carry it, and the reader may not read a name nothing sends. What the writer
// sends and a subset ignores is the reason a subset exists.
func partialWireFindings(mirrors []partialWireMirror, goFields map[string][]string, bodies map[string]string) []Finding {
	var out []Finding
	for _, m := range mirrors {
		writer, sends := goFields[m.writerFile+"."+m.writerType]
		if !sends {
			out = append(out, Finding{m.writerFile, 1, ruleWireParity,
				fmt.Sprintf("%s is declared a mirrored wire type and is not a struct here", m.writerType), 1})
			continue
		}
		reader, line, declares := partialReaderFields(m, goFields, bodies)
		if !declares {
			out = append(out, Finding{m.readerFile, 1, ruleWireParity,
				fmt.Sprintf("%s is declared a partial mirror of %s.%s and is not declared here", m.readerType, m.writerFile, m.writerType), 1})
			continue
		}
		for _, name := range m.fields {
			if !slices.Contains(writer, name) {
				out = append(out, Finding{m.writerFile, 1, ruleWireParity,
					fmt.Sprintf("%s no longer sends %q, a field %s decides from", m.writerType, name, m.readerType), 1})
			}
			if !slices.Contains(reader, name) {
				out = append(out, Finding{m.readerFile, line, ruleWireParity,
					fmt.Sprintf("%s no longer declares %q, which %s.%s sends and this copy decides from", m.readerType, name, m.writerFile, m.writerType), 1})
			}
		}
		for _, name := range missingFrom(reader, writer) {
			if slices.Contains(m.fields, name) {
				// Already named as the writer dropping a declared field.
				continue
			}
			out = append(out, Finding{m.readerFile, line, ruleWireParity,
				fmt.Sprintf("%s reads %q and %s.%s never sends it", m.readerType, name, m.writerFile, m.writerType), 1})
		}
	}
	return out
}

// partialReaderFields reads one side of a pair from whichever grammar declares
// it: the terminal's copy is Go, the page's is TypeScript.
func partialReaderFields(m partialWireMirror, goFields map[string][]string, bodies map[string]string) ([]string, int, bool) {
	if strings.HasSuffix(m.readerFile, ".ts") {
		return tsInterfaceFields(bodies[m.readerFile], m.readerType)
	}
	names, ok := goFields[m.readerFile+"."+m.readerType]
	return names, 1, ok
}

// wireParityFindings is the comparison itself, so a fixture can carry the one
// pair a case is about rather than the tree's real ones.
func wireParityFindings(mirrors []wireMirror, goFields map[string][]string, bodies map[string]string) []Finding {
	var out []Finding
	for _, m := range mirrors {
		fields, declared := goFields[m.goFile+"."+m.goType]
		if !declared {
			out = append(out, Finding{m.goFile, 1, ruleWireParity,
				fmt.Sprintf("%s is declared a mirrored wire type and is not a struct here", m.goType), 1})
			continue
		}
		tsFields, line, found := tsInterfaceFields(bodies[m.tsFile], m.tsType)
		if !found {
			out = append(out, Finding{m.tsFile, 1, ruleWireParity,
				fmt.Sprintf("%s mirrors %s.%s and is not declared here", m.tsType, m.goFile, m.goType), 1})
			continue
		}
		for _, name := range missingFrom(fields, tsFields) {
			out = append(out, Finding{m.tsFile, line, ruleWireParity,
				fmt.Sprintf("%s sends %q and %s cannot read it", m.goType, name, m.tsType), 1})
		}
		for _, name := range missingFrom(tsFields, fields) {
			out = append(out, Finding{m.tsFile, line, ruleWireParity,
				fmt.Sprintf("%s reads %q and %s never sends it", m.tsType, name, m.goType), 1})
		}
	}
	return out
}

func missingFrom(want, have []string) []string {
	var out []string
	for _, name := range want {
		if !slices.Contains(have, name) {
			out = append(out, name)
		}
	}
	return out
}

// wireFieldNames lists what this struct serialises as. A field the encoder skips
// is not part of the contract and is not reported. An embedded struct without a
// json name contributes its own fields, as encoding/json flattens it. One
// declared in another file or package is not read here: declare it as a pair of
// its own, as the intersection types in the desktop's wire.ts do.
func wireFieldNames(file *ast.File, typeName string) ([]string, bool) {
	return flattenedWireNames(file, typeName, map[string]bool{})
}

func flattenedWireNames(file *ast.File, typeName string, seen map[string]bool) ([]string, bool) {
	st, ok := structNamed(file, typeName)
	if !ok || seen[typeName] {
		return nil, false
	}
	seen[typeName] = true
	var out []string
	for _, field := range st.Fields.List {
		if len(field.Names) == 0 {
			embedded, ok := embeddedWireNames(file, field, seen)
			if !ok {
				return nil, false
			}
			out = append(out, embedded...)
			continue
		}
		for _, name := range field.Names {
			if !name.IsExported() {
				continue
			}
			if wire, keep := wireName(field, name.Name); keep {
				out = append(out, wire)
			}
		}
	}
	return out, true
}

func embeddedWireNames(file *ast.File, field *ast.Field, seen map[string]bool) ([]string, bool) {
	typ := field.Type
	if star, ok := typ.(*ast.StarExpr); ok {
		typ = star.X
	}
	var name string
	switch t := typ.(type) {
	case *ast.Ident:
		name = t.Name
	case *ast.SelectorExpr:
		name = t.Sel.Name
	default:
		return nil, true
	}
	wire, keep := wireName(field, name)
	if !keep {
		return nil, true
	}
	if jsonTagName(field) != "" {
		return []string{wire}, true
	}
	if _, foreign := typ.(*ast.SelectorExpr); foreign {
		return nil, true
	}
	if _, here := structNamed(file, name); !here {
		return nil, true
	}
	return flattenedWireNames(file, name, seen)
}

func jsonTagName(field *ast.Field) string {
	if field.Tag == nil {
		return ""
	}
	tag := reflect.StructTag(strings.Trim(field.Tag.Value, "`")).Get("json")
	name, _, _ := strings.Cut(tag, ",")
	return name
}

func structNamed(file *ast.File, typeName string) (*ast.StructType, bool) {
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gen.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name.Name != typeName {
				continue
			}
			if st, ok := ts.Type.(*ast.StructType); ok && st.Fields != nil {
				return st, true
			}
		}
	}
	return nil, false
}

func wireName(field *ast.Field, goName string) (string, bool) {
	if field.Tag == nil {
		return goName, true
	}
	tag := reflect.StructTag(strings.Trim(field.Tag.Value, "`")).Get("json")
	if tag == "-" {
		return "", false
	}
	if name, _, _ := strings.Cut(tag, ","); name != "" {
		return name, true
	}
	return goName, true
}

var (
	tsInterfaceRe = regexp.MustCompile(`(?m)^export interface (\w+)(?: extends ([\w, ]+))? \{`)
	tsPropertyRe  = regexp.MustCompile(`^\s*(\w+)\??:`)
)

// tsInterfaceFields reads one interface's property names and the line it opens
// on. It reads the declaration's shape, never its wording, which is all a
// contract is. An interface it extends contributes its properties, as the
// compiler's own view of the type does; one declared elsewhere cannot be read
// and fails the lookup.
func tsInterfaceFields(body, typeName string) ([]string, int, bool) {
	return flattenedTSFields(body, typeName, map[string]bool{})
}

func flattenedTSFields(body, typeName string, seen map[string]bool) ([]string, int, bool) {
	if seen[typeName] {
		return nil, 0, false
	}
	seen[typeName] = true
	for _, m := range tsInterfaceRe.FindAllStringSubmatchIndex(body, -1) {
		if body[m[2]:m[3]] != typeName {
			continue
		}
		fields, _, ok := strings.Cut(body[m[1]:], "\n}")
		if !ok {
			return nil, 0, false
		}
		var out []string
		if m[4] >= 0 {
			for parent := range strings.SplitSeq(body[m[4]:m[5]], ",") {
				inherited, _, found := flattenedTSFields(body, strings.TrimSpace(parent), seen)
				if !found {
					return nil, 0, false
				}
				out = append(out, inherited...)
			}
		}
		for line := range strings.SplitSeq(fields, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			if p := tsPropertyRe.FindStringSubmatch(line); p != nil {
				out = append(out, p[1])
			}
		}
		return out, strings.Count(body[:m[0]], "\n") + 1, true
	}
	return nil, 0, false
}
