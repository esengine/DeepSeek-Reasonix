import { release } from "node:os";
import { loadTheme, resolveThemePreference } from "@/config.js";
import { getLanguage, t } from "@/i18n/index.js";
import {
  DEEPSEEK_CONTEXT_TOKENS,
  DEEPSEEK_PRICING,
  DEFAULT_CONTEXT_TOKENS,
} from "@/telemetry/stats.js";
import { countTokensBounded } from "@/tokenizer.js";
import { VERSION } from "@/version.js";
import { writeClipboard } from "../../clipboard.js";
import { computeCtxBreakdown } from "../../ctx-breakdown.js";
import { buildFeedbackDiagnostic, buildFeedbackIssueUrl } from "../../feedback.js";
import { openUrl } from "../../open-url.js";
import type { SlashHandler } from "../dispatch.js";
import { compactNum } from "../helpers.js";

const context: SlashHandler = (_args, loop) => {
  const breakdown = computeCtxBreakdown(loop);
  const total =
    breakdown.systemTokens + breakdown.toolsTokens + breakdown.logTokens + breakdown.inputTokens;
  const winPct = breakdown.ctxMax > 0 ? Math.round((total / breakdown.ctxMax) * 100) : 0;
  const fallbackInfo = t("handlers.observability.contextInfo", {
    total: compactNum(total),
    max: compactNum(breakdown.ctxMax),
    pct: winPct,
    sys: compactNum(breakdown.systemTokens),
    tools: compactNum(breakdown.toolsTokens),
    log: compactNum(breakdown.logTokens),
  });
  return { info: fallbackInfo, ctxBreakdown: breakdown };
};

const status: SlashHandler = (_args, loop, ctx) => {
  const ctxMax = DEEPSEEK_CONTEXT_TOKENS[loop.model] ?? DEFAULT_CONTEXT_TOKENS;
  const summary = loop.stats.summary();
  const lastPromptTokens = summary.lastPromptTokens;
  const ctxPct = ctxMax > 0 ? Math.round((lastPromptTokens / ctxMax) * 100) : 0;
  const ctxBar = lastPromptTokens > 0 ? renderTinyBar(ctxPct, 16) : "";
  const ctxLine =
    lastPromptTokens > 0
      ? t("handlers.observability.statusCtx", {
          bar: ctxBar,
          used: compactNum(lastPromptTokens),
          max: compactNum(ctxMax),
          pct: ctxPct,
        })
      : t("handlers.observability.statusCtxNone");

  const cost = summary.totalCostUsd;
  const cacheLine =
    summary.turns > 3
      ? (() => {
          const cachePct = summary.cacheHitRatio * 100;
          return t("handlers.observability.statusCost", {
            cost: cost.toFixed(4),
            bar: renderTinyBar(cachePct, 12),
            pct: cachePct.toFixed(1),
            turns: summary.turns,
          });
        })()
      : t("handlers.observability.statusCostCold", {
          cost: cost.toFixed(4),
          turns: summary.turns,
        });

  const budgetLine =
    typeof loop.budgetUsd === "number"
      ? (() => {
          const pct = Math.round((cost / loop.budgetUsd!) * 100);
          const tag = pct >= 100 ? " ▲ EXHAUSTED" : pct >= 80 ? " ▲ 80%+" : "";
          return t("handlers.observability.statusBudget", {
            spent: cost.toFixed(4),
            cap: loop.budgetUsd!.toFixed(2),
            pct,
            tag,
          });
        })()
      : "";

  const pending = ctx.pendingEditCount ?? 0;
  const sessionLine = loop.sessionName
    ? t("handlers.observability.statusSession", {
        name: loop.sessionName,
        count: loop.log.length,
        resumed: loop.resumedMessageCount,
      })
    : t("handlers.observability.statusSessionEphemeral");
  const mcpCount = ctx.mcpSpecs?.length ?? 0;
  const toolCount = loop.prefix.toolSpecs.length;
  const mcpLine = t("handlers.observability.statusMcp", { servers: mcpCount, tools: toolCount });
  const pendingLine =
    pending > 0 ? t("handlers.observability.statusEdits", { count: pending }) : "";
  const planLine = ctx.planMode ? t("handlers.observability.statusPlan") : "";
  const modeLine =
    ctx.editMode === "yolo"
      ? t("handlers.observability.statusModeYolo")
      : ctx.editMode === "auto"
        ? t("handlers.observability.statusModeAuto")
        : ctx.editMode === "review"
          ? t("handlers.observability.statusModeReview")
          : "";
  const dashUrl = ctx.getDashboardUrl?.();
  const dashLine = dashUrl ? t("handlers.observability.statusDash", { url: dashUrl }) : "";
  const workspaceLine = ctx.codeRoot
    ? t("handlers.observability.statusWorkspace", { path: ctx.codeRoot })
    : "";
  const lines = [
    t("handlers.observability.statusModel", { model: loop.model }),
    t("handlers.observability.statusFlags", {
      stream: loop.stream ? "on" : "off",
      effort: loop.reasoningEffort,
    }),
    cacheLine,
    ctxLine,
    mcpLine,
    sessionLine,
  ];
  if (workspaceLine) lines.push(workspaceLine);
  if (budgetLine) lines.push(budgetLine);
  if (pendingLine) lines.push(pendingLine);
  if (planLine) lines.push(planLine);
  if (modeLine) lines.push(modeLine);
  if (dashLine) lines.push(dashLine);
  return { info: lines.join("\n") };
};

