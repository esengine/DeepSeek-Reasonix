/** Block-character progress bar used by usage / context / branch cards. */

import { Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { FG } from "../theme/tokens.js";

export interface ProgressBarProps {
  /** 0..1 fill ratio. Out-of-range values are clamped. */
  readonly ratio: number;
  /** Filled-cell color. */
  readonly color: string;
  /** Total cells the bar spans. Default 28. */
  readonly cells?: number;
}

export function ProgressBar({ ratio, color, cells = 28 }: ProgressBarProps): React.ReactElement {
  const clamped = Math.max(0, Math.min(1, ratio));
  const filled = Math.max(0, Math.min(cells, Math.round(clamped * cells)));
  const empty = cells - filled;
  return (
    <>
      <Text color={color}>{"█".repeat(filled)}</Text>
      <Text color={FG.faint}>{"░".repeat(empty)}</Text>
    </>
  );
}
