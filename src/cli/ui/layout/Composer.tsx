import { Box, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { t } from "../../../i18n/index.js";
import { HintRow } from "../PromptInput.js";
import { useAgentState } from "../state/provider.js";
import { useThemeTokens } from "../theme/context.js";
import { StatusRow } from "./StatusRow.js";

export function Composer(): React.ReactElement {
  const composer = useAgentState((s) => s.composer);
  const { fg, tone } = useThemeTokens();

  return (
    <Box flexDirection="column">
      <StatusRow />
      <Box height={1} />
      <Box flexDirection="row">
        <Text bold color={composer.shell ? tone.err : tone.brand}>
          {composer.shell ? "$" : "›"}{" "}
        </Text>
        {composer.value.length === 0 ? (
          <Text color={fg.meta}>{t("composer.placeholder")}</Text>
        ) : (
          <Text color={fg.body}>{composer.value}</Text>
        )}
      </Box>
      <Box height={1} />
      {composer.abortedHint ? (
        <Text color={fg.faint}>
          {"  "}
          {t("composer.abortedHint")}
        </Text>
      ) : (
        <HintRow />
      )}
    </Box>
  );
}
