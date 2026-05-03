import type { AnsiCode } from "../pools/style-pool.js";
import type { BorderStyle, BorderStyleName } from "./borders.js";

export interface TextNode {
  readonly kind: "text";
  readonly content: string;
  readonly style?: ReadonlyArray<AnsiCode>;
  readonly hyperlink?: string;
}

export type JustifyContent = "flex-start" | "flex-end" | "center" | "space-between";

export interface BoxNode {
  readonly kind: "box";
  readonly children: ReadonlyArray<LayoutNode>;
  readonly flexDirection?: "column" | "row";
  readonly flexGrow?: number;
  readonly justifyContent?: JustifyContent;
  readonly width?: number;
  readonly height?: number;
  readonly paddingTop?: number;
  readonly paddingBottom?: number;
  readonly paddingLeft?: number;
  readonly paddingRight?: number;
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

export type LayoutNode = TextNode | BoxNode;
