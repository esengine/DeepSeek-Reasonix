import { appendFileSync, closeSync, openSync } from "node:fs";
import {
  type CreateRendererOptions,
  type Renderer,
  type RustEvent,
  createRenderer,
  isRendererAvailable,
} from "./renderer.js";

const FILE_VAR = "REASONIX_SCENE_TRACE";
const RENDERER_VAR = "REASONIX_RENDERER";

let integratedHandler: ((event: RustEvent) => void) | null = null;

export function setIntegratedEventHandler(handler: (event: RustEvent) => void): void {
  integratedHandler = handler;
}

/** True when the rust renderer is selected and a native binding is loadable.
 * Set REASONIX_RENDERER=node to opt out to the legacy Ink TUI. */
export function isIntegratedRendererRequested(): boolean {
  if (process.env[RENDERER_VAR] === "node") return false;
  return isRendererAvailable();
}

type Mode = "off" | "file" | "child";

type TraceState = {
  mode: Mode;
  opened: boolean;
  path: string | null;
  child: Renderer | null;
};

const state: TraceState = { mode: "off", opened: false, path: null, child: null };

export function isSceneTraceEnabled(): boolean {
  ensureInitialized();
  return state.mode !== "off";
}

export function emitSceneMessage(message: unknown): void {
  ensureInitialized();
  switch (state.mode) {
    case "off":
      return;
    case "file":
      if (state.path) appendFileSync(state.path, `${JSON.stringify(message)}\n`);
      return;
    case "child":
      state.child?.emit(message);
      return;
  }
}

/** @deprecated kept for transition only; prefer emitSceneMessage. */
export const emitSceneFrame = emitSceneMessage;

export function resetSceneTrace(): void {
  if (state.child) {
    void state.child.close();
  }
  state.mode = "off";
  state.opened = false;
  state.path = null;
  state.child = null;
}

export async function flushSceneTrace(): Promise<void> {
  if (state.child) await state.child.close();
}

/** Force the renderer to be created now. Same single-process model as the
 * rest of the app, so no spawn race — just makes the timing explicit at the
 * chat boot site instead of relying on a React effect to fire first. */
export function ensureSceneTraceReady(): void {
  ensureInitialized();
}

function ensureInitialized(): void {
  if (state.opened) return;
  state.opened = true;
  const raw = process.env[FILE_VAR];
  if (raw && raw.length > 0) {
    state.mode = "file";
    state.path = raw;
    truncate(raw);
    return;
  }
  if (process.env[RENDERER_VAR] === "node") return;
  if (!isRendererAvailable()) return;
  const opts: CreateRendererOptions = {};
  if (integratedHandler) opts.onEvent = integratedHandler;
  state.mode = "child";
  state.child = createRenderer(opts);
}

function truncate(path: string): void {
  const fd = openSync(path, "w");
  closeSync(fd);
}
