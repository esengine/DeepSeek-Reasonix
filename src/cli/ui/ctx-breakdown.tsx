import { Box, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { t } from "../../i18n/index.js";
import type { CacheFirstLoop } from "../../loop.js";
import { resolveContextTokens } from "../../telemetry/stats.js";
import { countTokensBounded } from "../../tokenizer.js";
import { formatTokens } from "./primitives.js";
import { COLOR } from "./theme.js";

export interface CtxBreakdownData {
  systemTokens: number;
  toolsTokens: number;
  logTokens: number;
  inputTokens: number;
  ctxMax: number;
  toolsCount: number;
  logMessages: number;
  topTools: Array<{ name: string; tokens: number; turn: number }>;
}

/**
 * Walk the loop's prefix + log and tally tokens per category.
 * Uses bounded counting because `/context` can inspect oversized tool
 * results before the next loop-healing pass trims them.
 */
export function computeCtxBreakdown(loop: CacheFirstLoop): CtxBreakdownData {
  const systemTokens = countTokensBounded(loop.prefix.system);
  const toolsTokens = countTokensBounded(JSON.stringify(loop.prefix.toolSpecs));
  const entries = loop.log.toFullHistory();
  let userTokens = 0;
  let assistantTokens = 0;
  let toolResultTokens = 0;
  let toolCallTokens = 0;
  const toolBreakdown: Array<{ name: string; tokens: number; turn: number }> = [];
  let logTurn = 0;
  for (const e of entries) {
    const content = typeof e.content === "string" ? e.content : "";
    if (e.role === "user") {
      userTokens += countTokensBounded(content);
      logTurn += 1;
    } else if (e.role === "assistant") {
      assistantTokens += countTokensBounded(content);
      if (Array.isArray(e.tool_calls) && e.tool_calls.length > 0) {
        toolCallTokens += countTokensBounded(JSON.stringify(e.tool_calls));
      }
    } else if (e.role === "tool") {
      const n = countTokensBounded(content);
      toolResultTokens += n;
      toolBreakdown.push({ name: e.name ?? "?", tokens: n, turn: logTurn });
    }
  }
  const logTokens = userTokens + assistantTokens + toolResultTokens + toolCallTokens;
  const ctxMax = resolveContextTokens(loop.model);
  const topTools = [...toolBreakdown].sort((a, b) => b.tokens - a.tokens).slice(0, 5);
  return {
    systemTokens,
    toolsTokens,
    logTokens,
    inputTokens: 0,
    ctxMax,
    toolsCount: loop.prefix.toolSpecs.length,
    logMessages: entries.length,
    topTools,
  };
}

/**
 * 4-segment stacked bar with legend + top-tools list. Pushed to
 * scrollback by the `/context` slash; the always-on bottom footer
 * uses its own slim 1-row layout in `CtxFooter`.
 */
export function CtxBreakdownBlock({ data }: { data: CtxBreakdownData }): React.ReactElement {
  const total = data.systemTokens + data.toolsTokens + data.logTokens + data.inputTokens;
  const winPct = data.ctxMax > 0 ? Math.round((total / data.ctxMax) * 100) : 0;
  const barWidth = 48;
  const cellOf = (n: number) => (data.ctxMax > 0 ? Math.round((n / data.ctxMax) * barWidth) : 0);
  const sysCells = cellOf(data.systemTokens);
  const toolsCells = cellOf(data.toolsTokens);
  const logCells = cellOf(data.logTokens);
  const inputCells = cellOf(data.inputTokens);
  const used = sysCells + toolsCells + logCells + inputCells;
  const freeCells = Math.max(0, barWidth - used);
  const sevColor = winPct >= 80 ? COLOR.err : winPct >= 60 ? COLOR.warn : COLOR.ok;

  return (
    <Box
      flexDirection="column"
      marginY={1}
      borderStyle="single"
      borderTop={false}
      borderRight={false}
      borderBottom={false}
      borderColor={COLOR.brand}
      paddingLeft={1}
    >
      <Box>
        <Text color={COLOR.brand} bold>
          {t("ctxBreakdown.title")}
        </Text>
        <Text dim>{`  ${formatTokens(total)} of ${formatTokens(data.ctxMax)}`}</Text>
        <Text dim>{"  ·  "}</Text>
        <Text color={sevColor} bold>
          {`${winPct}%`}
        </Text>
        {winPct >= 80 ? (
          <Text color={COLOR.err} bold>
            {"  ·  /compact"}
          </Text>
        ) : null}
      </Box>
      <Box>
        <Text color={COLOR.brand}>{"█".repeat(sysCells)}</Text>
        <Text color={COLOR.accent}>{"█".repeat(toolsCells)}</Text>
        <Text color={COLOR.primary}>{"█".repeat(logCells)}</Text>
        <Text color={COLOR.tool}>{"█".repeat(inputCells)}</Text>
        <Text color={COLOR.info} dim>
          {"░".repeat(freeCells)}
        </Text>
      </Box>
      <Box>
        <Text color={COLOR.brand}>■</Text>
        <Text dim>{` ${t("cardLabels.system")} ${formatTokens(data.systemTokens)}`}</Text>
        <Text>{"   "}</Text>
        <Text color={COLOR.accent}>■</Text>
        <Text dim>{` ${t("cardLabels.tools")} ${formatTokens(data.toolsTokens)}`}</Text>
        <Text dim>{` (${data.toolsCount})`}</Text>
        <Text>{"   "}</Text>
        <Text color={COLOR.primary}>■</Text>
        <Text dim>{` ${t("cardLabels.log")} ${formatTokens(data.logTokens)}`}</Text>
        <Text dim>{` (${data.logMessages} ${t("ctxBreakdown.msg")})`}</Text>
        <Text>{"   "}</Text>
        <Text color={COLOR.tool}>■</Text>
        <Text dim>{` ${t("cardLabels.input")} ${formatTokens(data.inputTokens)}`}</Text>
      </Box>
      {data.topTools.length > 0 ? (
        <Box marginTop={1} flexDirection="column">
          <Text dim>{t("ctxBreakdown.topTools", { count: data.topTools.length })}</Text>
          {data.topTools.map((tool) => (
            <Box key={`${tool.turn}-${tool.name}`}>
              <Text
                dim
              >{`    ${t("ctxBreakdown.turnLabel")} ${String(tool.turn).padStart(3)}  `}</Text>
              <Text color={COLOR.info}>{tool.name.padEnd(22)}</Text>
              <Text dim>{`  ${formatTokens(tool.tokens).padStart(8)}`}</Text>
            </Box>
          ))}
        </Box>
      ) : null}
      <Box marginTop={1}>
        <Text dim>{t("ctxBreakdown.compactHint")}</Text>
      </Box>
    </Box>
  );
}
