// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React, { type ReactNode, useContext, useEffect, useRef } from "react";
import { Box } from "../react/components.js";
import { RendererBridgeContext } from "./renderer-bridge.js";

export interface StaticProps<T> {
  readonly items: ReadonlyArray<T>;
  readonly children: (item: T, index: number) => ReactNode;
}

export function Static<T>(props: StaticProps<T>): React.ReactElement | null {
  const bridge = useContext(RendererBridgeContext);
  const lastIndex = useRef(0);

  useEffect(() => {
    if (!bridge) return;
    if (props.items.length <= lastIndex.current) return;
    const fresh = props.items.slice(lastIndex.current);
    const startIndex = lastIndex.current;
    lastIndex.current = props.items.length;
    bridge.emitStatic(
      <Box flexDirection="column">
        {fresh.map((item, i) => (
          // biome-ignore lint/suspicious/noArrayIndexKey: Static items are positional and append-only
          <React.Fragment key={`s-${startIndex + i}`}>
            {props.children(item, startIndex + i)}
          </React.Fragment>
        ))}
      </Box>,
    );
  }, [bridge, props.items, props.children]);

  if (bridge) return null;
  return (
    <Box flexDirection="column">
      {props.items.map((item, i) => (
        // biome-ignore lint/suspicious/noArrayIndexKey: fallback path with no bridge — positional keys are fine
        <React.Fragment key={`s-${i}`}>{props.children(item, i)}</React.Fragment>
      ))}
    </Box>
  );
}
