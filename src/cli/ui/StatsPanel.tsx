import { basename } from "node:path";
import { Box, type Color, Text, useStdout } from "ink";
import React from "react";
import stringWidth from "string-width";
import type { EditMode } from "../../config.js";
import { t } from "../../i18n/index.js";
import type { SessionSummary } from "../../telemetry/stats.js";
import { Bar, ChromeRule } from "./primitives.js";
import { COLOR, GRADIENT } from "./theme.js";
import { FG, formatBalance, formatCost } from "./theme/tokens.js";

const COLD_START_TURNS = 3;

export interface StatsPanelProps {
  summary: SessionSummary;
  planMode?: boolean;
  editMode?: EditMode;
  balance?: { currency: string; total: number } | null;
  updateAvailable?: string | null;
  budgetUsd?: number | null;
  rootDir?: string;
  sessionName?: string | null;
}

export function StatsPanel({
  summary,
  planMode,
  editMode,
  balance,
  updateAvailable,
  budgetUsd,
  rootDir,
  sessionName,
}: StatsPanelProps) {
  const coldStart = summary.turns <= COLD_START_TURNS;
  return (
    <Box flexDirection="column" paddingX={1}>
      <ChromeRow
        editMode={editMode}
        planMode={planMode}
        summary={summary}
        coldStart={coldStart}
        rootDir={rootDir}
        sessionName={sessionName ?? null}
        updateAvailable={updateAvailable}
        balance={balance ?? null}
      />
      <ChromeRule />
      {budgetUsd !== null && budgetUsd !== undefined ? (
        <BudgetRow spent={summary.totalCostUsd} cap={budgetUsd} />
      ) : null}
    </Box>
  );
}

