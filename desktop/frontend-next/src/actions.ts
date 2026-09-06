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
  // ── The chrome: identity, and the two switches always on screen ──────────
  { id: "chrome.preset", kind: "kernel-mutation", target: "none", proof: "interaction" },
  { id: "chrome.focus", kind: "view", target: "none", proof: "browser" },
  { id: "chrome.settings", kind: "navigation", target: "none", proof: "interaction" },
  { id: "chrome.theme", kind: "view", target: "none", proof: "browser" },
  { id: "chrome.account", kind: "navigation", target: "none", proof: "interaction" },

  // ── The turn ─────────────────────────────────────────────────────────────
  // Send and stop share one button and are not one action: what the host is
  // asked to do differs, and so does what proves it happened.
  { id: "session.send", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "session.stop", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "plan.mode", kind: "kernel-mutation", target: "none", proof: "interaction" },

  // ── What is waiting to be sent ───────────────────────────────────────────
  { id: "queue.edit", kind: "kernel-mutation", target: "entity", proof: "interaction" },
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
  { id: "account.sign-in", kind: "kernel-mutation", target: "none", proof: "interaction" },
  { id: "account.sign-out", kind: "kernel-mutation", target: "none", proof: "interaction" },
  { id: "ask.answer", kind: "interaction", target: "none", proof: "authority-effect" },
  { id: "memory.save", kind: "kernel-mutation", target: "entity", proof: "interaction" },
  { id: "memory.restore", kind: "kernel-mutation", target: "entity", proof: "interaction" },
  { id: "memory.forget", kind: "destructive", target: "entity", proof: "interaction" },
  { id: "config.repair", kind: "kernel-mutation", target: "none", proof: "interaction" },
  { id: "extensions.reload", kind: "kernel-mutation", target: "none", proof: "interaction" },
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
