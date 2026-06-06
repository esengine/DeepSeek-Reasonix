import { Box, type Color, Text, useStdout } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { t } from "../../../i18n/index.js";
import { resolveContextTokens } from "../../../telemetry/stats.js";
import { VERSION } from "../../../version.js";
import { formatTokens } from "../primitives.js";
import { Countdown } from "../primitives/Countdown.js";
import { useAgentState } from "../state/provider.js";
import type { Mode, NetworkState, StatusBar } from "../state/state.js";
import { GLYPH } from "../theme.js";
import { FG, SURFACE, TONE, balanceColor, formatBalance, formatCost } from "../theme/tokens.js";

export interface StatusBarConfig {
  showBalance: boolean;
  showSessionCost: boolean;
  showTurnCost: boolean;
  showCacheHit: boolean;
  showCtxUsage: boolean;
  showVersion: boolean;
  showFeedbackHint: boolean;
}

const WALLET_MIN_COLS = 90;
const VERSION_MIN_COLS = 70;
const FEEDBACK_HINT_MIN_COLS = 100;

const CTX_TOKENS_MIN_COLS = 90;
const CTX_BAR_MIN_COLS = 110;
const CTX_BAR_CELLS = 8;

const DEFAULT_STATUS_BAR_CONFIG: StatusBarConfig = {
  showBalance: true,
  showSessionCost: true,
  showTurnCost: true,
  showCacheHit: true,
  showCtxUsage: true,
  showVersion: true,
  showFeedbackHint: true,
};

const BG = SURFACE.bgElev;

function Pill({ children }: { children: React.ReactNode }): React.ReactElement {
  return (
    <Box flexDirection="row" flexShrink={0}>
      {children}
    </Box>
  );
}

function Gap(): React.ReactElement {
  return <Text> </Text>;
}

export function StatusRow({
  statusBar = DEFAULT_STATUS_BAR_CONFIG,
}: { statusBar?: StatusBarConfig }): React.ReactElement {
  const status = useAgentState((s) => s.status);
  const session = useAgentState((s) => s.session);
  const { stdout } = useStdout();
  const cols = stdout?.columns ?? 80;
  const hasTurn = status.cost > 0;
  const hasSession = status.sessionCost > 0;
  const hasBalance = typeof status.balance === "number";
  const showWallet =
    cols >= WALLET_MIN_COLS &&
    ((hasSession && statusBar.showSessionCost) || (hasBalance && statusBar.showBalance));

  return (
    <Box flexDirection="row" flexShrink={0} marginTop={1}>
      <Box flexDirection="row" flexWrap="wrap" flexGrow={1}>
        <Text> </Text>
        {status.recording ? (
          <Pill>
            <RecordingPill rec={status.recording} />
          </Pill>
        ) : status.countdownSeconds !== undefined ? (
          <Pill>
            <CountdownRow mode={status.mode} secondsLeft={status.countdownSeconds} />
          </Pill>
        ) : (
          <Pill>
            <ModePill mode={status.mode} network={status.network} detail={status.networkDetail} />
          </Pill>
        )}
        <Gap />
        <Pill>
          <Text color={FG.sub}>{`${session.id} · ${session.branch}`}</Text>
        </Pill>
        {hasTurn && statusBar.showTurnCost && (
          <>
            <Gap />
            <Pill>
              <Text bold color={TONE.brand}>
                {"▸ "}
              </Text>
              <Text bold color={FG.body}>
                {`${formatCost(status.cost, status.costDisplayCurrency ?? status.balanceCurrency)} ${t("statusBar.turn")}`}
              </Text>
            </Pill>
          </>
        )}
        {statusBar.showCacheHit && (
          <>
            <Gap />
            <Pill>
              <Text color={TONE.accent}>
                {`${t("statusBar.cache")} ${Math.round(status.cacheHit * 100)}%`}
              </Text>
            </Pill>
          </>
        )}
        {statusBar.showCtxUsage && status.promptTokens !== undefined && status.promptTokens > 0 && (
          <>
            <Gap />
            <Pill>
              <CtxUsagePill
                tokens={status.promptTokens}
                cap={status.promptCap ?? resolveContextTokens(session.model)}
                cols={cols}
              />
            </Pill>
          </>
        )}
        {status.mcpLoading && status.mcpLoading.ready < status.mcpLoading.total && (
          <>
            <Gap />
            <Pill>
              <McpLoadingPill ready={status.mcpLoading.ready} total={status.mcpLoading.total} />
            </Pill>
          </>
        )}
        {showWallet && (
          <>
            <Gap />
            <Pill>
              <WalletPill
                sessionCostUsd={status.sessionCost}
                balance={status.balance}
                currency={status.balanceCurrency}
                showSessionCost={statusBar.showSessionCost}
                showBalance={statusBar.showBalance}
              />
            </Pill>
          </>
        )}
      </Box>
      <Box flexDirection="row" flexShrink={0}>
        {statusBar.showVersion && cols >= VERSION_MIN_COLS && (
          <Pill>
            <Text color={FG.faint}>{`v${VERSION}`}</Text>
          </Pill>
        )}
        {statusBar.showFeedbackHint && cols >= FEEDBACK_HINT_MIN_COLS && (
          <>
            <Gap />
            <Pill>
              <Text color={FG.meta}>{"⚑ "}</Text>
              <Text color={FG.sub}>{t("statusBar.shortcutsHint")}</Text>
            </Pill>
          </>
        )}
      </Box>
    </Box>
  );
}

