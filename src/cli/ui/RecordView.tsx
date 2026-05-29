/** Shared renderer for a single TranscriptRecord — used by ReplayApp and DiffApp. */

import { Box, Text } from "ink";
import React from "react";
import { t } from "../../i18n/index.js";
import type { TranscriptRecord } from "../../transcript/log.js";
import { FG } from "./theme/tokens.js";

export interface RecordViewProps {
  rec: TranscriptRecord;
  /**
   * When rendering side-by-side in diff mode, shorter truncation limits
   * keep long tool results from dominating the pane. Passes through
   * untouched when undefined.
   */
  compact?: boolean;
}

export function RecordView({ rec, compact = false }: RecordViewProps) {
  const toolArgsMax = compact ? 120 : 200;
  const toolContentMax = compact ? 200 : 400;

  if (rec.role === "user") {
    // Continuation indent of 6 spaces matches the `you › ` prefix width
    // so wrapped multi-line user messages align under the body text
    // instead of jumping to column 0.
    const content = rec.content.includes("\n")
      ? rec.content.split("\n").join("\n      ")
      : rec.content;
    return (
      <Box marginTop={1}>
        <Text bold color="ansi:cyan">
          {t("recordView.userPrefix")}
        </Text>
        <Text>{content}</Text>
      </Box>
    );
  }
  if (rec.role === "assistant_final") {
    return (
      <Box flexDirection="column" marginTop={1}>
        <Box>
          <Text bold color="ansi:green">
            {t("recordView.assistant")}
          </Text>
          {rec.cost !== undefined ? (
            <Text color={FG.faint}>
              {"  $"}
              {rec.cost.toFixed(6)}
            </Text>
          ) : null}
          {rec.usage ? <CacheBadge usage={rec.usage} /> : null}
        </Box>
        {rec.content ? (
          <Text>{rec.content}</Text>
        ) : (
          <Text color={FG.faint} italic>
            {t("recordView.toolCallOnly")}
          </Text>
        )}
      </Box>
    );
  }
  if (rec.role === "tool") {
    return (
      <Box flexDirection="column" marginTop={1}>
        <Text color="ansi:yellow">
          {t("recordView.toolPrefix")}
          {rec.tool ?? "?"}
          {">"}
        </Text>
        {rec.args ? (
          <Text color={FG.faint}>
            {t("recordView.argsLabel")}
            {truncate(rec.args, toolArgsMax)}
          </Text>
        ) : null}
        <Text color={FG.faint}>
          {t("recordView.resultArrow")}
          {truncate(rec.content, toolContentMax)}
        </Text>
      </Box>
    );
  }
  if (rec.role === "error") {
    return (
      <Box marginTop={1}>
        <Text color="ansi:red" bold>
          {t("recordView.error")}
        </Text>
        <Text color="ansi:red">{rec.error ?? rec.content}</Text>
      </Box>
    );
  }
  if (rec.role === "done" || rec.role === "assistant_delta") {
    // Noise in replay; skip.
    return null;
  }
  return (
    <Box>
      <Text color={FG.faint}>
        [{rec.role}] {rec.content}
      </Text>
    </Box>
  );
}

function CacheBadge({ usage }: { usage: NonNullable<TranscriptRecord["usage"]> }) {
  const hit = usage.prompt_cache_hit_tokens ?? 0;
  const miss = usage.prompt_cache_miss_tokens ?? 0;
  const total = hit + miss;
  if (total === 0) return null;
  const pct = (hit / total) * 100;
  const color = pct >= 70 ? "ansi:green" : pct >= 40 ? "ansi:yellow" : "ansi:red";
  return (
    <Text>
      <Text color={FG.faint}>{t("recordView.cache")}</Text>
      <Text color={color}>{pct.toFixed(1)}%</Text>
    </Text>
  );
}

function truncate(s: string, max: number): string {
  return s.length <= max
    ? s
    : `${s.slice(0, max)}${t("recordView.truncateExtra", { extra: s.length - max })}`;
}
