import { appendFileSync, closeSync, openSync } from "node:fs";
import { type RendererProcess, type RustEvent, spawnRenderer } from "./renderer-process.js";
import { resolveRenderer } from "./renderer-resolver.js";

const FILE_VAR = "REASONIX_SCENE_TRACE";
const RENDERER_VAR = "REASONIX_RENDERER";
const INTEGRATED_VAR = "REASONIX_RENDERER_INTEGRATED";

let integratedHandler: ((event: RustEvent) => void) | null = null;

export function setIntegratedEventHandler(handler: (event: RustEvent) => void): void {
  integratedHandler = handler;
}

/** Default-on when the rust renderer is active; set REASONIX_RENDERER_INTEGRATED=0 to opt back to the --emit-input split. */
export function isIntegratedRendererRequested(): boolean {
  return process.env[RENDERER_VAR] !== "node" && process.env[INTEGRATED_VAR] !== "0";
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

/** Force the trace child to spawn now — React useEffect in useSceneTrace was unreliable on macOS (sometimes never fired under npx). */
export function ensureSceneTraceReady(): void {
  ensureInitialized();
}

function ensureInitialized(): void {
  if (state.opened) return;
  state.opened = true;
  // Explicit file-trace opt-in wins over the renderer choice — it's the
  // dev/replay surface and the user has clearly asked for it.
  const raw = process.env[FILE_VAR];
  if (raw && raw.length > 0) {
    state.mode = "file";
    state.path = raw;
    truncate(raw);
    return;
  }
  if (process.env[RENDERER_VAR] === "node") return;
  const { command, source } = resolveRenderer();
  process.stderr.write(`[trace] resolver source=${source} command=${JSON.stringify(command)}\n`);
  if (source === null || command.length === 0) {
    process.stderr.write(
      "▲ trace.ts: resolveRenderer() returned no usable command — scene trace stays off. " +
        "Check optional-dep install (`ls node_modules/@reasonix/render-*`) or set REASONIX_RENDER_BIN.\n",
    );
    return;
  }
  const integrated = process.env[INTEGRATED_VAR] !== "0";
  process.stderr.write(`[trace] spawning rust child (integrated=${integrated})\n`);
  state.mode = "child";
  state.child = spawnRenderer({
    command,
    integrated,
    onEvent: integrated && integratedHandler ? integratedHandler : undefined,
  });
}

function truncate(path: string): void {
  const fd = openSync(path, "w");
  closeSync(fd);
}
