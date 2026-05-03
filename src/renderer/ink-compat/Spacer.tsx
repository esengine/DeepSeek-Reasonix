// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { Box } from "../react/components.js";

export function Spacer(): React.ReactElement {
  return <Box flexGrow={1} />;
}
