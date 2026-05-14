import { Box, Text, useStdout } from "ink";
import React, { useContext } from "react";
import { clipToCells } from "../../../frame/width.js";
import { t } from "../../../i18n/index.js";
import { countTokens } from "../../../tokenizer.js";
import { LiveExpandContext } from "../layout/LiveExpandContext.js";
import { useReserveRows } from "../layout/viewport-budget.js";
import { Markdown } from "../markdown.js";
import { Card } from "../primitives/Card.js";
import { CardHeader } from "../primitives/CardHeader.js";
import { PILL_MODEL, Pill, modelBadgeFor } from "../primitives/Pill.js";
import { Spinner } from "../primitives/Spinner.js";
import type { StreamingCard as StreamingCardData } from "../state/cards.js";
import { FG, TONE, TONE_ACTIVE } from "../theme/tokens.js";
import { useSlowTick } from "../ticker.js";
import { useIncrementalWrap } from "./useIncrementalWrap.js";

/** Streaming preview tail length — bounded live region so chunks don't thrash whole-card layout. */
const STREAMING_PREVIEW_LINES = 4;
/** Expanded mode shows up to this many lines so the card can't swallow the whole viewport. */
const EXPANDED_MAX_LINES = 60;

const MIN_ELAPSED_MS_FOR_RATE = 500;
const MIN_TOKENS_FOR_RATE = 4;
const LIVE_TOKEN_CALIBRATION_CHARS = 1000;
const ESTIMATED_CHARS_PER_TOKEN = 4;

export interface LiveTokenCalibration {
  cardId: string;
  chars: number;
  tokens: number;
}

interface TokenRate {
  tokens: number;
  tps: number | null;
}

function formatTokenCount(n: number): string {
  if (n >= 10000) return `${(n / 1000).toFixed(1)}k`;
  if (n >= 1000) return `${(n / 1000).toFixed(2)}k`;
  return String(n);
}

function rateFromTokens(tokens: number, startTs: number, endTs: number): TokenRate {
  const elapsedMs = endTs - startTs;
  if (elapsedMs < MIN_ELAPSED_MS_FOR_RATE || tokens < MIN_TOKENS_FOR_RATE) {
    return { tokens, tps: null };
  }
  return { tokens, tps: Math.round((tokens * 1000) / elapsedMs) };
}

function tokenRate(text: string, startTs: number, endTs: number): TokenRate {
  return rateFromTokens(countTokens(text), startTs, endTs);
}

export function estimateLiveTokenCount(
  text: string,
  cardId: string,
  calibration: LiveTokenCalibration | null,
  countFn: (value: string) => number = countTokens,
): { tokens: number; calibration: LiveTokenCalibration; exact: boolean } {
  const chars = text.length;
  const shouldCalibrate =
    chars > 0 &&
    (!calibration ||
      calibration.cardId !== cardId ||
      chars < calibration.chars ||
      (calibration.chars === 0 && chars > 0) ||
      chars - calibration.chars >= LIVE_TOKEN_CALIBRATION_CHARS);

  if (shouldCalibrate) {
    const tokens = countFn(text);
    return { tokens, calibration: { cardId, chars, tokens }, exact: true };
  }

  const base = calibration?.cardId === cardId && chars >= calibration.chars ? calibration : null;
  const baseChars = base?.chars ?? 0;
  const baseTokens = base?.tokens ?? 0;
  const estimatedDelta = Math.ceil(Math.max(0, chars - baseChars) / ESTIMATED_CHARS_PER_TOKEN);
  return {
    tokens: baseTokens + estimatedDelta,
    calibration: base ?? { cardId, chars: 0, tokens: 0 },
    exact: false,
  };
}

function useLiveTokenRate(card: StreamingCardData, enabled: boolean): TokenRate {
  const calibrationRef = React.useRef<LiveTokenCalibration | null>(null);
  if (!enabled) return { tokens: 0, tps: null };
  const estimate = estimateLiveTokenCount(card.text, card.id, calibrationRef.current);
  calibrationRef.current = estimate.calibration;
  return rateFromTokens(estimate.tokens, card.ts, Date.now());
}

