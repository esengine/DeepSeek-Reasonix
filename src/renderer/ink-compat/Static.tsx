// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React, { type ReactNode } from "react";
import { Box } from "../react/components.js";

export interface StaticProps<T> {
  readonly items: ReadonlyArray<T>;
  readonly children: (item: T, index: number) => ReactNode;
}

export function Static<T>(props: StaticProps<T>): React.ReactElement {
  return (
    <Box flexDirection="column">
      {props.items.map((item, i) => (
        // biome-ignore lint/suspicious/noArrayIndexKey: Static items are positional and stable per render — Ink uses index keys here too
        <React.Fragment key={`s-${i}`}>{props.children(item, i)}</React.Fragment>
      ))}
    </Box>
  );
}
