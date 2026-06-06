import { Box, type Color, Text, useStdout } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React, { useEffect } from "react";
import { useAgentState, useDispatch } from "../state/provider.js";
import type { Toast } from "../state/state.js";
import { FG, TONE } from "../theme/tokens.js";
import { useSlowTick } from "../ticker.js";

const TONE_COLOR = {
  ok: TONE.ok,
  info: TONE.brand,
  warn: TONE.warn,
  err: TONE.err,
} as const;

const TONE_GLYPH = {
  ok: "✓",
  info: "ⓘ",
  warn: "⚠",
  err: "✗",
} as const;

function bodyColor(toast: Toast, now: number): Color {
  const elapsed = now - toast.bornAt;
  const remaining = toast.ttlMs - elapsed;
  return remaining < toast.ttlMs / 3 ? FG.meta : FG.body;
}

export function ToastRail(): React.ReactElement | null {
  const toasts = useAgentState((s) => s.toasts);
  const dispatch = useDispatch();
  useSlowTick();
  const { stdout } = useStdout();
  const cols = stdout?.columns ?? 80;
  const rule = "━".repeat(Math.max(20, cols - 4));
  const now = Date.now();

  /** One-shot per-toast cleanup; effect re-runs only when the toast set changes (not every render). */
  useEffect(() => {
    const timers: ReturnType<typeof setTimeout>[] = [];
    for (const t of toasts) {
      const remaining = Math.max(0, t.ttlMs - (Date.now() - t.bornAt));
      timers.push(setTimeout(() => dispatch({ type: "toast.hide", id: t.id }), remaining));
    }
    return () => {
      for (const id of timers) clearTimeout(id);
    };
  }, [toasts, dispatch]);

  const visible = toasts.filter((t) => now - t.bornAt < t.ttlMs);
  if (visible.length === 0) return null;

  return (
    <Box flexDirection="column">
      {visible.map((t) => {
        const color = TONE_COLOR[t.tone];
        const glyph = TONE_GLYPH[t.tone];
        const body = bodyColor(t, now);
        const remainingSec = Math.max(0, Math.ceil((t.ttlMs - (now - t.bornAt)) / 1000));
        return (
          <Box key={t.id} flexDirection="column" paddingX={1}>
            <Text color={color}>{rule}</Text>
            <Box flexDirection="row">
              <Text color={color}>{glyph}</Text>
              <Text bold color={body}>{` ${t.title}`}</Text>
              {t.detail !== undefined && <Text color={FG.sub}>{`  ·  ${t.detail}`}</Text>}
              <Box flexGrow={1} />
              <Text color={FG.faint}>{`${remainingSec}s`}</Text>
            </Box>
          </Box>
        );
      })}
    </Box>
  );
}
