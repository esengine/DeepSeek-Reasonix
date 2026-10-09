// notice_codes.go — the wire-stable vocabulary a frontend localizes notices by.
package event

// Notice codes are stable machine-readable identifiers for known notices.
// Frontends localize a notice's main copy by Code and fall back to matching
// the English Text (or showing it raw) when Code is empty or unknown, so
// wording edits in Go no longer silently break localization. Values are
// wire-stable: never rename or reuse one once shipped.
const (
	NoticeCodeTurnIdentityMismatch                              = "turn_identity_mismatch"
	NoticeCodeFinalReadiness                                    = "final_readiness"
	NoticeCodeEmptyFinal                                        = "empty_final"
	NoticeCodeExecutorHandoff                                   = "executor_handoff"
	NoticeCodeToolBudget                                        = "tool_budget"
	NoticeCodePromptQueued                                      = "prompt_queued"
	NoticeCodeLoopGuard                                         = "loop_guard"
	NoticeCodePerseverationLoop                                 = "perseveration_loop"
	NoticeCodePerseverationRetry                                = "perseveration_retry"
	NoticeCodeProgressGuard                                     = "progress_guard"
	NoticeCodeEvidenceNudge                                     = "evidence_nudge"
	NoticeCodeReasoningGovernor                                 = "reasoning_governor"
	NoticeCodeWorkspaceLease                                    = "workspace_lease"
	NoticeCodeHookBlocked                                       = "hook_blocked"
	NoticeCodeHookWarned                                        = "hook_warned"
	NoticeCodeHookFailed                                        = "hook_failed"
	NoticeCodeHookUnevaluable                                   = "hook_unevaluable"
	NoticeCodeCancelledTurn                                     = "cancelled_turn_display"
	NoticeCodeUnappliedSteer                                    = "unapplied_steer"
	NoticeCodeSessionRecoveryForked                             = "session_recovery_forked"
	NoticeCodeSessionRecoveryAdopted                            = "session_recovery_adopted"
	NoticeCodeSessionRecoveryAdoptedCovered                     = "session_recovery_adopted_covered"
	NoticeCodeSessionRecoveryDepthCap                           = "session_recovery_depth_cap"
	NoticeCodeSessionShutdownRecoveryForked                     = "session_shutdown_recovery_forked"
	NoticeCodeVerificationStalled                               = "verification_stalled"
	NoticeCodeDecisionReceipt, NoticeCodeContextEditingFallback = "decision_receipt", "context_editing_fallback"
	// A reported lease wait always arrives as a pair: one of the two below
	// closes the one above, so no surface is left holding an open wait.
	NoticeCodeWorkspaceLeaseResumed, NoticeCodeWorkspaceLeaseAbandoned = "workspace_lease_resumed", "workspace_lease_abandoned"
	// A remembered approval: Detail carries what it allows, the rule's subject.
	NoticeCodePermissionSaved, NoticeCodePermissionCovered, NoticeCodePermissionSaveFailed = "permission_saved", "permission_covered", "permission_save_failed"
	// An external tool result a screening model judged to address the agent.
	NoticeCodeSuspectedInjection = "suspected_injection"
	// /context's report; Detail is the breakdown under its one-line summary.
	NoticeCodeContextReport = "context_report"
	// Programs a project file names (hooks, language servers) held back until approved.
	NoticeCodeProjectProgramsAwaitingApproval = "project_programs_awaiting_approval"
	// An approved workspace program whose files changed; the host did not run it.
	NoticeCodeProjectProgramChanged = "project_program_changed"
	// A project-declared MCP server held off until the user approves what it runs:
	// never approved, or changed since it was. Detail is the launch line.
	NoticeCodeProjectMCPAwaitingApproval, NoticeCodeProjectMCPChanged = "project_mcp_awaiting_approval", "project_mcp_changed"
	// A conversation opened from a 1.x log went on in a new session of its own.
	NoticeCodeSessionContinuedFrom1x = "session_continued_from_1x"
	// The user config names a default approval mode this build does not know; it loads as ask.
	NoticeCodeApprovalModeUnrecognized = "approval_mode_unrecognized"
	// A turn handed its open list back to the user; Detail is the model's `need`, as it wrote it.
	NoticeCodeAwaitUser = "await_user"
	// A slash command nothing resolves, refused rather than sent as prose.
	NoticeCodeUnknownCommand = "unknown_command"
	// The display currency preference changed; Detail is the stored value, "" for auto.
	NoticeCodeDisplayCurrency = "display_currency"
	// A saved language choice is overridden by the project config; Detail is the language in effect.
	NoticeCodeLanguageOverridden = "language_overridden"
	// default_model names nothing configured, so the window opened on a fallback; the file is unchanged.
	NoticeCodeDefaultModelUnavailable = "default_model_unavailable"
	// A legacy memory file could not be preserved, so the metadata migration left it as it was.
	NoticeCodeMemoryMigrationBackup = "memory_migration_backup"
	// /compact folded the context.
	NoticeCodeCompacted = "compacted"
	// /compact was declined as a verdict; Detail is the decline code, its text the English fallback.
	NoticeCodeCompactDeclined = "compact_declined"
	// /compact failed; Detail is the failure code, its text the English fallback.
	NoticeCodeCompactFailed = "compact_failed"
	// An optional extension was left out of an operation; Detail is the ExtensionSkipped payload.
	NoticeCodeExtensionSkipped = "extension_skipped"
	// A reopened session's inbox held unfinished instructions and came up paused; Detail is the InboxRecovered payload.
	NoticeCodeInboxRecovered = "inbox_recovered"
)
