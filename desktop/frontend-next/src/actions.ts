import { MARKET_ACTIONS } from "./actions_market";
import { SCHEDULE_ACTIONS } from "./actions_schedule";
import { DOCUMENT_ACTIONS } from "./actions_document";
import { ISOLATION_ACTIONS } from "./actions_isolation";

// The census oracle is independent of production literals so disagreements fail.
// An id names the intent; data-target and data-value name its entity and choice.

export type ActionKind =
  /** Changes only what is on screen. Must not reach the kernel. */
  | "view"
  /** Moves the reader somewhere else without changing session state. */
  | "navigation"
  /** Asks the kernel to change canonical state. */
  | "kernel-mutation"
  /** Answers something the host is blocked on: an approval, a question. */
  | "interaction"
  /** Removes something a person would have to recreate by hand. */
  | "destructive"
  /** Reaches the window, not the kernel. */
  | "shell-native"
  /** Safe to repeat; the boundary is part of its contract. */
  | "repeatable";

/** The lowest level of evidence this action's contract may rest on. Not a test
 *  file name — that would be a second hand-kept index, and it would rot. */
export type ActionProof =
  /** A render assertion is enough: nothing happens between click and answer. */
  | "static"
  /** jsdom: pending, refusal, repeat, focus. Most controls belong here. */
  | "interaction"
  /** Chromium: layout, hit target, scroll ownership, real shell adapters. */
  | "browser"
  /** The real kernel: the canonical state this claims to change did change. */
  | "authority-effect";

export interface UIAction {
  id: string;
  kind: ActionKind;
  /** Whether the click is about a particular entity, named by data-target. */
  target: "none" | "entity" | "optional";
  proof: ActionProof;
}

