import { Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { CARD } from "../theme/tokens.js";

export function CursorBlock(): React.ReactElement {
  return (
    <Text inverse color={CARD.streaming.color}>
      {" "}
    </Text>
  );
}