function ChromeRow({
  editMode,
  planMode,
  summary,
  coldStart,
  rootDir,
  sessionName,
  updateAvailable,
  balance,
}: {
  editMode?: EditMode;
  planMode?: boolean;
  summary: SessionSummary;
  coldStart: boolean;
  rootDir?: string;
  sessionName?: string | null;
  updateAvailable?: string | null;
  balance?: { currency: string; total: number } | null;
}) {
  const modePill = pickModePill(planMode, editMode);
  const projectName = rootDir ? basename(rootDir) : null;
  const cachePct = (summary.cacheHitRatio * 100).toFixed(1);
  const cacheColor =
    summary.cacheHitRatio >= 0.7 ? COLOR.ok : summary.cacheHitRatio >= 0.4 ? COLOR.warn : COLOR.err;
  const balanceLabel = balance
    ? `[${formatBalance(balance.total, balance.currency, { label: true })}]`
    : "";
  const costLabel = `[${formatCost(summary.totalCostUsd, balance?.currency)}]`;
  const cacheLabel = "[c ▰▰▰▰▰▰ 100%]";
  const updateLabel = updateAvailable ? `↑ ${updateAvailable}` : "";

  // Greedy width-aware fit. Layout (every gap = 2 cells):
  //   [brand][·project][›session]<spacer>[update][mode][cost][balance][cache]
  // Always shown: brand, project (if rootDir), mode (if set), cost.
  // Optional, dropped greedy by priority: balance > cache > session > update.
  const { stdout } = useStdout();
  const cols = (stdout?.columns ?? 80) - 2; // subtract paddingX={1} on both sides
  const SEP_DOT = stringWidth("  ·  ");
  const SEP_ARROW = stringWidth("  ›  ");
  const GAP = 2;

  const fixedLeft =
    stringWidth("◈ reasonix") + (projectName ? SEP_DOT + stringWidth(projectName) : 0);
  const modeW = modePill ? GAP + stringWidth(`[${modePill.label}]`) : 0;
  const fixedRight = modeW + stringWidth(costLabel);
  let budget = cols - fixedLeft - fixedRight;

  const balW = balance ? GAP + stringWidth(balanceLabel) : 0;
  const cacheW = GAP + stringWidth(cacheLabel);
  const sessionW = sessionName ? SEP_ARROW + stringWidth(sessionName) : 0;
  const updateW = updateLabel ? GAP + stringWidth(updateLabel) : 0;

  const showBalance = balW > 0 && budget >= balW;
  if (showBalance) budget -= balW;
  const showCache = budget >= cacheW;
  if (showCache) budget -= cacheW;
  const showSession = sessionW > 0 && budget >= sessionW;
  if (showSession) budget -= sessionW;
  const showUpdate = updateW > 0 && budget >= updateW;
  if (showUpdate) budget -= updateW;

  return (
    <Box>
      <Text bold color={GRADIENT[0]}>
        {"◈ "}
      </Text>
      <Text color={COLOR.brand} bold>
        reasonix
      </Text>
      {projectName ? (
        <>
          <Text color={FG.faint}>{"  ·  "}</Text>
          <Text>{projectName}</Text>
          {showSession && sessionName ? (
            <>
              <Text color={FG.faint}>{"  ›  "}</Text>
              <Text color={COLOR.info}>{sessionName}</Text>
            </>
          ) : null}
        </>
      ) : null}

      <Box flexGrow={1} />

      {showUpdate ? (
        <>
          <Text color={COLOR.warn} bold>
            {updateLabel}
          </Text>
          <Text>{"  "}</Text>
        </>
      ) : null}
      {modePill ? (
        <>
          <Text color={modePill.color} bold>
            {`[${modePill.label}]`}
          </Text>
          <Text>{"  "}</Text>
        </>
      ) : null}
      <Text
        color={summary.turns === 0 || coldStart ? FG.faint : sessionCostColor(summary.totalCostUsd)}
        bold={summary.turns > 0 && !coldStart}
      >
        {costLabel}
      </Text>
      {showBalance && balance ? (
        <>
          <Text>{"  "}</Text>
          <Text color={balance.total < 1 ? COLOR.err : balance.total < 5 ? COLOR.warn : COLOR.ok}>
            {balanceLabel}
          </Text>
        </>
      ) : null}
      {showCache ? (
        <>
          <Text>{"  "}</Text>
          <Text color={FG.faint}>{"["}</Text>
          <Text color={FG.faint}>{"c "}</Text>
          <Bar ratio={summary.cacheHitRatio} color={coldStart ? FG.faint : cacheColor} cells={6} />
          <Text> </Text>
          <Text color={coldStart ? FG.faint : cacheColor}>
            {coldStart && summary.turns === 0 ? "—" : `${cachePct}%`}
          </Text>
          <Text color={FG.faint}>{"]"}</Text>
        </>
      ) : null}
    </Box>
  );
}

function pickModePill(
  planMode: boolean | undefined,
  editMode: EditMode | undefined,
): { label: string; color: Color } | null {
  if (planMode) return { label: t("statsPanel.modePlan"), color: COLOR.err };
  if (editMode === "yolo") return { label: t("statsPanel.modeYolo"), color: COLOR.err };
  if (editMode === "auto") return { label: t("statsPanel.modeAuto"), color: COLOR.primary };
  if (editMode === "review") return { label: t("statsPanel.modeReview"), color: COLOR.info };
  return null;
}

function BudgetRow({ spent, cap }: { spent: number; cap: number }) {
  const pct = Math.max(0, (spent / cap) * 100);
  const color = pct >= 100 ? "#f87171" : pct >= 80 ? "#fbbf24" : "#94a3b8";
  return (
    <Box>
      <Text color={FG.faint}>{t("statsPanel.budget")}</Text>
      <Text color={color}>
        {`$${spent.toFixed(4)} / $${cap.toFixed(2)}`}
        <Text color={FG.faint}>{`  (${pct.toFixed(0)}%)`}</Text>
      </Text>
    </Box>
  );
}

function sessionCostColor(cost: number): Color | undefined {
  if (cost <= 0) return undefined;
  if (cost >= 5) return COLOR.err;
  if (cost >= 0.5) return COLOR.warn;
  return COLOR.ok;
}
