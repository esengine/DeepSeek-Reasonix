import { Box, Text, useStdout } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { t } from "../../../i18n/index.js";
import { VERSION } from "../../../version.js";
import { Countdown } from "../primitives/Countdown.js";
import { useAgentState } from "../state/provider.js";
import type { Mode, NetworkState, StatusBar } from "../state/state.js";
import { FG, TONE, balanceColor, formatBalance, formatCost } from "../theme/tokens.js";

const RULE_PAD = 4;
const RULE_MIN = 20;
const WALLET_MIN_COLS = 90;
const VERSION_MIN_COLS = 70;
const FEEDBACK_HINT_MIN_COLS = 100;

export function StatusRow(): React.ReactElement {
  const status = useAgentState((s) => s.status);
  const session = useAgentState((s) => s.session);
  const { stdout } = useStdout();
  const cols = stdout?.columns ?? 80;
  const ruleWidth = Math.max(RULE_MIN, cols - RULE_PAD);
  const hasTurn = status.cost > 0;
  const hasSession = status.sessionCost > 0;
  const hasBalance = typeof status.balance === "number";
  const showWallet = cols >= WALLET_MIN_COLS && (hasSession || hasBalance);

  return (
    <Box flexDirection="column" flexShrink={0} flexWrap="nowrap">
      <Box height={1} flexWrap="nowrap">
        <Text>{"  "}</Text>
        <Text color={FG.faint} wrap="truncate">
          {"─".repeat(ruleWidth)}
        </Text>
      </Box>
      <Box flexDirection="row" height={1} minHeight={1} flexWrap="nowrap" flexShrink={0}>
        <Text wrap="truncate">{"  "}</Text>
        {status.recording ? (
          <RecordingPill rec={status.recording} />
        ) : status.countdownSeconds !== undefined ? (
          <CountdownRow mode={status.mode} secondsLeft={status.countdownSeconds} />
        ) : (
          <ModePill mode={status.mode} network={status.network} detail={status.networkDetail} />
        )}
        <Sep />
        <Text color={FG.sub} wrap="truncate">{`${session.id} · ${session.branch}`}</Text>
        {hasTurn && (
          <>
            <Sep />
            <Text bold color={TONE.brand} wrap="truncate">
              {"▸ "}
            </Text>
            <Text bold color={FG.body} wrap="truncate">
              {`${formatCost(status.cost, status.balanceCurrency)} ${t("statusBar.turn")}`}
            </Text>
          </>
        )}
        <Sep />
        <Text
          color={TONE.accent}
          wrap="truncate"
        >{`${t("statusBar.cache")} ${Math.round(status.cacheHit * 100)}%`}</Text>
        {showWallet && (
          <WalletPill
            sessionCostUsd={status.sessionCost}
            balance={status.balance}
            currency={status.balanceCurrency}
          />
        )}
        {cols >= VERSION_MIN_COLS && (
          <>
            <Sep />
            <Text color={FG.faint} wrap="truncate">{`v${VERSION}`}</Text>
            {cols >= FEEDBACK_HINT_MIN_COLS && (
              <>
                <Text color={FG.faint} wrap="truncate">
                  {"  ·  "}
                </Text>
                <Text color={FG.meta} wrap="truncate">
                  {"⚑ "}
                </Text>
                <Text color={FG.sub} wrap="truncate">
                  {"/feedback"}
                </Text>
              </>
            )}
          </>
        )}
      </Box>
    </Box>
  );
}

function WalletPill({
  sessionCostUsd,
  balance,
  currency,
}: {
  sessionCostUsd: number;
  balance?: number;
  currency?: string;
}): React.ReactElement {
  const showSpent = sessionCostUsd > 0;
  const showBalance = typeof balance === "number";
  return (
    <>
      <Sep />
      <Text color={FG.meta} wrap="truncate">
        {"⛁ "}
      </Text>
      {showSpent && (
        <Text
          color={FG.body}
          wrap="truncate"
        >{`${formatCost(sessionCostUsd, currency, 2)} ${t("statusBar.spent")}`}</Text>
      )}
      {showSpent && showBalance && (
        <Text color={FG.meta} wrap="truncate">
          {"  /  "}
        </Text>
      )}
      {showBalance && (
        <Text bold color={balanceColor(balance, currency)} wrap="truncate">
          {formatBalance(balance, currency, { fractionDigits: 2 })}
        </Text>
      )}
      {showBalance && (
        <Text color={FG.faint} wrap="truncate">
          {t("statusBar.left")}
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
  if (network === "online") {
    const pill = modeGlyph(mode);
    return (
      <Box flexDirection="row" height={1} flexWrap="nowrap">
        <Text color={pill.color} wrap="truncate">
          {pill.glyph}
        </Text>
        <Text color={FG.sub} wrap="truncate">{` ${mode}`}</Text>
      </Box>
    );
  }
  const dot = networkDot(network);
  if (network === "slow") {
    const tail = detail ? ` · ${detail}` : "";
    return (
      <Box flexDirection="row" height={1} flexWrap="nowrap">
        <Text color={dot.color} wrap="truncate">
          {dot.glyph}
        </Text>
        <Text color={dot.color} wrap="truncate">{` ${mode} · ${t("statusBar.slow")}${tail}`}</Text>
      </Box>
    );
  }
  if (network === "disconnected") {
    const tail = detail ? ` · ${detail}` : "";
    return (
      <Box flexDirection="row" height={1} flexWrap="nowrap">
        <Text color={dot.color} wrap="truncate">
          {dot.glyph}
        </Text>
        <Text color={dot.color} wrap="truncate">{` ${t("statusBar.disconnect")}${tail}`}</Text>
      </Box>
    );
  }
  return (
    <Box flexDirection="row" height={1} flexWrap="nowrap">
      <Text color={dot.color} wrap="truncate">
        {dot.glyph}
      </Text>
      <Text color={dot.color} wrap="truncate">
        {` ${t("statusBar.reconnecting")}`}
      </Text>
    </Box>
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
    <Box flexDirection="row" height={1} flexWrap="nowrap">
      <Text color={pill.color} wrap="truncate">
        {pill.glyph}
      </Text>
      <Text color={FG.sub} wrap="truncate">{` ${mode}   ·   `}</Text>
      <Text color={TONE.warn} wrap="truncate">
        {t("statusBar.approvingIn")}
      </Text>
      <Countdown endsAt={endsAt} />
      <Text color={TONE.warn} wrap="truncate">
        {t("statusBar.escToInterrupt")}
      </Text>
    </Box>
  );
}

function RecordingPill({ rec }: { rec: NonNullable<StatusBar["recording"]> }): React.ReactElement {
  const sizeMb = (rec.sizeBytes / (1024 * 1024)).toFixed(1);
  return (
    <Box flexDirection="row" height={1} flexWrap="nowrap">
      <Text bold color={TONE.err} wrap="truncate">
        {t("statusBar.recordingGlyph")}
      </Text>
      <Text
        color={TONE.err}
        wrap="truncate"
      >{` ${sizeMb}${t("statusBar.mb")} · ${rec.events}${t("statusBar.evt")}`}</Text>
    </Box>
  );
}

function Sep(): React.ReactElement {
  return (
    <Text color={FG.meta} wrap="truncate">
      {"   ·   "}
    </Text>
  );
}

function modeGlyph(mode: Mode): { glyph: string; color: string } {
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

function networkDot(state: NetworkState): { glyph: string; color: string } {
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