function renderTinyBar(pct: number, width: number): string {
  const w = Math.max(4, width);
  const clamped = Math.max(0, Math.min(100, pct));
  const filled = Math.round((w * clamped) / 100);
  return `[${"█".repeat(filled)}${"░".repeat(w - filled)}]`;
}

const compact: SlashHandler = (_args, loop, ctx) => {
  void loop
    .compactHistory()
    .then((r) => {
      if (!r.folded) {
        ctx.postInfo?.(t("handlers.observability.compactNoop"));
        return;
      }
      ctx.postInfo?.(
        t("handlers.observability.compactDone", {
          before: r.beforeMessages,
          after: r.afterMessages,
          chars: r.summaryChars.toLocaleString(),
        }),
      );
    })
    .catch((err: Error) => {
      ctx.postInfo?.(t("handlers.observability.compactFailed", { reason: err.message }));
    });
  return { info: t("handlers.observability.compactStarting") };
};

const cost: SlashHandler = (args, loop, ctx) => {
  if (args.length > 0) {
    return estimateCost(args.join(" "), loop);
  }
  const turn = loop.stats.turns[loop.stats.turns.length - 1];
  if (!turn) {
    return { info: t("handlers.observability.costNoTurn") };
  }
  if (!ctx.postUsage) {
    return { info: t("handlers.observability.costNeedsTui") };
  }
  const summary = loop.stats.summary();
  const ctxMax = DEEPSEEK_CONTEXT_TOKENS[loop.model] ?? DEFAULT_CONTEXT_TOKENS;
  ctx.postUsage({
    turn: turn.turn,
    promptTokens: turn.usage.promptTokens,
    reasonTokens: 0,
    outputTokens: turn.usage.completionTokens,
    promptCap: ctxMax,
    cacheHit: turn.cacheHitRatio,
    cost: turn.cost,
    sessionCost: summary.totalCostUsd,
  });
  return {};
};