function CtxUsagePill({
  tokens,
  cap,
  cols,
}: {
  tokens: number;
  cap: number;
  cols: number;
}): React.ReactElement {
  const ratio = cap > 0 ? Math.min(1, tokens / cap) : 0;
  const pct = Math.round(ratio * 100);
  const color = ratio >= 0.8 ? TONE.err : ratio >= 0.5 ? TONE.warn : TONE.ok;
  const showTokens = cols >= CTX_TOKENS_MIN_COLS;
  const showBar = cols >= CTX_BAR_MIN_COLS;
  const filled = Math.round(CTX_BAR_CELLS * ratio);
  return (
    <>
      <Text color={FG.meta} wrap="truncate">{`${t("statusBar.ctx")} `}</Text>
      {showBar && (
        <>
          <Text color={color} wrap="truncate">
            {GLYPH.block.repeat(filled)}
          </Text>
          <Text color={FG.faint} wrap="truncate">
            {GLYPH.shade1.repeat(CTX_BAR_CELLS - filled)}
          </Text>
          <Text wrap="truncate"> </Text>
        </>
      )}
      <Text color={color} wrap="truncate">{`${pct}%`}</Text>
      {showTokens && (
        <Text color={FG.faint}>{` · ${formatTokens(tokens)}/${formatTokens(cap)}`}</Text>
      )}
    </>
  );
}

function McpLoadingPill({
  ready,
  total,
}: {
  ready: number;
  total: number;
}): React.ReactElement {
  return (
    <>
      <Text color={TONE.brand} wrap="truncate">
        {"⌁ "}
      </Text>
      <Text color={FG.body}>{`${t("statusBar.mcpLoading")} ${ready}/${total}`}</Text>
    </>
  );
}

function WalletPill({
  sessionCostUsd,
  balance,
  currency,
  showSessionCost,
  showBalance: showBalanceCfg,
}: {
  sessionCostUsd: number;
  balance?: number;
  currency?: string;
  showSessionCost: boolean;
  showBalance: boolean;
}): React.ReactElement {
  const showSpent = showSessionCost && sessionCostUsd > 0;
  const showBalanceLine = showBalanceCfg && typeof balance === "number";
  return (
    <>
      <Text color={FG.meta} wrap="truncate">
        {"⛁ "}
      </Text>
      {showSpent && (
        <Text color={FG.body}>
          {`${formatCost(sessionCostUsd, currency, 2)} ${t("statusBar.spent")}`}
        </Text>
      )}
      {showSpent && showBalanceLine && (
        <Text color={FG.meta} wrap="truncate">
          {"  /  "}
        </Text>
      )}
      {showBalanceLine && (
        <Text color={FG.faint} wrap="truncate">
          {t("statusBar.left")}
        </Text>
      )}
      {showBalanceLine && (
        <Text bold color={balanceColor(balance, currency)} wrap="truncate">
          {formatBalance(balance, currency, { fractionDigits: 2 })}
        </Text>
      )}
    </>
  );
}

