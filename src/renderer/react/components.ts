import type { ReactNode } from "react";
import type { BorderStyle, BorderStyleName } from "../layout/borders.js";
import type { AnsiCode } from "../pools/style-pool.js";

export interface BoxProps {
  readonly children?: ReactNode;
  readonly flexDirection?: "column" | "row";
  readonly flexGrow?: number;
  readonly paddingTop?: number;
  readonly paddingBottom?: number;
  readonly paddingLeft?: number;
  readonly paddingRight?: number;
  readonly paddingX?: number;
  readonly paddingY?: number;
  readonly padding?: number;
  readonly borderStyle?: BorderStyle | BorderStyleName;
  readonly borderTop?: boolean;
  readonly borderBottom?: boolean;
  readonly borderLeft?: boolean;
  readonly borderRight?: boolean;
  readonly borderColor?: ReadonlyArray<AnsiCode>;
  readonly borderTopColor?: ReadonlyArray<AnsiCode>;
  readonly borderBottomColor?: ReadonlyArray<AnsiCode>;
  readonly borderLeftColor?: ReadonlyArray<AnsiCode>;
  readonly borderRightColor?: ReadonlyArray<AnsiCode>;
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
