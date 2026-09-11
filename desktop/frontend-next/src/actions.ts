// What Studio claims a person can do, as a set of stable identities.
//
// The oracle for the census, and deliberately not something production imports:
// a button writes the id as a literal and this file writes it again, so the two
// can disagree and be caught. Sharing one constant would make the check compare
// a value with itself — the shape of false green this tree keeps removing.
//
// An id is <surface>.<intent>: the semantic class, never the instance and never
// the wording. Which entry a click is about rides on data-target, and which of
// several answers it gives rides on data-value, so a row rendered seventy times
// is still one contract, and 「取消」 — seventeen buttons, eight destinations —
// is never an identity.

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
  // ── The window's own switches, and the way into the turn's policy ────────
  // chrome.preset keeps the id it was registered under when the preset still
  // lived in the title bar: where a control is drawn is not its identity,
  // which is why the settings sheet has always rendered it too. The composer's
  // panel renders it beside reasoning.effort and tool-approval.mode, which the
  // settings sheet already owned — one question, three ids that already exist.
  { id: "chrome.preset", kind: "kernel-mutation", target: "none", proof: "interaction" },
  { id: "chrome.policy", kind: "view", target: "none", proof: "interaction" },
  { id: "chrome.focus", kind: "view", target: "none", proof: "browser" },
  { id: "chrome.settings", kind: "navigation", target: "none", proof: "interaction" },
  // The way out of a window whose assets an update replaced underneath it.
  { id: "chrome.reload", kind: "navigation", target: "none", proof: "interaction" },
  { id: "chrome.account", kind: "navigation", target: "none", proof: "interaction" },

  // ── The turn ─────────────────────────────────────────────────────────────
  // Send and stop remain two actions even when they sit together: during a
  // live turn the first steers and the second cancels the current turn.
  { id: "session.send", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "session.stop", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "plan.mode", kind: "kernel-mutation", target: "none", proof: "interaction" },

  // ── What is waiting to be sent ───────────────────────────────────────────
  { id: "queue.edit", kind: "kernel-mutation", target: "entity", proof: "interaction" },
  // Giving a session a name of your own. The sidebar's tree and the pane tabs
  // rename the same session through the same host call, so it is one id.
  { id: "session.rename", kind: "kernel-mutation", target: "entity", proof: "authority-effect" },
  { id: "queue.cancel", kind: "kernel-mutation", target: "entity", proof: "interaction" },
  { id: "queue.retry", kind: "repeatable", target: "entity", proof: "interaction" },
  { id: "queue.refresh", kind: "repeatable", target: "entity", proof: "interaction" },
  { id: "queue.move", kind: "kernel-mutation", target: "entity", proof: "interaction" },
  { id: "queue.pause", kind: "kernel-mutation", target: "none", proof: "interaction" },

  // ── Answers the host is blocked on. One id per port call, with the verdict
  //    on data-value: the kernel takes one decision with an answer, not three
  //    calls named after button text.
  { id: "decision.tool", kind: "interaction", target: "entity", proof: "authority-effect" },
  { id: "decision.plan", kind: "interaction", target: "entity", proof: "authority-effect" },

  // ── Capabilities ─────────────────────────────────────────────────────────
  { id: "skill.enabled", kind: "kernel-mutation", target: "entity", proof: "authority-effect" },
  { id: "mcp.enabled", kind: "kernel-mutation", target: "entity", proof: "interaction" },
  { id: "mcp.retry", kind: "repeatable", target: "entity", proof: "interaction" },
  { id: "mcp.remove", kind: "destructive", target: "entity", proof: "interaction" },

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
  { id: "ask.answer", kind: "interaction", target: "none", proof: "authority-effect" },
  { id: "remote-ask.answer", kind: "interaction", target: "none", proof: "interaction" },
  { id: "memory.reload", kind: "repeatable", target: "none", proof: "interaction" },
  { id: "memory.save", kind: "kernel-mutation", target: "entity", proof: "interaction" },
  { id: "memory.restore", kind: "kernel-mutation", target: "entity", proof: "interaction" },
  { id: "memory.forget", kind: "destructive", target: "entity", proof: "interaction" },
  { id: "config.repair", kind: "kernel-mutation", target: "none", proof: "interaction" },
  { id: "extensions.reload", kind: "kernel-mutation", target: "none", proof: "interaction" },
  { id: "extensions.inspect", kind: "kernel-mutation", target: "none", proof: "interaction" },
  { id: "extensions.install", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "extensions.update", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "extensions.back", kind: "navigation", target: "none", proof: "interaction" },
  { id: "extensions.cancel", kind: "navigation", target: "none", proof: "interaction" },
  { id: "extensions.finish", kind: "navigation", target: "none", proof: "interaction" },
  { id: "extensions.add", kind: "navigation", target: "none", proof: "interaction" },
  { id: "extensions.export", kind: "repeatable", target: "none", proof: "interaction" },
  { id: "extensions.enabled", kind: "kernel-mutation", target: "entity", proof: "authority-effect" },
  { id: "extensions.remove", kind: "destructive", target: "entity", proof: "authority-effect" },
  // Taking back the innermost open thing: a popover, an inline form. One
  // intent, reached by pressing away and by Escape.
  { id: "layer.dismiss", kind: "navigation", target: "none", proof: "interaction" },
  { id: "settings.close", kind: "navigation", target: "none", proof: "interaction" },
  { id: "pane.activate", kind: "navigation", target: "none", proof: "interaction" },
  // How this session is being read. One id across a bar button and a menu row,
  // because reaching a view from the bar and from the detail menu is one intent
  // in two places, not two things a person can do.
  { id: "pane.view", kind: "view", target: "none", proof: "interaction" },
  { id: "transcript.scroll", kind: "navigation", target: "none", proof: "browser" },
  { id: "session.new", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  // The bytes reach the host as the file is chosen, before anything is sent:
  // the picker's button only opens a dialog, and this is where the upload is.
  { id: "session.attach", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "capability.scope", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "model.select", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "network.save", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "onboarding.connect", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  // Handing the reader to a browser. The vendor's console during setup and a
  // link in the transcript are the same thing to do, and neither moves any
  // state — which is why it is one id and why it is not a mutation.
  { id: "external.open", kind: "shell-native", target: "none", proof: "interaction" },
  { id: "remote-host.remove", kind: "destructive", target: "none", proof: "authority-effect" },
  { id: "task.summarize-phase", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "workspace.remove", kind: "destructive", target: "none", proof: "authority-effect" },
  { id: "extensions.invoke", kind: "kernel-mutation", target: "none", proof: "authority-effect" },

  // ── Opening what a person already has ────────────────────────────────────
  // Reached from the workspace tree and from the remote host tree, which are
  // two surfaces for one intent; opening an existing session is not starting a
  // new one, and the host asked differs.
  { id: "session.open", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "remote.open", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  // Asking a machine whether it answers. Nothing on either side changes, and
  // asking twice is the point when the first answer was "not yet".
  { id: "remote.probe", kind: "repeatable", target: "entity", proof: "authority-effect" },
  { id: "workspace.add", kind: "kernel-mutation", target: "none", proof: "authority-effect" },

  // ── Taking a change back ─────────────────────────────────────────────────
  // Planning a revert and performing one are two contracts: the first asks the
  // host what would change and the second overwrites the file, and a person who
  // stops between them has done nothing. Which answer a person gave to the
  // "changed since the checkpoint" question rides on data-value.
  { id: "file-revert.prepare", kind: "kernel-mutation", target: "entity", proof: "authority-effect" },
  { id: "file-revert.commit", kind: "destructive", target: "entity", proof: "authority-effect" },
  { id: "rewind.prepare", kind: "kernel-mutation", target: "entity", proof: "authority-effect" },
  { id: "rewind.commit", kind: "destructive", target: "entity", proof: "authority-effect" },
  { id: "rewind.undo", kind: "kernel-mutation", target: "entity", proof: "authority-effect" },
  { id: "extensions.submit", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "provider.probe", kind: "repeatable", target: "none", proof: "interaction" },
  { id: "provider.model-check", kind: "repeatable", target: "entity", proof: "interaction" },
  { id: "provider.add", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "provider.save", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "roles.model", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "permissions.rule-level", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "sandbox.workspace-root", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  // Where compaction starts. Declaring it rebuilds the runtime, which is why
  // it must not be able to fire twice for one keystroke.
  { id: "compaction.threshold", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  // Which bound is in force is the setting; how much of it is on screen is not.
  { id: "compaction.advanced", kind: "view", target: "none", proof: "interaction" },
  { id: "panel.move", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "panel.delegate-placement", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "context.window-tokens", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "mcp.add", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "mcp.inspect", kind: "repeatable", target: "none", proof: "interaction" },
  { id: "remotes.add", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "remotes.save", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "remotes.remove", kind: "destructive", target: "entity", proof: "authority-effect" },
  { id: "versions.reload", kind: "repeatable", target: "none", proof: "interaction" },
  { id: "versions.pin", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "versions.activate", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "shell.executor", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "shell.custom-path", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "hooks.recipe", kind: "kernel-mutation", target: "entity", proof: "authority-effect" },
  { id: "hooks.remove", kind: "destructive", target: "none", proof: "authority-effect" },
  { id: "hooks.run-once", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "hooks.save", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "tray.icon", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "tray.close-to-tray", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "wallpaper.remove", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "wallpaper.change", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "theme.activate", kind: "kernel-mutation", target: "none", proof: "authority-effect" },

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
  { id: "appearance.reading-size", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "appearance.background", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "appearance.font", kind: "kernel-mutation", target: "entity", proof: "authority-effect" },
  // Kept where the window keeps them: these are this browser's, and no kernel
  // state moves.
  { id: "appearance.scheme", kind: "view", target: "none", proof: "browser" },
  { id: "appearance.weight", kind: "view", target: "none", proof: "browser" },
  { id: "appearance.contrast", kind: "view", target: "none", proof: "browser" },
  { id: "pane.close", kind: "destructive", target: "none", proof: "authority-effect" },
  { id: "settings.section", kind: "navigation", target: "none", proof: "interaction" },
  // Finding a setting changes what is on screen and nothing else: it reaches
  // no kernel state, and the jump it offers is settings.section, because
  // arriving from a search is the same arrival as arriving from the nav.
  { id: "settings.search", kind: "view", target: "none", proof: "interaction" },
  { id: "workspace.isolate", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "reasoning.effort", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "tool-approval.mode", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "storage.move", kind: "kernel-mutation", target: "none", proof: "interaction" },
  { id: "network.diagnose", kind: "repeatable", target: "none", proof: "interaction" },

  // ── Permissions, the sandbox, and the endpoints a session can reach.
  { id: "permissions.recipe", kind: "kernel-mutation", target: "entity", proof: "authority-effect" },
  { id: "permissions.mode", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "permissions.add-rule", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "permissions.remove-rule", kind: "destructive", target: "entity", proof: "authority-effect" },
  { id: "sandbox.mode", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "sandbox.network", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "sandbox.add-write-root", kind: "kernel-mutation", target: "optional", proof: "authority-effect" },
  { id: "sandbox.remove-write-root", kind: "destructive", target: "entity", proof: "authority-effect" },
  { id: "provider.remove", kind: "destructive", target: "entity", proof: "interaction" },
  { id: "provider.protocol", kind: "kernel-mutation", target: "entity", proof: "interaction" },
  { id: "provider.web-search", kind: "kernel-mutation", target: "entity", proof: "interaction" },
  { id: "provider.thinking", kind: "kernel-mutation", target: "entity", proof: "interaction" },

  // ── The two side panels. Reached from the keyboard here, and from each
  //    gutter's own grip, which this pass has not annotated yet.
  { id: "rail.toggle", kind: "view", target: "none", proof: "interaction" },
  { id: "inspector.toggle", kind: "view", target: "none", proof: "interaction" },
];

// No action in Studio is reachable from the keyboard alone: every shortcut in
// the table has a control on screen that means the same thing. So there is no
// "keyboard-only" flag here — the day one exists, it is a field with a reader.
