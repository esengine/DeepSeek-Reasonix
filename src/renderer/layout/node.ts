import type { AnsiCode } from "../pools/style-pool.js";

export interface TextNode {
  readonly kind: "text";
  readonly content: string;
  readonly style?: ReadonlyArray<AnsiCode>;
  readonly hyperlink?: string;
}

export interface BoxNode {
  readonly kind: "box";
  readonly children: ReadonlyArray<LayoutNode>;
}

export type LayoutNode = TextNode | BoxNode;
