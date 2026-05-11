import { Box, Text } from "ink";
import React, { useState } from "react";
import { t } from "../../i18n/index.js";
import { useKeystroke } from "./keystroke-context.js";
import { FG, TONE } from "./theme/tokens.js";

export interface DenyContextInputProps {
  description?: string;
  onSubmit: (context: string) => void;
  onCancel: () => void;
}

const DEFAULT_DESCRIPTION = t("denyContextInput.description");

export function DenyContextInput({
  description = DEFAULT_DESCRIPTION,
  onSubmit,
  onCancel,
}: DenyContextInputProps) {
  const [value, setValue] = useState("");

  useKeystroke((ev) => {
    if (ev.paste) {
      setValue((v) => v + ev.input);
      return;
    }
    if (ev.escape) {
      onCancel();
      return;
    }
    if (ev.return) {
      onSubmit(value);
      return;
    }
    if (ev.backspace) {
      setValue((v) => v.slice(0, -1));
      return;
    }
    if (ev.input && !ev.tab && !ev.upArrow && !ev.downArrow && !ev.leftArrow && !ev.rightArrow) {
      setValue((v) => v + ev.input);
    }
  });

  return (
    <Box flexDirection="column">
      <Box flexDirection="column" marginBottom={1}>
        <Text color={FG.sub}>{description}</Text>
      </Box>
      <Box>
        <Text bold color={TONE.brand}>
          {"› "}
        </Text>
        <Text color={FG.body}>{value}</Text>
        <Text backgroundColor={TONE.brand} color={"#000"}>
          {" "}
        </Text>
      </Box>
    </Box>
  );
}
