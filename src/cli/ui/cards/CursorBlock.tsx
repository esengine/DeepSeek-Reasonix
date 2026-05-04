/** Blinking caret cell shown at the tail of streaming reasoning text. */

import { Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { TONE } from "../theme/tokens.js";
import { useTick } from "../ticker.js";

export function CursorBlock(): React.ReactElement {
  const tick = useTick();
  const on = Math.floor(tick / 4) % 2 === 0;
  return (
    <Text inverse={on} color={TONE.brand}>
      {" "}
    </Text>
  );
}
