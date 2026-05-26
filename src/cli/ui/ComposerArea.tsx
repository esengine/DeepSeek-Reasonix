/**
 * ComposerArea — the bottom dock region: input, suggestions, status.
 * Extracted from App.tsx per #565 Phase 2.
 */

import { Box, Text } from "ink";
import React from "react";

import type { EditMode } from "../../config.js";
import type { JobRegistry } from "../../tools/jobs.js";
import { useRenderTrace } from "./render-trace.js";

import { AtMentionSuggestions } from "./AtMentionSuggestions.js";
import { PromptInput } from "./PromptInput.js";
import { ShortcutsHelpModal } from "./ShortcutsHelpModal.js";
import type { SlashArgPickerProps } from "./SlashArgPicker.js";
import { SlashArgPicker } from "./SlashArgPicker.js";
import type { SlashSuggestionsProps } from "./SlashSuggestions.js";
import { SlashSuggestions } from "./SlashSuggestions.js";

import { StatusRow } from "./layout/StatusRow.js";
import { formatLoopStatus } from "./loop.js";
import { useSlowTick } from "./ticker.js";

import type { StatusBarConfig } from "./layout/StatusRow.js";

// ── Props ─────────────────────────────────────────────────────────

export interface ComposerAreaProps {
  // ── status / mode — types flow from ModeStatusBar + StatusRow ────
  editMode: EditMode;
  pendingCount: number;
  modeFlash: boolean;
  planMode: boolean;
  undoArmed: boolean;
  jobs?: JobRegistry;
  activeLoop?: Parameters<typeof LoopStatusRow>[0]["loop"] | null;
  statusBar: StatusBarConfig;
  /** Current mode for input box bottom display. */
  mode?: string;
  /** Current model for input box bottom display. */
  model?: string;
  /** Show the shortcuts help modal above the input box. */
  showShortcuts?: boolean;

  // ── prompt ───────────────────────────────────────────────────────
  input: string;
  setInput: (next: string) => void;
  busy: boolean;
  steerBusy?: boolean;
  onSubmit: (raw: string) => Promise<void>;
  onHistoryPrev: () => void;
  onHistoryNext: () => void;
  onOpenExternalEditor: () => void;
  onCursorChange: (cursor: number) => void;
  isHistoryMode?: boolean;

  // ── slash / @-mention / arg picker — derived from sub-component props
  slashMatches: SlashSuggestionsProps["matches"] | null;
  slashSelected: SlashSuggestionsProps["selectedIndex"];
  slashGroupMode: SlashSuggestionsProps["groupMode"];
  slashAdvancedHidden: SlashSuggestionsProps["advancedHidden"];

  atState: React.ComponentProps<typeof AtMentionSuggestions>["state"] | null;
  atSelected: React.ComponentProps<typeof AtMentionSuggestions>["selectedIndex"];

  slashArgContext: {
    spec: SlashArgPickerProps["spec"];
    kind: SlashArgPickerProps["kind"];
    partial: string;
  } | null;
  slashArgMatches: Parameters<typeof SlashArgPicker>[0]["matches"];
  slashArgSelected: number;
}

// ── Component ─────────────────────────────────────────────────────

export const ComposerArea: React.FC<ComposerAreaProps> = React.memo(
  ({
    editMode,
    pendingCount,
    modeFlash,
    planMode,
    undoArmed,
    jobs,
    activeLoop,
    statusBar,
    mode,
    model,
    showShortcuts,
    input,
    setInput,
    busy,
    steerBusy,
    onSubmit,
    onHistoryPrev,
    onHistoryNext,
    onOpenExternalEditor,
    onCursorChange,
    isHistoryMode,
    slashMatches,
    slashSelected,
    slashGroupMode,
    slashAdvancedHidden,
    atState,
    atSelected,
    slashArgContext,
    slashArgMatches,
    slashArgSelected,
  }) => {
    useRenderTrace("ComposerArea");
    const inputArea = (
      <Box flexDirection="column" flexShrink={0} flexWrap="nowrap">
        <Box flexDirection="column" flexShrink={0} flexWrap="nowrap">
          {slashMatches !== null ? (
            <SlashSuggestions
              key={`slash-suggestions:${slashGroupMode ? "group" : "search"}`}
              matches={slashMatches}
              selectedIndex={slashSelected}
              groupMode={slashGroupMode}
              advancedHidden={slashAdvancedHidden}
            />
          ) : null}
          {atState !== null ? (
            <AtMentionSuggestions state={atState} selectedIndex={atSelected} />
          ) : null}
          {slashArgContext ? (
            <SlashArgPicker
              matches={slashArgMatches}
              selectedIndex={slashArgSelected}
              spec={slashArgContext.spec}
              kind={slashArgContext.kind}
              partial={slashArgContext.partial}
            />
          ) : null}
        </Box>
        {showShortcuts ? <ShortcutsHelpModal /> : null}
        <PromptInput
          value={input}
          onChange={setInput}
          onSubmit={onSubmit}
          disabled={busy}
          steerBusy={steerBusy}
          onHistoryPrev={onHistoryPrev}
          onHistoryNext={onHistoryNext}
          onOpenExternalEditor={onOpenExternalEditor}
          onCursorChange={onCursorChange}
          rowsAfter={2 + (activeLoop ? 1 : 0)}
          mode={mode}
          model={model}
          isHistoryMode={isHistoryMode}
          planMode={planMode}
        />
        {activeLoop ? <LoopStatusRow loop={activeLoop} /> : null}
        <StatusRow statusBar={statusBar} />
      </Box>
    );

    return inputArea;
  },
);
ComposerArea.displayName = "ComposerArea";

// ── Loop status row (moved from App.tsx) ──────────────────────────

function LoopStatusRow({
  loop,
}: {
  loop: { prompt: string; intervalMs: number; nextFireAt: number; iter: number };
}) {
  useSlowTick();
  const nextFireMs = Math.max(0, loop.nextFireAt - Date.now());
  return (
    <Box>
      <Text color="cyan">
        {`loop: ${formatLoopStatus(loop.prompt, nextFireMs, loop.iter)} — /loop stop or type to cancel`}
      </Text>
    </Box>
  );
}
