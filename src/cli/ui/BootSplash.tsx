import { Box, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React, { useEffect, useState } from "react";
import { t } from "../../i18n/index.js";
import { FG, TONE } from "./theme/tokens.js";

const REASONIX_LOGO = [
  "██████╗ ███████╗ █████╗ ███████╗ ██████╗ ███╗   ██╗██╗██╗  ██╗",
  "██╔══██╗██╔════╝██╔══██╗██╔════╝██╔═══██╗████╗  ██║██║╚██╗██╔╝",
  "██████╔╝█████╗  ███████║███████╗██║   ██║██╔██╗ ██║██║ ╚███╔╝ ",
  "██╔══██╗██╔══╝  ██╔══██║╚════██║██║   ██║██║╚██╗██║██║ ██╔██╗ ",
  "██║  ██║███████╗██║  ██║███████║╚██████╔╝██║ ╚████║██║██╔╝ ██╗",
  "╚═╝  ╚═╝╚══════╝╚═╝  ╚═╝╚══════╝ ╚═════╝ ╚═╝  ╚═══╝╚═╝╚═╝  ╚═╝",
];

const WHALE_LINES = [
  "                _____:_____",
  "          __.-''           ''-.__",
  "       ,-'   ░░▒▒▒▒▒▒▒▒▒▒▒▒░░     '-.",
  "     ,'   ░▒▒▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▒▒░     '\\",
  "    /   ░▒▓▓▓▓◉▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▒░     '\\___",
  "   |  ░▒▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▒░         '\\\\__",
  "   |  ░▒▓▓▓▓▓▓ ‿‿‿ ▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▒░          \\\\__\\",
  "   |  ░▒▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▒░          //__/",
  "    \\   ░▒▒▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▒▒░         //",
  "     '\\.   ░░▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒░░       __,/'",
  "        '-..___                 ___..-'",
  "              '''---..........---'''",
];

const SPOUT_FRAMES: ReadonlyArray<readonly string[]> = [
  ["                  ", "                  ", "                  "],
  ["                  ", "                  ", "                 ."],
  ["                  ", "                 .", "                 :"],
  ["                 .", "                 :", "                 :"],
  ["              .  '  .", "                 :", "                 :"],
  ["              '  .  '", "                 '", "                 :"],
  ["              .     .", "                  '", "                  "],
];

const WAVE_SOURCE = "~^~^~^~^~^~^~^~^~^~^~^~^~^~^~^~^~^~^~^~^~^~^~^~^~^~^~^";
const WAVE_WIDTH = 44;
const FRAME_MS = 200;

export function BootSplash(): React.ReactElement {
  const [frame, setFrame] = useState(0);
  useEffect(() => {
    const t = setInterval(() => setFrame((f) => f + 1), FRAME_MS);
    return () => clearInterval(t);
  }, []);
  const spout = SPOUT_FRAMES[frame % SPOUT_FRAMES.length] as readonly string[];
  const waveOffset = frame % 4;
  const wave = WAVE_SOURCE.slice(waveOffset, waveOffset + WAVE_WIDTH);
  const dots = ".".repeat((frame % 4) + 1);
  return (
    <Box flexDirection="column" alignItems="center" marginY={1}>
      <Box flexDirection="column" alignItems="flex-start" marginBottom={1}>
        {REASONIX_LOGO.map((line) => (
          <Text key={line} color={TONE.brand} bold>
            {line}
          </Text>
        ))}
      </Box>
      <Box flexDirection="column" alignItems="flex-start">
        {spout.map((line, i) => (
          // biome-ignore lint/suspicious/noArrayIndexKey: fixed-length spout column, position is the identity
          <Text key={i} color={TONE.accent}>
            {line.length > 0 ? line : " "}
          </Text>
        ))}
        {WHALE_LINES.map((line) => (
          <Text key={line} color={TONE.brand} bold>
            {line}
          </Text>
        ))}
        <Text color={FG.faint}>{wave}</Text>
      </Box>
      <Box marginTop={1}>
        <Text color={FG.meta}>{`${t("common.loading")}${dots}`}</Text>
      </Box>
    </Box>
  );
}
