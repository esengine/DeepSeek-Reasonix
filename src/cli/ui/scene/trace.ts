import { appendFileSync, closeSync, openSync } from "node:fs";
import type { SceneFrame } from "./types.js";

const ENV_VAR = "REASONIX_SCENE_TRACE";

type TraceState = { path: string | null; opened: boolean };

const state: TraceState = { path: null, opened: false };

export function isSceneTraceEnabled(): boolean {
  ensureInitialized();
  return state.path !== null;
}

export function emitSceneFrame(frame: SceneFrame): void {
  ensureInitialized();
  if (!state.path) return;
  appendFileSync(state.path, `${JSON.stringify(frame)}\n`);
}

export function resetSceneTrace(): void {
  state.path = null;
  state.opened = false;
}

function ensureInitialized(): void {
  if (state.opened) return;
  state.opened = true;
  const raw = process.env[ENV_VAR];
  if (!raw || raw.length === 0) return;
  state.path = raw;
  truncate(raw);
}

function truncate(path: string): void {
  const fd = openSync(path, "w");
  closeSync(fd);
}
