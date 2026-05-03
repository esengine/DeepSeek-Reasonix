import type { ReactNode } from "react";
import type { AnsiCode } from "../pools/style-pool.js";

export interface BoxProps {
  readonly children?: ReactNode;
}

export interface TextProps {
  readonly children?: ReactNode;
  readonly style?: ReadonlyArray<AnsiCode>;
  readonly hyperlink?: string;
}

const HOST_GUARD = "Reasonix renderer host element — only rendered through cell-tui's render().";

export function Box(_props: BoxProps): never {
  throw new Error(HOST_GUARD);
}

export function Text(_props: TextProps): never {
  throw new Error(HOST_GUARD);
}