const PILL_RATE = { bg: "#11141a", fg: "#8b949e" } as const;

export function StreamingCard({ card }: { card: StreamingCardData }): React.ReactElement {
  const { stdout } = useStdout();
  const cols = stdout?.columns ?? 80;
  const expanded = useContext(LiveExpandContext);
  const reserveCap = expanded ? EXPANDED_MAX_LINES + 2 : STREAMING_PREVIEW_LINES + 2;
  useReserveRows("stream", {
    min: STREAMING_PREVIEW_LINES + 1,
    max: reserveCap,
  });
  // Re-render at 1Hz so the rate keeps updating even when chunks stall.
  // Frozen once `card.done` is true — settled cards render via Static.
  useSlowTick();
  const liveRate = useLiveTokenRate(card, !card.done && !card.aborted);
  const lineCells = Math.max(20, cols - 4);
  const visualLines = useIncrementalWrap(card.text, lineCells);

  const modelBadge = card.model ? modelBadgeFor(card.model) : null;
  const modelPill = modelBadge ? (
    <Pill label={modelBadge.label} {...PILL_MODEL[modelBadge.kind]} bold={false} />
  ) : null;

  if (card.done && !card.aborted) {
    const { tokens, tps } = tokenRate(card.text, card.ts, card.endedAt ?? Date.now());
    const ratePill =
      tokens >= MIN_TOKENS_FOR_RATE && tps !== null ? (
        <Pill label={`${formatTokenCount(tokens)} tok · ${tps} t/s`} {...PILL_RATE} bold={false} />
      ) : null;
    return (
      <Card tone={TONE.ok}>
        <CardHeader
          glyph="‹"
          tone={TONE.ok}
          title={t("cardTitles.reply")}
          right={
            <>
              {ratePill}
              {modelPill}
            </>
          }
        />
        <Markdown text={card.text} />
      </Card>
    );
  }

  const cap = expanded ? EXPANDED_MAX_LINES : STREAMING_PREVIEW_LINES;
  const visible = visualLines.slice(-cap);
  const droppedAbove = Math.max(0, visualLines.length - visible.length);
  const aborted = !!card.aborted;
  const headColor = aborted ? TONE.err : TONE_ACTIVE.brand;
  const glyph = aborted ? "‹" : "◈";
  const headLabel = aborted ? t("cardLabels.aborted") : t("cardLabels.writing");

  const liveRatePill =
    !aborted && liveRate.tokens >= MIN_TOKENS_FOR_RATE && liveRate.tps !== null ? (
      <Pill label={`${liveRate.tps} t/s`} {...PILL_RATE} bold={false} />
    ) : null;
  const expandPill = !aborted ? (
    <Pill label={expanded ? "expanded ⌃o" : "preview ⌃o"} {...PILL_RATE} bold={false} />
  ) : null;

  return (
    <Card tone={headColor}>
      <CardHeader
        glyph={glyph}
        tone={headColor}
        title={headLabel}
        right={
          <>
            {liveRatePill}
            {expandPill}
            {aborted ? null : <Spinner kind="braille" color={TONE_ACTIVE.brand} />}
            {modelPill}
          </>
        }
      />
      {expanded && droppedAbove > 0 ? (
        <Text color={FG.faint}>
          {t(droppedAbove === 1 ? "cardLabels.earlierLine" : "cardLabels.earlierLines", {
            count: droppedAbove,
          })}
        </Text>
      ) : null}
      {visible.map((line, i) => (
        <Box key={`${card.id}:${visualLines.length - visible.length + i}`} flexDirection="row">
          <Text color={aborted ? FG.meta : FG.body}>{clipToCells(line, lineCells)}</Text>
        </Box>
      ))}
      {aborted ? <Text color={FG.faint}>{t("cardLabels.truncatedByEsc")}</Text> : null}
    </Card>
  );
}
