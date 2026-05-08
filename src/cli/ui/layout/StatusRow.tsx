import { Box, Text, useStdout } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { Countdown } from "../primitives/Countdown.js";
import { useAgentState } from "../state/provider.js";
import type { Mode, NetworkState, StatusBar } from "../state/state.js";
import { FG, TONE, formatCost } from "../theme/tokens.js";

const RULE_PAD = 4;
const RULE_MIN = 20;

export function StatusRow(): React.ReactElement {
  const status = useAgentState((s) => s.status);
  const session = useAgentState((s) => s.session);
  const { stdout } = useStdout();
  const cols = stdout?.columns ?? 80;
  const ruleWidth = Math.max(RULE_MIN, cols - RULE_PAD);
  const hasTurn = status.cost > 0;

  return (
    <Box flexDirection="column">
      <Box>
        <Text>{"  "}</Text>
        <Text color={FG.faint}>{"─".repeat(ruleWidth)}</Text>
      </Box>
      <Box flexDirection="row">
        <Text>{"  "}</Text>
        {status.recording ? (
          <RecordingPill rec={status.recording} />
        ) : status.countdownSeconds !== undefined ? (
          <CountdownRow mode={status.mode} secondsLeft={status.countdownSeconds} />
        ) : (
          <ModePill mode={status.mode} network={status.network} detail={status.networkDetail} />
        )}
        <Sep />
        <Text color={FG.sub}>{`${session.id} · ${session.branch}`}</Text>
        {hasTurn && (
          <>
            <Sep />
            <Text bold color={TONE.brand}>
              {"▸ "}
            </Text>
            <Text bold color={FG.body}>
              {`${formatCost(status.cost, status.balanceCurrency)} turn`}
            </Text>
          </>
        )}
        <Sep />
        <Text color={TONE.accent}>{`cache ${Math.round(status.cacheHit * 100)}%`}</Text>
      </Box>
    </Box>
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
      <Box flexDirection="row">
        <Text color={pill.color}>{pill.glyph}</Text>
        <Text color={FG.sub}>{` ${mode}`}</Text>
      </Box>
    );
  }
  const dot = networkDot(network);
  if (network === "slow") {
    const tail = detail ? ` · ${detail}` : "";
    return (
      <Box flexDirection="row">
        <Text color={dot.color}>{dot.glyph}</Text>
        <Text color={dot.color}>{` ${mode} · slow${tail}`}</Text>
      </Box>
    );
  }
  if (network === "disconnected") {
    const tail = detail ? ` · ${detail}` : "";
    return (
      <Box flexDirection="row">
        <Text color={dot.color}>{dot.glyph}</Text>
        <Text color={dot.color}>{` disconnect${tail}`}</Text>
      </Box>
    );
  }
  return (
    <Box flexDirection="row">
      <Text color={dot.color}>{dot.glyph}</Text>
      <Text color={dot.color}>{" reconnecting…"}</Text>
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
    <Box flexDirection="row">
      <Text color={pill.color}>{pill.glyph}</Text>
      <Text color={FG.sub}>{` ${mode}   ·   `}</Text>
      <Text color={TONE.warn}>{"approving in "}</Text>
      <Countdown endsAt={endsAt} />
      <Text color={TONE.warn}>{"s · esc to interrupt"}</Text>
    </Box>
  );
}

function RecordingPill({ rec }: { rec: NonNullable<StatusBar["recording"]> }): React.ReactElement {
  const sizeMb = (rec.sizeBytes / (1024 * 1024)).toFixed(1);
  return (
    <Box flexDirection="row">
      <Text bold color={TONE.err}>
        {"●REC"}
      </Text>
      <Text color={TONE.err}>{` ${sizeMb} MB · ${rec.events} evt`}</Text>
    </Box>
  );
}

function Sep(): React.ReactElement {
  return <Text color={FG.meta}>{"   ·   "}</Text>;
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
