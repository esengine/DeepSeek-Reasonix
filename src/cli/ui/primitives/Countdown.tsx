import { Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { TONE } from "../theme/tokens.js";
import { useSlowTick } from "../ticker.js";

export interface CountdownProps {
  /** Absolute timestamp (ms since epoch) when the countdown reaches zero. */
  endsAt: number;
  /** Override digit color — default brand sky. */
  color?: string;
  backgroundColor?: string;
}

export function Countdown({
  endsAt,
  color = TONE.brand,
  backgroundColor,
}: CountdownProps): React.ReactElement {
  useSlowTick();
  const remainingSec = Math.max(0, Math.ceil((endsAt - Date.now()) / 1000));
  return (
    <Text bold color={color} backgroundColor={backgroundColor}>
      {String(remainingSec)}
    </Text>
  );
}