function estimateCost(userText: string, loop: import("@/loop.js").CacheFirstLoop) {
  const pricing = DEEPSEEK_PRICING[loop.model];
  if (!pricing) {
    return { info: t("handlers.observability.costNoPricing", { model: loop.model }) };
  }
  const userTokens = countTokensBounded(userText);
  const breakdown = computeCtxBreakdown(loop);
  const promptTokens =
    breakdown.systemTokens + breakdown.toolsTokens + breakdown.logTokens + userTokens;

  const turns = loop.stats.turns;
  const avgOutput =
    turns.length > 0
      ? Math.round(turns.reduce((s, tk) => s + tk.usage.completionTokens, 0) / turns.length)
      : 800;
  const cacheHit = loop.stats.summary().cacheHitRatio;

  const inputUsdMiss = (promptTokens * pricing.inputCacheMiss) / 1_000_000;
  const inputUsdLikely =
    (promptTokens * ((1 - cacheHit) * pricing.inputCacheMiss + cacheHit * pricing.inputCacheHit)) /
    1_000_000;
  const outputUsd = (avgOutput * pricing.output) / 1_000_000;

  const fmt = (n: number) => `$${n < 0.01 ? n.toFixed(5) : n.toFixed(4)}`;
  const lines = [
    t("handlers.observability.costEstimate", {
      model: loop.model,
      prompt: promptTokens.toLocaleString(),
      sys: compactNum(breakdown.systemTokens),
      tools: compactNum(breakdown.toolsTokens),
      log: compactNum(breakdown.logTokens),
      msg: compactNum(userTokens),
    }),
    t("handlers.observability.costWorstCase", {
      input: fmt(inputUsdMiss),
      output: fmt(outputUsd),
      avg: avgOutput.toLocaleString(),
      total: fmt(inputUsdMiss + outputUsd),
    }),
    turns.length > 0
      ? t("handlers.observability.costLikely", {
          pct: Math.round(cacheHit * 100),
          input: fmt(inputUsdLikely),
          output: fmt(outputUsd),
          total: fmt(inputUsdLikely + outputUsd),
        })
      : t("handlers.observability.costLikelyCold"),
  ];
  return { info: lines.join("\n") };
}

const feedback: SlashHandler = (_args, loop, ctx) => {
  const themeName = resolveThemePreference(loadTheme(), process.env.REASONIX_THEME);
  const diagnostic = buildFeedbackDiagnostic({
    version: VERSION,
    latestVersion: ctx.latestVersion ?? undefined,
    platform: process.platform,
    osRelease: release(),
    termProgram: process.env.TERM_PROGRAM,
    term: process.env.TERM,
    colorTerm: process.env.COLORTERM,
    inWindowsTerminal: !!process.env.WT_SESSION,
    inTmux: !!process.env.TMUX,
    inSsh: !!process.env.SSH_TTY,
    wslDistro: process.env.WSL_DISTRO_NAME,
    cols: process.stdout.columns,
    rows: process.stdout.rows,
    nodeVersion: process.version,
    locale: getLanguage(),
    theme: themeName,
    model: loop.model,
    reasoningEffort: loop.reasoningEffort,
    editMode: ctx.editMode,
    planMode: ctx.planMode,
    mcpServerCount: ctx.mcpServers?.length ?? ctx.mcpSpecs?.length,
    sessionId: ctx.sessionId,
  });
  // Clipboard is the belt-and-suspenders: GitHub's new-issue page accepts
  // `?body=…` and we use that, but if the URL ever fails to open the
  // user can paste from clipboard against any tracker.
  writeClipboard(diagnostic);
  const url = buildFeedbackIssueUrl(diagnostic);
  const opened = openUrl(url);
  const lines = [
    opened.opened
      ? "▸ issue page opened with the diagnostic block pre-filled. Just describe what you were doing and submit."
      : `▸ couldn't open the browser (${opened.reason ?? "unknown"}). Diagnostic info is on your clipboard; open this URL manually: ${url}`,
    "",
    diagnostic,
  ];
  return { info: lines.join("\n") };
};

export const handlers: Record<string, SlashHandler> = {
  context,
  status,
  compact,
  cost,
  feedback,
};