function ModePill({
  mode,
  network,
  detail,
}: {
  mode: Mode;
  network: NetworkState;
  detail?: string;
}): React.ReactElement {
  const modeLabel = `${t("statusBar.editsLabel")}${mode}`;
  if (network === "online") {
    const pill = modeGlyph(mode);
    return (
      <>
        <Text color={pill.color} wrap="truncate">
          {pill.glyph}
        </Text>
        <Text color={FG.sub} wrap="truncate">{` ${modeLabel}`}</Text>
      </>
    );
  }
  const dot = networkDot(network);
  if (network === "slow") {
    const tail = detail ? ` · ${detail}` : "";
    return (
      <>
        <Text color={dot.color} wrap="truncate">
          {dot.glyph}
        </Text>
        <Text color={dot.color}>{` ${modeLabel} · ${t("statusBar.slow")}${tail}`}</Text>
      </>
    );
  }
  if (network === "disconnected") {
    const tail = detail ? ` · ${detail}` : "";
    return (
      <>
        <Text color={dot.color} wrap="truncate">
          {dot.glyph}
        </Text>
        <Text color={dot.color} wrap="truncate">
          {` ${t("statusBar.disconnect")}${tail}`}
        </Text>
      </>
    );
  }
  return (
    <>
      <Text color={dot.color} wrap="truncate">
        {dot.glyph}
      </Text>
      <Text color={dot.color} wrap="truncate">
        {` ${t("statusBar.reconnecting")}`}
      </Text>
    </>
  );
}

function CountdownRow({
  mode,
  secondsLeft,
}: {
  mode: Mode;
  secondsLeft: number;
}): React.ReactElement {
  const pill = modeGlyph(mode);
  const endsAt = Date.now() + secondsLeft * 1000;
  return (
    <>
      <Text color={pill.color} wrap="truncate">
        {pill.glyph}
      </Text>
      <Text color={FG.sub} wrap="truncate">
        {` ${t("statusBar.editsLabel")}${mode} · `}
      </Text>
      <Text color={TONE.warn} wrap="truncate">
        {t("statusBar.approvingIn")}
      </Text>
      <Countdown endsAt={endsAt} />
      <Text color={TONE.warn} wrap="truncate">
        {t("statusBar.escToInterrupt")}
      </Text>
    </>
  );
}

function RecordingPill({ rec }: { rec: NonNullable<StatusBar["recording"]> }): React.ReactElement {
  const sizeMb = (rec.sizeBytes / (1024 * 1024)).toFixed(1);
  return (
    <>
      <Text bold color={TONE.err} wrap="truncate">
        {t("statusBar.recordingGlyph")}
      </Text>
      <Text color={TONE.err}>
        {` ${sizeMb}${t("statusBar.mb")} · ${rec.events}${t("statusBar.evt")}`}
      </Text>
    </>
  );
}

function modeGlyph(mode: Mode): { glyph: string; color: Color } {
  switch (mode) {
    case "auto":
      return { glyph: "●", color: TONE.ok };
    case "ask":
      return { glyph: "◐", color: TONE.warn };
    case "plan":
      return { glyph: "⊞", color: TONE.accent };
    case "edit":
      return { glyph: "±", color: TONE.ok };
  }
}

function networkDot(state: NetworkState): { glyph: string; color: Color } {
  switch (state) {
    case "online":
      return { glyph: "●", color: TONE.ok };
    case "slow":
      return { glyph: "◌", color: TONE.warn };
    case "disconnected":
      return { glyph: "✗", color: TONE.err };
    case "reconnecting":
      return { glyph: "↻", color: TONE.brand };
  }
}

export type { StatusBar };
