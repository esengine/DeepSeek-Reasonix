/** Shared editMode → auto-resolve rules so CLI TUI + Tauri desktop don't drift. */

import type { EditMode } from "../config.js";
import type { PauseRequest } from "./pause-gate.js";

/** Mirrors shell.ts's allowAll bypass: only review still pauses on checkpoints. */
export function shouldAutoResolveCheckpoint(editMode: EditMode): boolean {
  return editMode === "auto" || editMode === "yolo";
}

/** null = surface to user; non-null = resolve gate immediately with this verdict. */
export function autoResolveVerdict(req: PauseRequest, editMode: EditMode): unknown | null {
  if (req.kind === "plan_checkpoint" && shouldAutoResolveCheckpoint(editMode)) {
    return { type: "continue" };
  }
  // yolo mirrors shell.ts's allowAll bypass — outside-sandbox reads/writes pass
  // through too. Stays "run_once" rather than "always_allow" so the YOLO session
  // doesn't pollute the on-disk allowlist with every transient path it touched.
  if (req.kind === "path_access" && editMode === "yolo") {
    return { type: "run_once" };
  }
  // Shell commands in YOLO: shell.ts's `allowAll` callback should already have
  // skipped gate.ask for these, but the closure reads on-disk config via
  // `loadEditMode()` while ACP's `--yolo` flag and any future runtime-only
  // YOLO source don't write to config. Without this second layer those paths
  // surface a confirmation prompt even though the user is in YOLO (#1448).
  // `run_once` matches shell.ts's behavior — don't pollute the persistent
  // allowlist with every transient command.
  if ((req.kind === "run_command" || req.kind === "run_background") && editMode === "yolo") {
    return { type: "run_once" };
  }
  return null;
}
