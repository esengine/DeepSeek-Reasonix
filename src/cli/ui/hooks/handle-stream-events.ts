import type { Dispatch, MutableRefObject, SetStateAction } from "react";
import type { LoopEvent } from "../../../loop.js";
import type { TurnTranslator } from "../state/TurnTranslator.js";
import type { Scrollback } from "./useScrollback.js";

function parseJsonOrRaw(input: string | undefined): unknown {
  if (!input) return undefined;
  try {
    return JSON.parse(input);
  } catch {
    return input;
  }
}

export interface ToolStartContext {
  setOngoingTool: Dispatch<SetStateAction<{ name: string; args?: string } | null>>;
  setToolProgress: Dispatch<
    SetStateAction<{ progress: number; total?: number; message?: string } | null>
  >;
  toolStartedAtRef: MutableRefObject<number | null>;
  translator: TurnTranslator;
  codeModeOn: boolean;
  recordRecentFile: (path: string) => void;
}

export function handleToolStart(ev: LoopEvent, ctx: ToolStartContext): void {
  ctx.setOngoingTool({ name: ev.toolName ?? "?", args: ev.toolArgs });
  ctx.setToolProgress(null);
  ctx.toolStartedAtRef.current = Date.now();
  ctx.translator.toolStart(ev.toolName ?? "?", parseJsonOrRaw(ev.toolArgs), ev.callId);
  // Feed the `@` picker's recency LRU from any path-shaped field in the
  // tool args. Picker surfaces these next time `@` is typed, even if mtime
  // is stale.
  if (!ctx.codeModeOn || !ev.toolArgs) return;
  try {
    const parsed = JSON.parse(ev.toolArgs) as {
      path?: unknown;
      file_path?: unknown;
      file?: unknown;
    };
    for (const k of ["path", "file_path", "file"] as const) {
      const v = parsed[k];
      if (typeof v === "string" && v.trim()) {
        ctx.recordRecentFile(v.trim());
        break;
      }
    }
  } catch {
    /* malformed args — skip recency tracking */
  }
}

export interface ErrorContext {
  log: Scrollback;
  setOngoingTool: Dispatch<SetStateAction<{ name: string; args?: string } | null>>;
  setToolProgress: Dispatch<
    SetStateAction<{ progress: number; total?: number; message?: string } | null>
  >;
  toolStartedAtRef: MutableRefObject<number | null>;
  translator: TurnTranslator;
}

export function handleErrorEvent(ev: LoopEvent, ctx: ErrorContext): void {
  ctx.setOngoingTool(null);
  ctx.setToolProgress(null);
  ctx.toolStartedAtRef.current = null;
  ctx.translator.toolAbort(ev.error ?? ev.content);
  ctx.log.pushError("tool error", ev.error ?? ev.content);
}

export interface WarningContext {
  log: Scrollback;
  setTurnOnPro: Dispatch<SetStateAction<boolean>>;
}

export function handleWarningEvent(ev: LoopEvent, ctx: WarningContext): void {
  ctx.log.pushWarning("warning", ev.content);
  // Loop emits warnings starting with "⇧" whenever this turn is (or just
  // became) running on pro — flip the badge so the escalation shows.
  if (ev.content?.startsWith("⇧ ")) ctx.setTurnOnPro(true);
}