export const ACTIONS: UIAction[] = [
  {id:"agent-composition.select",kind:"kernel-mutation",target:"none",proof:"authority-effect"},
  {id:"agent-composition.manage",kind:"view",target:"none",proof:"interaction"},
  {id:"agent-preset.show-picker",kind:"view",target:"none",proof:"interaction"},
  {id:"agent-composition.unavailable",kind:"view",target:"none",proof:"interaction"},
  {id:"agent-composition.new",kind:"kernel-mutation",target:"none",proof:"authority-effect"},
  {id:"agent-preset.reload",kind:"view",target:"none",proof:"interaction"},
  {id:"agent-preset.view",kind:"view",target:"none",proof:"interaction"},
  {id:"agent-preset.new-task-default",kind:"view",target:"entity",proof:"interaction"},
  {id:"agent-preset.usage",kind:"view",target:"entity",proof:"interaction"},
  {id:"agent-preset.create",kind:"view",target:"none",proof:"interaction"},
  {id:"agent-preset.leave",kind:"view",target:"none",proof:"interaction"},
  {id:"agent-preset.leave-cancel",kind:"view",target:"none",proof:"interaction"},
  {id:"agent-preset.leave-discard",kind:"view",target:"none",proof:"interaction"},
  {id:"agent-preset.persona",kind:"view",target:"none",proof:"interaction"},
  {id:"agent-preset.copy",kind:"view",target:"none",proof:"interaction"},
  {id:"agent-preset.scope",kind:"view",target:"none",proof:"interaction"},
  {id:"agent-preset.id",kind:"view",target:"none",proof:"interaction"},
  {id:"agent-preset.name",kind:"view",target:"none",proof:"interaction"},
  {id:"agent-preset.description",kind:"view",target:"none",proof:"interaction"},
  {id:"agent-preset.module-mode",kind:"view",target:"entity",proof:"interaction"},
  {id:"agent-preset.module-id",kind:"view",target:"entity",proof:"interaction"},
  {id:"agent-preset.module-parameters",kind:"view",target:"entity",proof:"interaction"},
  {id:"agent-preset.module-add",kind:"view",target:"entity",proof:"interaction"},
  {id:"agent-preset.module-choose",kind:"view",target:"entity",proof:"interaction"},
  {id:"agent-preset.module-toggle",kind:"view",target:"entity",proof:"interaction"},
  {id:"agent-preset.module-select-all",kind:"view",target:"entity",proof:"interaction"},
  {id:"agent-preset.module-select-group",kind:"view",target:"entity",proof:"interaction"},
  {id:"agent-preset.module-clear",kind:"view",target:"entity",proof:"interaction"},
  {id:"agent-preset.module-search",kind:"view",target:"entity",proof:"interaction"},
  {id:"agent-preset.module-navigate",kind:"view",target:"none",proof:"interaction"},
  {id:"agent-preset.module-done",kind:"view",target:"none",proof:"interaction"},
  {id:"agent-preset.module-remove",kind:"view",target:"entity",proof:"interaction"},
  {id:"agent-preset.save",kind:"kernel-mutation",target:"none",proof:"authority-effect"},
  {id:"agent-preset.default",kind:"kernel-mutation",target:"none",proof:"authority-effect"},
  {id:"agent-preset.workspace-default",kind:"kernel-mutation",target:"none",proof:"authority-effect"},
  {id:"agent-preset.delete-preview",kind:"view",target:"none",proof:"interaction"},
  {id:"agent-preset.delete",kind:"kernel-mutation",target:"none",proof:"authority-effect"},
  {id:"agent-preset.delete-cancel",kind:"view",target:"none",proof:"interaction"},
  {id:"agent-preset.export",kind:"view",target:"none",proof:"interaction"},
  {id:"agent-preset.import",kind:"view",target:"none",proof:"interaction"},
  // ── The window's own switches, and the way into the turn's policy ────────
  // Completion verification is adaptive now, rather than a user-facing preset.
  // The legacy endpoint remains in the port for older clients and sessions,
  // but it is intentionally not advertised as an interface action.
  { id: "chart.scroll", kind: "view", target: "none", proof: "interaction" },
  { id: "chart.data", kind: "view", target: "none", proof: "interaction" },
  { id: "chrome.policy", kind: "view", target: "none", proof: "interaction" },
  { id: "phone-device.enabled", kind: "shell-native", target: "none", proof: "interaction" },
  { id: "phone-device.occasion", kind: "shell-native", target: "none", proof: "interaction" },
  { id: "phone-device.retry", kind: "shell-native", target: "none", proof: "interaction" },
  { id: "phone-device.restart", kind: "shell-native", target: "none", proof: "interaction" },
  { id: "chrome.rail", kind: "view", target: "none", proof: "browser" },
  { id: "chrome.more", kind: "view", target: "none", proof: "interaction" },
  { id: "chrome.open-project", kind: "shell-native", target: "none", proof: "interaction" },
  { id: "appearance.theme", kind: "view", target: "none", proof: "interaction" },
  { id: "appearance.nav-rail", kind: "view", target: "none", proof: "interaction" },
  { id: "appearance.nav-entry-visible", kind: "view", target: "none", proof: "interaction" },
  { id: "appearance.nav-entry-move", kind: "view", target: "none", proof: "interaction" },
  { id: "appearance.nav-entries-reset", kind: "view", target: "none", proof: "interaction" },
  { id: "appearance.live-grace", kind: "view", target: "none", proof: "interaction" },
  { id: "appearance.legacy-sessions", kind: "view", target: "none", proof: "interaction" },
  { id: "appearance.open-session-limit", kind: "view", target: "none", proof: "interaction" },
  { id: "appearance.live-stream-budget", kind: "view", target: "none", proof: "interaction" },
  { id: "appearance.session-quick-actions", kind: "view", target: "none", proof: "interaction" },
  { id: "appearance.new-answer-marker", kind: "view", target: "none", proof: "interaction" },
  { id: "appearance.graphics", kind: "view", target: "none", proof: "interaction" },
  { id: "appearance.session-tabs", kind: "view", target: "none", proof: "interaction" },
  { id: "appearance.esc-stop", kind: "view", target: "none", proof: "interaction" },
  { id: "workspace.configure", kind: "view", target: "none", proof: "interaction" },
  { id: "workspace.prepare-cancel", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "workspace.prepare-details", kind: "view", target: "none", proof: "interaction" },
  { id: "workspace.prepare-edit", kind: "view", target: "none", proof: "interaction" },
  { id: "workspace.source-search", kind: "view", target: "none", proof: "interaction" },
  { id: "workspace.source-navigate", kind: "view", target: "none", proof: "interaction" },
  { id: "workspace.source-select", kind: "view", target: "none", proof: "interaction" },
  { id: "workspace.source-open", kind: "view", target: "none", proof: "interaction" },
  { id: "workspace.worktree-toggle", kind: "view", target: "none", proof: "interaction" },
  { id: "workspace.search", kind: "view", target: "none", proof: "interaction" },
  { id: "session.filter", kind: "view", target: "none", proof: "interaction" },
  { id: "session.more", kind: "repeatable", target: "none", proof: "interaction" },
  { id: "session.selection", kind: "view", target: "optional", proof: "interaction" },
  { id: "session.batch-archive", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "session.batch-delete", kind: "destructive", target: "none", proof: "authority-effect" },
  { id: "chrome.settings", kind: "navigation", target: "none", proof: "interaction" },
  { id: "browser.open", kind: "view", target: "none", proof: "interaction" },
  { id: "browser.wide", kind: "view", target: "none", proof: "interaction" },
  { id: "browser.full", kind: "view", target: "none", proof: "interaction" },
  { id: "browser.back", kind: "navigation", target: "none", proof: "interaction" },
  { id: "browser.reload", kind: "repeatable", target: "none", proof: "interaction" },
  { id: "browser.address", kind: "view", target: "none", proof: "static" },
  { id: "browser.external", kind: "shell-native", target: "none", proof: "interaction" },
  { id: "tool.copy-output", kind: "shell-native", target: "none", proof: "interaction" },
  // The way out of a window whose assets an update replaced underneath it.
  { id: "chrome.reload", kind: "navigation", target: "none", proof: "interaction" },
  { id: "chrome.changed-files", kind: "kernel-mutation", target: "none", proof: "interaction" },
  { id: "chrome.feedback-level-notices", kind: "kernel-mutation", target: "none", proof: "interaction" },
  { id: "level-up.notice-dismiss", kind: "kernel-mutation", target: "none", proof: "interaction" },
  { id: "chrome.fold-work", kind: "kernel-mutation", target: "none", proof: "interaction" },
  { id: "chrome.turn-narration", kind: "kernel-mutation", target: "none", proof: "interaction" },
  { id: "changes.open", kind: "navigation", target: "none", proof: "interaction" },
  { id: "chrome.receipt", kind: "view", target: "none", proof: "interaction" },
  { id: "chrome.receipt-line", kind: "view", target: "none", proof: "interaction" },
  { id: "chrome.keep-awake", kind: "view", target: "none", proof: "interaction" },
  { id: "composer.send-shortcut", kind: "view", target: "none", proof: "interaction" },
  { id: "transcript.fold", kind: "view", target: "none", proof: "interaction" },
  { id: "transcript.fold-preset", kind: "view", target: "none", proof: "interaction" },
  { id: "transcript.fold-preview", kind: "view", target: "none", proof: "interaction" },
  { id: "chrome.account", kind: "navigation", target: "none", proof: "interaction" },
  { id: "metrics.details", kind: "view", target: "optional", proof: "interaction" },
  { id: "metrics.hide-amounts", kind: "view", target: "none", proof: "interaction" },
  { id: "session.menu", kind: "view", target: "entity", proof: "interaction" },
  { id: "workspace.menu", kind: "view", target: "entity", proof: "interaction" },
  { id: "machine.menu", kind: "view", target: "none", proof: "interaction" },
  { id: "session.pin", kind: "view", target: "entity", proof: "interaction" },
  { id: "session.mark-read", kind: "kernel-mutation", target: "entity", proof: "interaction" },
  { id: "session.pause", kind: "kernel-mutation", target: "entity", proof: "interaction" },
  // Closing a conversation's pane ends its runtime and keeps the transcript;
  // refused while a turn runs, so it never stops work.
  { id: "session.close", kind: "kernel-mutation", target: "entity", proof: "interaction" },
  { id: "session.branch", kind: "kernel-mutation", target: "entity", proof: "interaction" },
  { id: "session.archive", kind: "kernel-mutation", target: "entity", proof: "authority-effect" },
  { id: "session.copy-info", kind: "shell-native", target: "entity", proof: "interaction" },
  { id: "session.export", kind: "shell-native", target: "entity", proof: "browser" },
  { id: "session.delete", kind: "destructive", target: "entity", proof: "authority-effect" },

  // What a call showed the model, at the size a card allows and then at the
  // size a person can read. Opening and closing are one intent seen twice, but
  // they are two buttons in two places: the thumbnail and the enlargement.
  // Taking back what a prompt allowed for this session. One id: the row and the
  // header ask the same thing of the kernel, and which grant rides on
  // data-target, with "all" as the value that names none of them.
  { id: "permissions.revoke-session", kind: "kernel-mutation", target: "entity", proof: "authority-effect" },
  { id: "permissions.revoke-remembered", kind: "kernel-mutation", target: "entity", proof: "authority-effect" },

  { id: "image.open", kind: "view", target: "none", proof: "interaction" },
  { id: "image.close", kind: "view", target: "none", proof: "interaction" },
  { id: "image.focus-trap", kind: "navigation", target: "none", proof: "interaction" },
  { id: "tool.show-all", kind: "view", target: "none", proof: "interaction" },
  { id: "tool.show-full", kind: "view", target: "none", proof: "interaction" },
  { id: "agent.open", kind: "view", target: "entity", proof: "interaction" },
  { id: "agent.focus-trap", kind: "navigation", target: "none", proof: "interaction" },
  { id: "agent.close", kind: "view", target: "none", proof: "interaction" },

  // ── The turn ─────────────────────────────────────────────────────────────
  // Send and stop remain two actions even when they sit together: during a
  // live turn the first steers and the second cancels the current turn.
  { id: "session.send", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "session.stop", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "tool.bring", kind: "view", target: "entity", proof: "interaction" },
  { id: "tool.unbring", kind: "view", target: "entity", proof: "interaction" },
  { id: "tool.stop", kind: "kernel-mutation", target: "entity", proof: "interaction" },
  { id: "session.open-match", kind: "navigation", target: "entity", proof: "interaction" },
  { id: "job.cancel", kind: "kernel-mutation", target: "entity", proof: "authority-effect" },
  { id: "job.tip", kind: "view", target: "entity", proof: "interaction" },
  { id: "job.view", kind: "view", target: "entity", proof: "interaction" },
  { id: "job.close", kind: "view", target: "none", proof: "interaction" },
  { id: "plan.mode", kind: "kernel-mutation", target: "none", proof: "interaction" },
  // A rewrite is asked of the kernel but changes nothing there; taking it only
  // replaces what is in the box.
  { id: "prompt.refine", kind: "repeatable", target: "none", proof: "interaction" },
  { id: "prompt.adopt", kind: "view", target: "none", proof: "interaction" },
  { id: "prompt.discard", kind: "view", target: "none", proof: "interaction" },

  // ── Committing what is staged ────────────────────────────────────────────
  // Drafting reads the index and asks the model; only confirm records a commit,
  // and only locally.
  { id: "commit.draft", kind: "repeatable", target: "none", proof: "interaction" },
  { id: "workbench.stage", kind: "kernel-mutation", target: "entity", proof: "authority-effect" },
  { id: "workbench.stage-all", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "commit.edit", kind: "view", target: "none", proof: "interaction" },
  { id: "commit.acknowledge", kind: "view", target: "none", proof: "interaction" },
  { id: "commit.confirm", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "commit.close", kind: "view", target: "none", proof: "interaction" },

  // ── Which branch the workspace is on ─────────────────────────────────────
  // The composer's branch chip opens the locals; picking one checks it out in
  // the workspace and re-reads what the checkout moved.
  { id: "git.branch", kind: "kernel-mutation", target: "none", proof: "authority-effect" },

  // ── What is waiting to be sent ───────────────────────────────────────────
  { id: "queue.edit", kind: "kernel-mutation", target: "entity", proof: "interaction" },
  // Giving a session a name of your own. The sidebar's tree and the pane tabs
  // rename the same session through the same host call, so it is one id.
  { id: "session.auto-name", kind: "kernel-mutation", target: "entity", proof: "authority-effect" },
  { id: "session.rename-start", kind: "navigation", target: "entity", proof: "interaction" },
  { id: "session.rename", kind: "kernel-mutation", target: "entity", proof: "authority-effect" },
  { id: "session.rename.title", kind: "view", target: "none", proof: "interaction" },
  { id: "queue.cancel", kind: "kernel-mutation", target: "entity", proof: "interaction" },
  { id: "queue.send-now", kind: "kernel-mutation", target: "entity", proof: "interaction" },
  { id: "queue.retry", kind: "repeatable", target: "entity", proof: "interaction" },
  { id: "queue.refresh", kind: "repeatable", target: "entity", proof: "interaction" },
  { id: "queue.move", kind: "kernel-mutation", target: "entity", proof: "interaction" },
  { id: "queue.pause", kind: "kernel-mutation", target: "none", proof: "interaction" },

  // ── Answers the host is blocked on. One id per port call, with the verdict
  //    on data-value: the kernel takes one decision with an answer, not three
  //    calls named after button text.
  { id: "decision.tool", kind: "interaction", target: "entity", proof: "authority-effect" },
  { id: "decision.full-access", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "decision.plan", kind: "interaction", target: "entity", proof: "authority-effect" },

  // ── Capabilities ─────────────────────────────────────────────────────────
  { id: "skill.enabled", kind: "kernel-mutation", target: "entity", proof: "authority-effect" },
  { id: "mcp.enabled", kind: "kernel-mutation", target: "entity", proof: "interaction" },
  { id: "mcp.retry", kind: "repeatable", target: "entity", proof: "interaction" },
  { id: "mcp.trust", kind: "kernel-mutation", target: "entity", proof: "interaction" },
  { id: "mcp.signin", kind: "kernel-mutation", target: "entity", proof: "interaction" },
  { id: "mcp.upgrade", kind: "kernel-mutation", target: "entity", proof: "interaction" },
  { id: "mcp.follow", kind: "kernel-mutation", target: "entity", proof: "interaction" },
  { id: "mcp.unfollow", kind: "kernel-mutation", target: "entity", proof: "interaction" },
  { id: "mcp.remove", kind: "destructive", target: "entity", proof: "interaction" },
  { id: "mcp.load", kind: "kernel-mutation", target: "entity", proof: "interaction" },
  { id: "mcp.tool-search", kind: "view", target: "entity", proof: "interaction" },
  { id: "mcp.tool-search-clear", kind: "view", target: "entity", proof: "interaction" },

  // ── The window itself ────────────────────────────────────────────────────
  // A mock that records the call proves the wiring and nothing about the
  // window, so these do not settle below browser.
  { id: "window.minimize", kind: "shell-native", target: "none", proof: "browser" },
  { id: "window.maximize", kind: "shell-native", target: "none", proof: "browser" },
  { id: "window.close", kind: "shell-native", target: "none", proof: "browser" },

  // ── Tier A: a click that writes. Which of the surface is here was decided
  //    by the transport verb behind it — a port method reaching post/patch/del
  //    changes canonical state, one reaching get does not, and no name or
  //    return type says which.
  // Asking again for a read that was refused. Nothing changes on either side,
  // and the refusal that put the button on screen is what makes it worth one.
  { id: "account.reload", kind: "repeatable", target: "none", proof: "interaction" },
  { id: "account.sign-in", kind: "kernel-mutation", target: "none", proof: "interaction" },
  { id: "account.sign-out", kind: "kernel-mutation", target: "none", proof: "interaction" },
  { id: "community.join", kind: "shell-native", target: "none", proof: "interaction" },
  { id: "community.discord", kind: "shell-native", target: "none", proof: "interaction" },
  { id: "community.issues", kind: "shell-native", target: "none", proof: "interaction" },
  { id: "community.contributors", kind: "shell-native", target: "none", proof: "interaction" },
  { id: "community.profile", kind: "shell-native", target: "entity", proof: "interaction" },
  { id: "community.douyin-qr", kind: "view", target: "none", proof: "interaction" },
  { id: "community.author", kind: "shell-native", target: "none", proof: "interaction" },
  { id: "community.more", kind: "view", target: "none", proof: "interaction" },
  { id: "sponsor.paypal", kind: "shell-native", target: "none", proof: "interaction" },
  { id: "sponsor.qr", kind: "view", target: "none", proof: "interaction" },
  { id: "feedback.open", kind: "view", target: "none", proof: "interaction" },
  { id: "feedback.tab", kind: "view", target: "none", proof: "interaction" },
  { id: "feedback.category", kind: "view", target: "none", proof: "interaction" },
  { id: "feedback.body", kind: "view", target: "none", proof: "interaction" },
  { id: "feedback.preview", kind: "view", target: "none", proof: "interaction" },
  { id: "feedback.name", kind: "view", target: "none", proof: "interaction" },
  { id: "feedback.contact", kind: "view", target: "none", proof: "interaction" },
  { id: "feedback.shot.pick", kind: "view", target: "none", proof: "interaction" },
  { id: "feedback.shot.capture", kind: "shell-native", target: "none", proof: "interaction" },
  { id: "side.collapse", kind: "view", target: "none", proof: "interaction" },
  { id: "side.toggle", kind: "view", target: "none", proof: "interaction" },
  { id: "side.quote", kind: "view", target: "none", proof: "interaction" },
  { id: "side.unquote", kind: "view", target: "none", proof: "interaction" },
  { id: "side.draft", kind: "view", target: "none", proof: "interaction" },
  { id: "side.send", kind: "kernel-mutation", target: "none", proof: "interaction" },
  { id: "side.close", kind: "kernel-mutation", target: "none", proof: "interaction" },
  { id: "toast.report", kind: "view", target: "none", proof: "interaction" },
  { id: "errbar.report", kind: "view", target: "none", proof: "interaction" },
  { id: "toast.dismiss", kind: "view", target: "entity", proof: "interaction" },
  { id: "model.group", kind: "view", target: "entity", proof: "interaction" },
  { id: "model.group-clear", kind: "view", target: "none", proof: "interaction" },
  { id: "feedback.shot.add", kind: "view", target: "none", proof: "interaction" },
  { id: "feedback.shot.remove", kind: "view", target: "entity", proof: "interaction" },
  { id: "feedback.send", kind: "kernel-mutation", target: "none", proof: "interaction" },
  { id: "feedback.another", kind: "view", target: "none", proof: "interaction" },
  { id: "feedback.thread.more", kind: "view", target: "none", proof: "interaction" },
  { id: "feedback.thread.toggle", kind: "kernel-mutation", target: "entity", proof: "interaction" },
  { id: "feedback.reply.open", kind: "view", target: "none", proof: "interaction" },
  { id: "feedback.reply.body", kind: "view", target: "none", proof: "interaction" },
  { id: "feedback.reply.send", kind: "kernel-mutation", target: "none", proof: "interaction" },
  { id: "feedback.reply.cancel", kind: "view", target: "none", proof: "interaction" },
  { id: "feedback.mine", kind: "view", target: "none", proof: "interaction" },
  { id: "feedback.logs", kind: "shell-native", target: "none", proof: "interaction" },
  { id: "feedback.refresh", kind: "repeatable", target: "none", proof: "interaction" },
  { id: "feedback.account-section", kind: "view", target: "none", proof: "interaction" },
  { id: "feedback.filter", kind: "view", target: "none", proof: "interaction" },
  { id: "feedback.account-link", kind: "view", target: "none", proof: "interaction" },
  { id: "feedback.account-link-confirm", kind: "kernel-mutation", target: "none", proof: "interaction" },
  { id: "feedback.account-link-cancel", kind: "view", target: "none", proof: "interaction" },
  { id: "feedback.board-join-choice", kind: "view", target: "none", proof: "interaction" },
  { id: "feedback.board-offer-dismiss", kind: "view", target: "none", proof: "interaction" },
  { id: "feedback.account-unlink", kind: "kernel-mutation", target: "none", proof: "interaction" },
  { id: "feedback.account-unlink-other", kind: "kernel-mutation", target: "entity", proof: "interaction" },
  { id: "feedback.board-join", kind: "kernel-mutation", target: "none", proof: "interaction" },
  { id: "feedback.board-leave", kind: "kernel-mutation", target: "none", proof: "interaction" },
  { id: "feedback.board-release", kind: "view", target: "none", proof: "interaction" },
  { id: "feedback.board-name", kind: "view", target: "none", proof: "interaction" },
  { id: "feedback.retry-env", kind: "repeatable", target: "none", proof: "interaction" },
  { id: "feedback.retry-mine", kind: "repeatable", target: "none", proof: "interaction" },
  { id: "feedback.withdraw-open", kind: "view", target: "entity", proof: "interaction" },
  { id: "feedback.withdraw-cancel", kind: "view", target: "entity", proof: "interaction" },
  { id: "feedback.withdraw-confirm", kind: "kernel-mutation", target: "entity", proof: "interaction" },
  { id: "paste.save", kind: "kernel-mutation", target: "none", proof: "interaction" },
  { id: "feedback.link", kind: "shell-native", target: "none", proof: "interaction" },
  { id: "feedback.focus", kind: "view", target: "none", proof: "interaction" },
  { id: "feedback.expand", kind: "view", target: "none", proof: "interaction" },
  { id: "feedback.close", kind: "view", target: "none", proof: "interaction" },
  { id: "backup.category", kind: "view", target: "entity", proof: "interaction" },
  { id: "backup.label", kind: "view", target: "none", proof: "interaction" },
  { id: "backup.passphrase", kind: "view", target: "optional", proof: "interaction" },
  { id: "backup.create", kind: "kernel-mutation", target: "none", proof: "interaction" },
  { id: "backup.file-export", kind: "kernel-mutation", target: "none", proof: "interaction" },
  { id: "backup.file-import", kind: "view", target: "none", proof: "interaction" },
  { id: "backup.file-choose", kind: "view", target: "none", proof: "interaction" },
  { id: "backup.restore", kind: "view", target: "entity", proof: "interaction" },
  { id: "backup.delete", kind: "destructive", target: "entity", proof: "interaction" },
  { id: "backup.preview", kind: "repeatable", target: "none", proof: "interaction" },
  { id: "backup.cancel", kind: "view", target: "none", proof: "interaction" },
  { id: "backup.pick", kind: "view", target: "entity", proof: "interaction" },
  { id: "backup.allow", kind: "view", target: "entity", proof: "interaction" },
  { id: "backup.apply", kind: "kernel-mutation", target: "none", proof: "interaction" },
  { id: "backup.install-plugin", kind: "view", target: "entity", proof: "interaction" },
  { id: "backup.done", kind: "view", target: "none", proof: "interaction" },
  { id: "surface.refresh", kind: "repeatable", target: "none", proof: "interaction" },
  { id: "surface.action", kind: "interaction", target: "entity", proof: "authority-effect" },
  { id: "surface.delivery", kind: "view", target: "none", proof: "interaction" },
  { id: "surface.explore", kind: "view", target: "none", proof: "interaction" },
  { id: "surface.edit", kind: "view", target: "entity", proof: "interaction" },
  { id: "ask.answer", kind: "interaction", target: "none", proof: "authority-effect" },
  { id: "ask.step", kind: "view", target: "none", proof: "interaction" },
  { id: "code.download", kind: "interaction", target: "none", proof: "interaction" },
  { id: "code.fold", kind: "view", target: "none", proof: "interaction" },
  { id: "remote-ask.answer", kind: "interaction", target: "none", proof: "interaction" },
  { id: "browser.login", kind: "interaction", target: "none", proof: "interaction" },
  { id: "browser.trust-certificate", kind: "shell-native", target: "none", proof: "interaction" },
  { id: "memory.reload", kind: "repeatable", target: "none", proof: "interaction" },
  { id: "memory.save", kind: "kernel-mutation", target: "entity", proof: "interaction" },
  { id: "memory.restore", kind: "kernel-mutation", target: "entity", proof: "interaction" },
  { id: "memory.forget", kind: "destructive", target: "entity", proof: "interaction" },
  { id: "config.repair", kind: "kernel-mutation", target: "none", proof: "interaction" },
  { id: "extensions.reload", kind: "kernel-mutation", target: "none", proof: "interaction" },
  { id: "extensions.refresh", kind: "repeatable", target: "none", proof: "interaction" },
  { id: "extensions.inspect", kind: "kernel-mutation", target: "none", proof: "interaction" },
  { id: "extensions.install", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "extensions.update", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "extensions.back", kind: "navigation", target: "none", proof: "interaction" },
  { id: "extensions.cancel", kind: "navigation", target: "none", proof: "interaction" },
  { id: "extensions.finish", kind: "navigation", target: "none", proof: "interaction" },
  { id: "extensions.add", kind: "navigation", target: "none", proof: "interaction" },
  { id: "extensions.pick-folder", kind: "shell-native", target: "none", proof: "interaction" },
  { id: "folders.pick", kind: "shell-native", target: "none", proof: "interaction" },
  { id: "folders.add", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "folders.remove", kind: "kernel-mutation", target: "entity", proof: "authority-effect" },
  { id: "folders.retry", kind: "repeatable", target: "none", proof: "interaction" },
  { id: "extensions.export", kind: "repeatable", target: "none", proof: "interaction" },
  { id: "extensions.enabled", kind: "kernel-mutation", target: "entity", proof: "authority-effect" },
  { id: "extensions.remove", kind: "destructive", target: "entity", proof: "authority-effect" },
  { id: "extensions.tab", kind: "view", target: "none", proof: "interaction" },
  { id: "extensions.search", kind: "view", target: "none", proof: "interaction" },
  { id: "extensions.clear-search", kind: "view", target: "none", proof: "interaction" },
  ...MARKET_ACTIONS,
  // Taking back the innermost open thing: a popover, an inline form. One
  // intent, reached by pressing away and by Escape.
  { id: "layer.dismiss", kind: "navigation", target: "none", proof: "interaction" },
  { id: "file-drop.finish", kind: "view", target: "none", proof: "interaction" },
  // Walking the sidebar tree. The arrows only move focus; what Enter reaches is
  // the row's own action, so this one never writes anything itself.
  { id: "tree.navigate", kind: "view", target: "none", proof: "interaction" },
  { id: "settings.close", kind: "navigation", target: "none", proof: "interaction" },
  { id: "settings.expand", kind: "view", target: "none", proof: "interaction" },
  { id: "pane.activate", kind: "navigation", target: "none", proof: "interaction" },
  // How this session is being read. One id across a bar button and a menu row,
  // because reaching a view from the bar and from the detail menu is one intent
  // in two places, not two things a person can do.
  { id: "pane.view", kind: "view", target: "none", proof: "interaction" },
  { id: "analysis.signal", kind: "view", target: "entity", proof: "interaction" },
  { id: "analysis.row", kind: "view", target: "entity", proof: "interaction" },
  // The agent's browser as a person watches it: which of its pages is drawn,
  // and the controls a person has over the page on screen. They reach the
  // window's view of the page, never the kernel.
  { id: "browser.tab", kind: "view", target: "entity", proof: "interaction" },
  { id: "browser.control", kind: "shell-native", target: "entity", proof: "interaction" },
  { id: "browser.navigate", kind: "shell-native", target: "entity", proof: "interaction" },
  { id: "transcript.find", kind: "view", target: "none", proof: "interaction" },
  { id: "turn.edit", kind: "view", target: "entity", proof: "interaction" },
  { id: "turn.resend", kind: "kernel-mutation", target: "entity", proof: "authority-effect" },
  { id: "session.new", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  // The bytes reach the host as the file is chosen, before anything is sent:
  // the picker's button only opens a dialog, and this is where the upload is.
  { id: "session.attach", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "capability.scope", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "model.select", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "network.save", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "onboarding.connect", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "transcript.scroll", kind: "view", target: "none", proof: "interaction" },
  { id: "onboarding.protocol", kind: "view", target: "none", proof: "interaction" },
  { id: "onboarding.skip", kind: "view", target: "none", proof: "interaction" },
  { id: "onboarding.reopen", kind: "view", target: "none", proof: "interaction" },
  // Handing the reader to a browser. The vendor's console during setup and a
  // link in the transcript are the same thing to do, and neither moves any
  // state — which is why it is one id and why it is not a mutation.
  { id: "external.open", kind: "shell-native", target: "none", proof: "interaction" },
  { id: "remote-host.remove", kind: "destructive", target: "none", proof: "authority-effect" },
  { id: "remote-host.move", kind: "kernel-mutation", target: "entity", proof: "authority-effect" },
  { id: "workspace.reveal", kind: "shell-native", target: "entity", proof: "interaction" },
  { id: "workspace.open-with", kind: "shell-native", target: "entity", proof: "interaction" },
  { id: "workbench.open-with-menu", kind: "view", target: "none", proof: "interaction" },
  { id: "workspace.remove", kind: "destructive", target: "none", proof: "authority-effect" },
  { id: "extensions.invoke", kind: "kernel-mutation", target: "none", proof: "authority-effect" },

  // ── Opening what a person already has ────────────────────────────────────
  // Reached from the workspace tree and from the remote host tree, which are
  // two surfaces for one intent; opening an existing session is not starting a
  // new one, and the host asked differs.
  { id: "peer.thread-open", kind: "navigation", target: "none", proof: "interaction" },
  { id: "session.open", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "remote.open", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  // Asking a machine whether it answers. Nothing on either side changes, and
  // asking twice is the point when the first answer was "not yet".
  { id: "remote.probe", kind: "repeatable", target: "entity", proof: "authority-effect" },
  { id: "remote.hide", kind: "view", target: "entity", proof: "interaction" },
  { id: "remote.enable", kind: "view", target: "entity", proof: "interaction" },
  { id: "remote.read", kind: "repeatable", target: "entity", proof: "authority-effect" },
  { id: "workspace.add", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "workspace.fold-all", kind: "view", target: "none", proof: "interaction" },
  { id: "workspace.add-cancel", kind: "view", target: "none", proof: "interaction" },
  { id: "root-missing.reselect", kind: "view", target: "none", proof: "interaction" },
  { id: "workspace.move", kind: "kernel-mutation", target: "entity", proof: "authority-effect" },
  { id: "session.move", kind: "kernel-mutation", target: "entity", proof: "authority-effect" },
  { id: "session.move-to-start", kind: "view", target: "entity", proof: "interaction" },
  { id: "session.move-to", kind: "kernel-mutation", target: "entity", proof: "authority-effect" },
  { id: "session.move-to-cancel", kind: "view", target: "none", proof: "interaction" },
  { id: "session.move-to-target", kind: "view", target: "none", proof: "interaction" },
  { id: "workspace.reorder", kind: "kernel-mutation", target: "entity", proof: "authority-effect" },

  // ── Taking a change back ─────────────────────────────────────────────────
  // Planning a revert and performing one are two contracts: the first asks the
  // host what would change and the second overwrites the file, and a person who
  // stops between them has done nothing. Which answer a person gave to the
  // "changed since the checkpoint" question rides on data-value.
  { id: "file-revert.prepare", kind: "kernel-mutation", target: "entity", proof: "authority-effect" },
  { id: "file-revert.commit", kind: "destructive", target: "entity", proof: "authority-effect" },
  { id: "rewind.prepare", kind: "kernel-mutation", target: "entity", proof: "authority-effect" },
  { id: "rewind.commit", kind: "destructive", target: "entity", proof: "authority-effect" },
  { id: "session.reload", kind: "repeatable", target: "entity", proof: "interaction" },
  { id: "rewind.undo", kind: "kernel-mutation", target: "entity", proof: "authority-effect" },
  { id: "rewind.notice-dismiss", kind: "view", target: "entity", proof: "interaction" },
  { id: "extensions.submit", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "provider.probe", kind: "repeatable", target: "none", proof: "interaction" },
  { id: "provider.draft", kind: "interaction", target: "none", proof: "interaction" },
  { id: "provider.price-source", kind: "shell-native", target: "entity", proof: "interaction" },
  { id: "provider.discard-edits", kind: "destructive", target: "none", proof: "interaction" },
  { id: "provider.revert", kind: "interaction", target: "none", proof: "interaction" },
  { id: "provider.model-check-all", kind: "repeatable", target: "none", proof: "interaction" },
  { id: "provider.model-check", kind: "repeatable", target: "entity", proof: "interaction" },
  { id: "provider.add", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "provider.preset", kind: "view", target: "entity", proof: "interaction" },
  { id: "provider.preset-retry", kind: "repeatable", target: "none", proof: "interaction" },
  { id: "provider.preset-toggle", kind: "view", target: "none", proof: "interaction" },
  { id: "provider.preset-back", kind: "view", target: "none", proof: "interaction" },
  { id: "provider.preset-cancel", kind: "view", target: "none", proof: "interaction" },
  { id: "provider.add-start", kind: "view", target: "none", proof: "interaction" },
  { id: "provider.select", kind: "view", target: "entity", proof: "interaction" },
  { id: "provider.move", kind: "view", target: "entity", proof: "interaction" },
  { id: "provider.rename-start", kind: "view", target: "entity", proof: "interaction" },
  { id: "provider.rename", kind: "kernel-mutation", target: "entity", proof: "interaction" },
  { id: "provider.save", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "roles.model", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "roles.effort", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "roles.override.clear", kind: "kernel-mutation", target: "entity", proof: "interaction" },
  { id: "permissions.rule-level", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "sandbox.workspace-root", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  // Where compaction starts. Declaring it rebuilds the runtime, which is why
  // it must not be able to fire twice for one keystroke.
  { id: "compaction.threshold", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  // Which bound is in force is the setting; how much of it is on screen is not.
  { id: "compaction.advanced", kind: "view", target: "none", proof: "interaction" },
  { id: "context.largest", kind: "view", target: "none", proof: "interaction" },
  { id: "compaction.confirm", kind: "view", target: "none", proof: "interaction" },
  { id: "compaction.now", kind: "kernel-mutation", target: "none", proof: "interaction" },
  { id: "panel.move", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "panel.delegate-placement", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "context.window-tokens", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "mcp.add", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "mcp.inspect", kind: "repeatable", target: "none", proof: "interaction" },
  { id: "mcp.scope", kind: "interaction", target: "none", proof: "interaction" },
  { id: "remotes.add", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "remotes.save", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "remotes.remove", kind: "destructive", target: "entity", proof: "authority-effect" },
  { id: "remotes.kind", kind: "view", target: "none", proof: "interaction" },
  { id: "remotes.distro", kind: "view", target: "none", proof: "interaction" },
  { id: "share.card", kind: "view", target: "none", proof: "interaction" },
  { id: "device.ask-leave", kind: "view", target: "none", proof: "interaction" },
  { id: "device.leave", kind: "destructive", target: "none", proof: "authority-effect" },
  { id: "share.copy", kind: "shell-native", target: "none", proof: "interaction" },
  { id: "share.cloud-copy", kind: "shell-native", target: "none", proof: "interaction" },
  { id: "share.cloud-offer", kind: "repeatable", target: "none", proof: "interaction" },
  { id: "share.toggle", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "share.address", kind: "view", target: "none", proof: "interaction" },
  { id: "share.port", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "share.offer", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "share.ask-revoke", kind: "view", target: "entity", proof: "interaction" },
  { id: "share.keep", kind: "view", target: "entity", proof: "interaction" },
  { id: "share.revoke", kind: "destructive", target: "entity", proof: "authority-effect" },
  { id: "share.remember", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "versions.reload", kind: "repeatable", target: "none", proof: "interaction" },
  { id: "versions.pin", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "versions.activate", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "versions.restart", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "versions.later", kind: "view", target: "none", proof: "interaction" },
  { id: "versions.more", kind: "view", target: "none", proof: "interaction" },
  { id: "versions.notes", kind: "repeatable", target: "none", proof: "interaction" },
  { id: "versions.notes-retry", kind: "repeatable", target: "none", proof: "interaction" },
  { id: "versions.notes-close", kind: "view", target: "none", proof: "interaction" },
  { id: "versions.notes-lang", kind: "view", target: "none", proof: "interaction" },
  { id: "shell.executor", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "shell.custom-path", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "shell.custom-args", kind: "view", target: "none", proof: "interaction" },
  { id: "hooks.recipe", kind: "kernel-mutation", target: "entity", proof: "authority-effect" },
  { id: "hooks.remove", kind: "destructive", target: "none", proof: "authority-effect" },
  { id: "hooks.run-once", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "hooks.save", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "tray.icon", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "tray.close-to-tray", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "notify.enabled", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "notify.turn-done", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "notify.approval", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "notify.ask", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "notify.feedback-reply", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "notify.disk-low", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "notify.sound", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "phone-notify.enabled", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "phone-notify.approval", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "phone-notify.input", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "phone-notify.error", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "phone.add", kind: "kernel-mutation", target: "entity", proof: "authority-effect" },
  { id: "phone.replace", kind: "kernel-mutation", target: "entity", proof: "authority-effect" },
  { id: "phone.reject", kind: "kernel-mutation", target: "entity", proof: "authority-effect" },
  { id: "phone.revoke", kind: "destructive", target: "entity", proof: "authority-effect" },
  { id: "phone.revoke-ask", kind: "view", target: "entity", proof: "interaction" },
  { id: "workspace.editor", kind: "repeatable", target: "none", proof: "authority-effect" },
  { id: "wallpaper.remove", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "wallpaper.change", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "theme.activate", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "theme.import", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "theme.folder", kind: "repeatable", target: "none", proof: "interaction" },
  { id: "theme.remove", kind: "destructive", target: "entity", proof: "authority-effect" },

  // Usage changes what report is read, not canonical session state.
  { id: "usage.range.today", kind: "view", target: "none", proof: "interaction" },
  { id: "usage.range.start", kind: "view", target: "none", proof: "interaction" },
  { id: "usage.range.end", kind: "view", target: "none", proof: "interaction" },
  { id: "usage.range.month", kind: "view", target: "none", proof: "interaction" },

  // ── How the window looks ─────────────────────────────────────────────────
  // Named for what a person is doing, not for the control they reached for.
  // The interface scale has a row of presets and a slider beside it, and both
  // are the same intent; changing the background's darkening is not a
  // different capability from changing its opacity, so the four sliders share
  // one id and say which parameter on data-value.
  //
  // These stay apart on purpose. theme.activate installs a palette pack;
  // appearance.scheme is light, dark or follow-the-system. wallpaper.change
  // and wallpaper.remove decide which image there is; appearance.background is
  // how the image that is already there is drawn. And weight and contrast are
  // two different things a reader wants, however alike they look in CSS.
  { id: "appearance.language", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "appearance.zoom", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "web-access.backend", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "web-access.move", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "web-access.consent", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "web-access.probe", kind: "repeatable", target: "none", proof: "interaction" },
  { id: "web-access.searxng-url", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "trace-export.enabled", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "trace-export.endpoint", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "trace-export.header-name", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "trace-export.header-env", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "trace-export.service-name", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "web-access.video-url", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "appearance.reading-size", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "appearance.text-size", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "appearance.measure", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  // Dragging the column's edge is a view adjustment held on this machine,
  // not a setting written to the kernel: it changes what is drawn, not what
  // the transcript says.
  { id: "appearance.reading-width", kind: "view", target: "none", proof: "interaction" },
  { id: "appearance.background", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "appearance.font", kind: "kernel-mutation", target: "entity", proof: "authority-effect" },
  // Kept where the window keeps them: these are this browser's, and no kernel
  // state moves.
  { id: "appearance.scheme", kind: "view", target: "none", proof: "browser" },
  { id: "appearance.weight", kind: "view", target: "none", proof: "browser" },
  { id: "appearance.contrast", kind: "view", target: "none", proof: "browser" },
  { id: "appearance.effects", kind: "view", target: "none", proof: "browser" },
  { id: "pane.close", kind: "destructive", target: "none", proof: "authority-effect" },
  { id: "settings.section", kind: "navigation", target: "none", proof: "interaction" },
  // Finding a setting changes what is on screen and nothing else: it reaches
  // no kernel state, and the jump it offers is settings.section, because
  // arriving from a search is the same arrival as arriving from the nav.
  { id: "settings.search", kind: "view", target: "none", proof: "interaction" },
  { id: "workspace.isolate", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "reasoning.effort", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "workbench.new", kind: "view", target: "none", proof: "interaction" },
  { id: "workbench.launch-files", kind: "view", target: "none", proof: "interaction" },
  { id: "workbench.launch-changes", kind: "view", target: "none", proof: "interaction" },
  { id: "workbench.launch-browser", kind: "view", target: "none", proof: "interaction" },
  { id: "workbench.history-reload", kind: "repeatable", target: "none", proof: "interaction" },
  { id: "workbench.tab", kind: "view", target: "entity", proof: "interaction" },
  { id: "workbench.browser-menu", kind: "view", target: "entity", proof: "interaction" },
  { id: "workbench.browser-close", kind: "kernel-mutation", target: "entity", proof: "authority-effect" },
  { id: "workbench.close", kind: "kernel-mutation", target: "entity", proof: "authority-effect" },
  { id: "workbench.mode", kind: "view", target: "none", proof: "interaction" },
  { id: "workbench.save", kind: "kernel-mutation", target: "entity", proof: "authority-effect" },
  { id: "workbench.search", kind: "view", target: "none", proof: "interaction" },
  { id: "workbench.clear-search", kind: "view", target: "none", proof: "interaction" },
  { id: "workbench.folder", kind: "view", target: "entity", proof: "interaction" },
  { id: "workbench.diff", kind: "view", target: "entity", proof: "interaction" },
  { id: "files.open", kind: "navigation", target: "none", proof: "interaction" },
  { id: "workbench.files", kind: "view", target: "none", proof: "interaction" },
  { id: "workbench.hidden", kind: "view", target: "none", proof: "interaction" },
  { id: "workbench.reveal", kind: "shell-native", target: "optional", proof: "interaction" },
  { id: "workbench.rename-tab", kind: "view", target: "entity", proof: "interaction" },
  { id: "workbench.open-file", kind: "shell-native", target: "entity", proof: "interaction" },
  { id: "workbench.copy-path", kind: "shell-native", target: "entity", proof: "interaction" },
  { id: "workbench.add-to-chat", kind: "view", target: "entity", proof: "interaction" },
  { id: "workbench.quote-selection", kind: "view", target: "none", proof: "interaction" },
  { id: "workbench.copy-ref", kind: "shell-native", target: "entity", proof: "interaction" },
  { id: "workbench.menu", kind: "view", target: "entity", proof: "interaction" },
  { id: "palette.pick", kind: "view", target: "entity", proof: "interaction" },
  { id: "palette.search", kind: "view", target: "none", proof: "interaction" },
  { id: "reply.quote", kind: "view", target: "entity", proof: "interaction" },
  { id: "reply.branch", kind: "kernel-mutation", target: "entity", proof: "interaction" },
  { id: "reply.selection.menu", kind: "view", target: "entity", proof: "interaction" },
  { id: "reply.retry", kind: "view", target: "entity", proof: "interaction" },
  { id: "reply.retry-now", kind: "kernel-mutation", target: "entity", proof: "interaction" },
  { id: "reply.configure", kind: "view", target: "none", proof: "interaction" },
  { id: "reply.more", kind: "view", target: "entity", proof: "interaction" },
  { id: "reply.download", kind: "view", target: "entity", proof: "interaction" },
  { id: "reply.run-detail", kind: "view", target: "none", proof: "interaction" },
  { id: "deck.agents", kind: "view", target: "none", proof: "interaction" },
  { id: "deck.jobs", kind: "view", target: "none", proof: "interaction" },
  { id: "plan.fold", kind: "view", target: "none", proof: "interaction" },
  { id: "workbench.file", kind: "view", target: "entity", proof: "interaction" },
  { id: "tool-approval.mode", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  // The folder-trust question and the no-sandbox note beside the composer.
  { id: "workspace.trust", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "posture-note.dismiss", kind: "view", target: "none", proof: "interaction" },
  { id: "storage.move", kind: "kernel-mutation", target: "none", proof: "interaction" },
  { id: "session.import-legacy", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "session.import-native", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "network.diagnose", kind: "repeatable", target: "none", proof: "interaction" },

  // ── Permissions, the sandbox, and the endpoints a session can reach.
  { id: "permissions.recipe", kind: "kernel-mutation", target: "entity", proof: "authority-effect" },
  { id: "permissions.mode", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "permissions.add-rule", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "permissions.remove-rule", kind: "destructive", target: "entity", proof: "authority-effect" },
  { id: "sandbox.mode", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "sandbox.network", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "sandbox.protect-changed-files", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "sandbox.add-write-root", kind: "kernel-mutation", target: "optional", proof: "authority-effect" },
  { id: "sandbox.remove-write-root", kind: "destructive", target: "entity", proof: "authority-effect" },
  { id: "browser-tools.enabled", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "present-files.enabled", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "size.set", kind: "kernel-mutation", target: "none", proof: "interaction" },
  { id: "size.keep", kind: "view", target: "none", proof: "interaction" },
  { id: "size.restore", kind: "kernel-mutation", target: "none", proof: "interaction" },
  { id: "tree.retry", kind: "kernel-mutation", target: "none", proof: "interaction" },
  { id: "update.mode", kind: "kernel-mutation", target: "none", proof: "interaction" },
  { id: "update.restart", kind: "kernel-mutation", target: "none", proof: "interaction" },
  { id: "update.later", kind: "view", target: "none", proof: "interaction" },
  { id: "skills.update-open", kind: "view", target: "entity", proof: "interaction" },
  { id: "skills.update-close", kind: "view", target: "none", proof: "interaction" },
  { id: "skills.update-overwrite", kind: "view", target: "none", proof: "interaction" },
  { id: "skills.update-hidden", kind: "view", target: "none", proof: "interaction" },
  { id: "skills.update-reload", kind: "view", target: "none", proof: "interaction" },
  { id: "skills.update-apply", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "skills.remove-open", kind: "view", target: "entity", proof: "interaction" },
  { id: "skills.remove-cancel", kind: "view", target: "none", proof: "interaction" },
  { id: "skills.remove-reload", kind: "view", target: "none", proof: "interaction" },
  { id: "skills.remove-confirm", kind: "destructive", target: "none", proof: "authority-effect" },
  { id: "subagent-claims.optimistic", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "subagent-steer.max-bytes", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "subagent-steer.per-minute", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "write-lease.optimistic", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "write-lease.collaboration", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "write-lease.auto-integrate", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  ...ISOLATION_ACTIONS,
  { id: "write-lease.max-wait", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "workbench.stage-folder", kind: "kernel-mutation", target: "entity", proof: "authority-effect" },
  { id: "workbench.change-folder", kind: "view", target: "entity", proof: "interaction" },
  { id: "workbench.more-changes", kind: "view", target: "none", proof: "interaction" },
  { id: "workbench.discard", kind: "view", target: "entity", proof: "interaction" },
  { id: "workbench.discard-confirm", kind: "kernel-mutation", target: "entity", proof: "authority-effect" },
  { id: "workbench.discard-cancel", kind: "view", target: "none", proof: "interaction" },
  { id: "workbench.repo", kind: "view", target: "entity", proof: "interaction" },
  { id: "workbench.repos-reload", kind: "view", target: "none", proof: "interaction" },
  { id: "workbench.commit", kind: "view", target: "entity", proof: "interaction" },
  { id: "workbench.commit-file", kind: "view", target: "entity", proof: "interaction" },
  { id: "push.preview", kind: "view", target: "none", proof: "interaction" },
  { id: "push.confirm", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "push.close", kind: "view", target: "none", proof: "interaction" },
  { id: "permissions.ask-on-external-origin", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "plan-approval.auto", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "default-mode.plan", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "tool-list.toggle", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "peers.target", kind: "view", target: "none", proof: "interaction" },
  { id: "peers.text", kind: "view", target: "none", proof: "interaction" },
  { id: "peers.expand", kind: "view", target: "none", proof: "interaction" },
  { id: "peers.send", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "peers.approve", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "peers.approve-project", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "peers.enable", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "peers.dispatch", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "peers.release-revoke", kind: "kernel-mutation", target: "entity", proof: "authority-effect" },
  { id: "peers.release-add", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "peers.release-pick", kind: "view", target: "none", proof: "interaction" },
  { id: "peers.deny", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  // Read by the running turn at its next round, so no rebuild stands between the
  // click and the canonical change.
  { id: "auto-archive.notice-dismiss", kind: "view", target: "none", proof: "interaction" },
  { id: "auto-archive.enabled", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "auto-archive.days", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  // The persona editor edits a draft; only the save reaches the kernel, and the
  // persona is composed once at boot so the write is read by later sessions.
  { id: "persona.prefix", kind: "view", target: "none", proof: "interaction" },
  { id: "persona.suffix", kind: "view", target: "none", proof: "interaction" },
  { id: "persona.preset", kind: "view", target: "optional", proof: "interaction" },
  { id: "persona.save", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "persona.runtime-context", kind: "view", target: "none", proof: "interaction" },
  // The kernel announces the change on the stream, so no rebuild stands between
  // the click and the canonical change.
  { id: "currency.mode", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "ask-timeout.enabled", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "ask-timeout.seconds", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "session-ref.tokens", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "session-ref.reset", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "session.reference-match", kind: "view", target: "entity", proof: "interaction" },
  { id: "mods.enabled", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "mods.budget", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "progress-watch.pause", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "progress-watch.rounds", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "progress-watch.token-multiple", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  // The stall strip beside the composer. Stopping is the ordinary interrupt and
  // continuing is an ordinary message; muting and dismissing touch this window.
  { id: "stall.stop", kind: "kernel-mutation", target: "none", proof: "interaction" },
  { id: "stall.continue", kind: "kernel-mutation", target: "none", proof: "interaction" },
  { id: "stall.mute", kind: "view", target: "none", proof: "interaction" },
  { id: "stall.dismiss", kind: "view", target: "none", proof: "interaction" },
  { id: "provider.remove", kind: "destructive", target: "entity", proof: "interaction" },
  { id: "provider.protocol", kind: "kernel-mutation", target: "entity", proof: "interaction" },

  // ── The two side panels. Reached from the keyboard here, and from each
  //    gutter's own grip, which this pass has not annotated yet.
  { id: "rail.star", kind: "view", target: "none", proof: "interaction" },
  { id: "rail.toggle", kind: "view", target: "none", proof: "interaction" },
  ...SCHEDULE_ACTIONS,
  ...DOCUMENT_ACTIONS,
];

// No action in Studio is reachable from the keyboard alone: every shortcut in
// the table has a control on screen that means the same thing. So there is no
// "keyboard-only" flag here — the day one exists, it is a field with a reader.
