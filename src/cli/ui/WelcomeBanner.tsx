/** Empty-session welcome card — REASONIX × 🐋 DeepSeek brand row + tagline + starter slash commands. */

import { Box, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { t } from "../../i18n/index.js";
import { FG, TONE } from "./theme/tokens.js";

export interface WelcomeBannerProps {
  /** True when running `reasonix code`. Surfaces code-mode hints. */
  inCodeMode?: boolean;
  /** Pinned workspace root — only meaningful in code mode. Surfaced so first-time users see they can pass --dir at next launch. */
  workspaceRoot?: string;
  /** Live URL of the embedded dashboard, or null when it isn't running. */
  dashboardUrl?: string | null;
  /** Bumped on language change; forces re-render so t() picks up new locale. */
  languageVersion?: number;
}

const HINTS = ["/help", "/init", "/memory", "/cost"] as const;

export function WelcomeBanner({
  inCodeMode,
  workspaceRoot,
  dashboardUrl,
}: WelcomeBannerProps): React.ReactElement {
  const tagline = inCodeMode ? t("ui.taglineCode") : t("ui.taglineChat");
  const taglineSub = t("ui.taglineSub");
  const startTextRaw = t("ui.startSessionHint");

  return (
    <Box flexDirection="column" alignItems="center" marginY={1}>
      <Box
        flexDirection="column"
        alignItems="center"
        borderStyle="round"
        borderColor={TONE.brand}
        paddingX={4}
        paddingY={1}
      >
        <Box flexDirection="row" gap={2}>
          <Text color={TONE.brand} bold>
            {"REASONIX"}
          </Text>
          <Text color={FG.faint}>{"×"}</Text>
          <Box flexDirection="row" gap={1}>
            <Text>{"🐋"}</Text>
            <Text color={TONE.accent} bold>
              {"DeepSeek"}
            </Text>
          </Box>
        </Box>

        <Box marginTop={1} flexDirection="column" alignItems="center">
          <Text color={FG.body}>{tagline}</Text>
          <Text color={FG.meta}>{taglineSub}</Text>
        </Box>
      </Box>

      <Box marginTop={1}>
        <Text color={FG.sub}>{startTextRaw}</Text>
      </Box>

      <Box marginTop={1} flexDirection="row" gap={3}>
        {HINTS.map((cmd) => (
          <Text key={cmd} color={FG.meta}>
            {cmd}
          </Text>
        ))}
      </Box>

      {inCodeMode && workspaceRoot ? (
        <Box marginTop={1} flexDirection="row" gap={1}>
          <Text color={TONE.brand}>{"▸ workspace"}</Text>
          <Text color={FG.faint}>{"·"}</Text>
          <Text color={FG.body}>{workspaceRoot}</Text>
          <Text color={FG.faint}>{"  (relaunch with --dir <path> to switch)"}</Text>
        </Box>
      ) : null}

      {dashboardUrl ? (
        <Box marginTop={1} flexDirection="row" gap={1}>
          <Text color={TONE.brand} bold>
            {"▸ web"}
          </Text>
          <Text color={FG.faint}>{"·"}</Text>
          <Text color={TONE.accent}>{dashboardUrl}</Text>
        </Box>
      ) : null}
    </Box>
  );
}
