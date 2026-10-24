// notice_codes.go — the wire-stable vocabulary a frontend localizes notices by.
package event

// Notice codes are stable machine-readable identifiers for known notices.
// Frontends localize a notice's main copy by Code and fall back to matching
// the English Text (or showing it raw) when Code is empty or unknown, so
// wording edits in Go no longer silently break localization. Values are
// wire-stable: never rename or reuse one once shipped.
const (
	NoticeCodeSessionTreeChanged                                = "session_tree_changed"
	NoticeCodeCheckpointRecordingFailed                         = "checkpoint_recording_failed"
	NoticeCodeTurnIdentityMismatch                              = "turn_identity_mismatch"
	NoticeCodePlanAutoApproved                                  = "plan_auto_approved"
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
	NoticeCodeSettingChanged, NoticeCodeSettingOpen             = "setting_changed", "setting_open"
	NoticeCodeAskTimeoutInvalid                                 = "ask_timeout_invalid"
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
	// MCP tools whose definitions are new or changed since approval, withheld from the model; Detail lists them.
	NoticeCodeMCPToolsHeld = "mcp_tools_held"
	// Definitions of a server the user follows that changed and were taken without asking; Detail lists them.
	NoticeCodeMCPToolsFollowed = "mcp_tools_followed"
	// An MCP server refused a call for want of access the user did not grant; Detail is the server name.
	NoticeCodeMCPScopeUpgrade = "mcp_scope_upgrade"
	// A conversation opened from a 1.x log went on in a new session of its own.
	NoticeCodeSessionContinuedFrom1x = "session_continued_from_1x"
	// The user config names a default approval mode this build does not know; it loads as ask.
	NoticeCodeApprovalModeUnrecognized = "approval_mode_unrecognized"
	// Saved permission rules name no tool, so they match nothing; Detail is a PermissionRulesDormant.
	NoticeCodePermissionRulesDormant = "permission_rules_dormant"
	// A submission was refused while the session, workspace or runtime was changing.
	// Its text says to resend, which is right for a frontend that keeps no queue; one that queues on the typed refusal says so itself.
	NoticeCodeInputRefused = "input_refused"
	// A turn handed its open list back to the user; Detail is the model's `need`, as it wrote it.
	NoticeCodeAwaitUser = "await_user"
	// A Goal paused on its completion check was retried by the host (user setting goal_auto_retry).
	NoticeCodeGoalRetry = "goal_retry"
	// The retry allowance ran out; the Goal waits for the user.
	NoticeCodeGoalRetryGaveUp = "goal_retry_gave_up"
	// Bash reached a host outside allowed_domains because the user's file says not to ask; Detail is the host.
	NoticeCodeEgressAutoAllowed = "egress_auto_allowed"
	// The user allowed no wait for an answer and the host continued the list on its own; Detail is the model's `need`.
	NoticeCodeAwaitTimedOut = "await_timed_out"
	// The host stopped continuing an unanswered list after several tries in a row; it waits for the user. Detail is the `need`.
	NoticeCodeAwaitGaveUp = "await_gave_up"
	// A slash command nothing resolves, refused rather than sent as prose.
	NoticeCodeUnknownCommand = "unknown_command"
	// The display currency preference changed; Detail is the stored value, "" for auto.
	NoticeCodeDisplayCurrency = "display_currency"
	// A saved language choice is overridden by the project config; Detail is the language in effect.
	NoticeCodeLanguageOverridden = "language_overridden"
	// default_model names nothing configured, so the window opened on a fallback; the file is unchanged.
	NoticeCodeDefaultModelUnavailable        = "default_model_unavailable"
	NoticeCodeInheritedSubagentEffortDropped = "inherited_subagent_effort_dropped"
	// A branch's saved model disappeared from the current provider catalog; the pane uses a fallback.
	NoticeCodeBranchModelUnavailable = "branch_model_unavailable"
	// A legacy memory file could not be preserved, so the metadata migration left it as it was.
	NoticeCodeMemoryMigrationBackup = "memory_migration_backup"
	// /compact folded the context.
	NoticeCodeCompacted = "compacted"
	// /compact was declined as a verdict; Detail is the decline code, its text the English fallback.
	NoticeCodeCompactDeclined = "compact_declined"
	// /compact failed; Detail is the failure code, its text the English fallback.
	NoticeCodeCompactFailed = "compact_failed"
	// Automatic compaction is due but an earlier attempt's failure still holds the retry; Detail is that failure's code.
	NoticeCodeCompactHeld = "compact_held"
	// The context is over its compaction threshold and folding cannot help; Detail is a ContextComposition.
	NoticeCodeContextComposition = "context_composition"
	// A sub-agent is queued behind another's write claim; Detail is a SubagentWaiting.
	NoticeCodeSubagentWaiting = "subagent_waiting"
	// An optional extension was left out of an operation; Detail is the ExtensionSkipped payload.
	NoticeCodeExtensionSkipped = "extension_skipped"
	// A reopened session's inbox held unfinished instructions and came up paused; Detail is the InboxRecovered payload.
	NoticeCodeInboxRecovered = "inbox_recovered"
	// A session's inbox bodies could not be read, so the queue's contents are unknown and it came up paused; Detail is the InboxUnrecovered payload.
	NoticeCodeInboxUnrecovered = "inbox_unrecovered"
	// A context fold finished; Detail is the CompactionNotice payload, its text the English fallback.
	NoticeCodeCompactionTelemetry = "compaction_telemetry"
	// A background job started; Detail is the JobNotice payload, its text the English fallback.
	NoticeCodeJobStarted = "job_started"
	// A background job ended; Detail is the JobNotice payload, its text the English fallback.
	NoticeCodeJobFinished = "job_finished"
	// A background job was killed; Detail is the JobNotice payload, its text the English fallback.
	NoticeCodeJobKilled = "job_killed"
	// A background job failed; Detail is the JobNotice payload, its text the English fallback.
	NoticeCodeJobFailed = "job_failed"
	// A background job has been silent on its own output past the stalled threshold; Detail is the JobNotice payload with QuietSeconds, its text the English fallback.
	NoticeCodeJobStalled = "job_stalled"
	// The selected web search model cannot serve, so search is off; Detail is the provider/model, the code names why.
	NoticeCodeWebSearchModelBadRef        = "web_search_model_bad_ref"
	NoticeCodeWebSearchModelNotAdded      = "web_search_model_not_added"
	NoticeCodeWebSearchModelRemoved       = "web_search_model_removed"
	NoticeCodeWebSearchModelUnsupported   = "web_search_model_unsupported"
	NoticeCodeWebSearchModelNoCredentials = "web_search_model_no_credentials"
	NoticeCodeWebSearchBackendFallback    = "web_search_backend_fallback"
	NoticeCodeWebSearchBackendUnusable    = "web_search_backend_unusable"
	NoticeCodeWebFetchBackendFallback     = "web_fetch_backend_fallback"
	NoticeCodeWebFetchBackendUnusable     = "web_fetch_backend_unusable"
)

// A saved fold was not reused when its session was bound: the whole history is
// sent instead.
const (
	NoticeCodeFoldNotReusedHistory = "fold_not_reused_history"
	NoticeCodeFoldNotReusedLineage = "fold_not_reused_lineage"
)
