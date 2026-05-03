import React, { type ReactElement, type ReactNode } from "react";
import type { BorderStyle, BorderStyleName } from "../layout/borders.js";
import type { JustifyContent } from "../layout/node.js";
import type { AnsiCode } from "../pools/style-pool.js";

export const HOST_BOX = "rsx-box" as const;
export const HOST_TEXT = "rsx-text" as const;

export interface BoxProps {
  readonly children?: ReactNode;
  readonly flexDirection?: "column" | "row";
  readonly flexGrow?: number;
  readonly justifyContent?: JustifyContent;
  readonly width?: number;
  readonly height?: number;
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

export function Box(props: BoxProps): ReactElement {
  return React.createElement(HOST_BOX, props);
}

export function Text(props: TextProps): ReactElement {
  return React.createElement(HOST_TEXT, props);
}
