package tool

// Host tool identities are shared with runtime Name methods. Recognition does not
// imply registration or permission in a particular session.
const (
	HostAsk                        = "ask"
	HostCompleteSubtask            = "complete_subtask"
	HostDocs                       = "docs"
	HostExplore                    = "explore"
	HostFleet                      = "fleet"
	HostForget                     = "forget"
	HostHistory                    = "history"
	HostInstallSkill               = "install_skill"
	HostInstallSource              = "install_source"
	HostListSessions               = "list_sessions"
	HostLspDefinition              = "lsp_definition"
	HostLspDiagnostics             = "lsp_diagnostics"
	HostLspHover                   = "lsp_hover"
	HostLspReferences              = "lsp_references"
	HostMemory                     = "memory"
	HostParallelTasks              = "parallel_tasks"
	HostReadOnlySkill              = "read_only_skill"
	HostReadOnlyTask               = "read_only_task"
	HostReadSession                = "read_session"
	HostReadSkill                  = "read_skill"
	HostReadSubagentResult         = "read_subagent_result"
	HostRemember                   = "remember"
	HostResearch                   = "research"
	HostReview                     = "review"
	HostReviewReport               = "review_report"
	HostRunSkill                   = "run_skill"
	HostSecurityReview             = "security_review"
	HostSessionReadStrategyReceipt = "session_read_strategy_receipt"
	HostSessionToolResult          = "session_tool_result"
	HostSetSessionTitle            = "set_session_title"
	HostSlashCommand               = "slash_command"
	HostSubmitPlan                 = "submit_plan"
	HostTask                       = "task"
	HostUseCapability              = "use_capability"
	HostWebSearch                  = "web_search"
)

// KnownToolNames combines compile-time built-ins with host-managed identities.
// Callers must import tool/builtin to initialize the compile-time inventory.
func KnownToolNames() []string {
	names := []string{
		HostAsk,
		HostCompleteSubtask,
		HostDocs,
		HostExplore,
		HostFleet,
		HostForget,
		HostHistory,
		HostInstallSkill,
		HostInstallSource,
		HostListSessions,
		HostLspDefinition,
		HostLspDiagnostics,
		HostLspHover,
		HostLspReferences,
		HostMemory,
		HostParallelTasks,
		HostReadOnlySkill,
		HostReadOnlyTask,
		HostReadSession,
		HostReadSkill,
		HostReadSubagentResult,
		HostRemember,
		HostResearch,
		HostReview,
		HostReviewReport,
		HostRunSkill,
		HostSecurityReview,
		HostSessionReadStrategyReceipt,
		HostSessionToolResult,
		HostSetSessionTitle,
		HostSlashCommand,
		HostSubmitPlan,
		HostTask,
		HostUseCapability,
		HostWebSearch,
	}
	for _, t := range Builtins() {
		names = append(names, t.Name())
	}
	return names
}
