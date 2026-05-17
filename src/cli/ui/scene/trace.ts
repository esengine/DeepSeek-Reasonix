import { appendFileSync, closeSync, openSync } from "node:fs";
import {
  DEFAULT_COMMAND,
  type RendererProcess,
  type RustEvent,
  spawnRenderer,
} from "./renderer-process.js";

const FILE_VAR = "REASONIX_SCENE_TRACE";
const RENDERER_VAR = "REASONIX_RENDERER";
const COMMAND_OVERRIDE_VAR = "REASONIX_RENDER_CMD";
const INTEGRATED_VAR = "REASONIX_RENDERER_INTEGRATED";

let integratedHandler: ((event: RustEvent) => void) | null = null;

export function setIntegratedEventHandler(handler: (event: RustEvent) => void): void {
  integratedHandler = handler;
}

export function isIntegratedRendererRequested(): boolean {
  return process.env[RENDERER_VAR] === "rust" && process.env[INTEGRATED_VAR] === "1";
}

type Mode = "off" | "file" | "child";

type TraceState = {
  mode: Mode;
  opened: boolean;
  path: string | null;
  child: RendererProcess | null;
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
      if (state.path) {
        appendFileSync(state.path, `${JSON.stringify(message)}\n`);
      }
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
    state.child.close();
  }
  state.mode = "off";
  state.opened = false;
  state.path = null;
  state.child = null;
}

export async function flushSceneTrace(): Promise<void> {
  if (state.child) {
    await state.child.close();
  }
}

function ensureInitialized(): void {
  if (state.opened) return;
  state.opened = true;
  if (process.env[RENDERER_VAR] === "rust") {
    state.mode = "child";
    const integrated = process.env[INTEGRATED_VAR] === "1";
    state.child = spawnRenderer({
      command: rendererCommand(),
      integrated,
      onEvent: integrated && integratedHandler ? integratedHandler : undefined,
    });
    return;
  }
  const raw = process.env[FILE_VAR];
  if (!raw || raw.length === 0) return;
  state.mode = "file";
  state.path = raw;
  truncate(raw);
}

function rendererCommand(): readonly string[] {
  const override = process.env[COMMAND_OVERRIDE_VAR];
  if (override && override.length > 0) {
    try {
      const parsed = JSON.parse(override);
      if (Array.isArray(parsed) && parsed.every((p) => typeof p === "string")) {
        return parsed;
      }
    } catch {
      // fall through to default
    }
  }
  return [...DEFAULT_COMMAND];
}

function truncate(path: string): void {
  const fd = openSync(path, "w");
  closeSync(fd);
}
