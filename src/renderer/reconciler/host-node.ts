import type { BoxNode, LayoutNode, TextNode } from "../layout/node.js";
import type { BoxProps, TextProps } from "../react/components.js";

export type HostType = "box" | "text" | "raw-text";

export interface HostBox {
  readonly type: "box";
  props: BoxProps;
  children: HostNode[];
  parent: HostBox | null;
}

export interface HostText {
  readonly type: "text";
  props: TextProps;
  children: HostNode[];
  parent: HostBox | null;
}

export interface HostRawText {
  readonly type: "raw-text";
  text: string;
  parent: HostBox | HostText | null;
}

export type HostNode = HostBox | HostText | HostRawText;

export function makeHostBox(props: BoxProps): HostBox {
  return { type: "box", props, children: [], parent: null };
}

export function makeHostText(props: TextProps): HostText {
  return { type: "text", props, children: [], parent: null };
}

export function makeHostRawText(text: string): HostRawText {
  return { type: "raw-text", text, parent: null };
}

export function hostToLayoutNode(node: HostNode): LayoutNode | null {
  if (node.type === "raw-text") {
    return { kind: "text", content: node.text };
  }
  if (node.type === "text") {
    return {
      kind: "text",
      content: collectText(node.children),
      ...(node.props.style ? { style: node.props.style } : {}),
      ...(node.props.hyperlink ? { hyperlink: node.props.hyperlink } : {}),
    } satisfies TextNode;
  }
  const layoutChildren: LayoutNode[] = [];
  for (const c of node.children) {
    const child = hostToLayoutNode(c);
    if (child) layoutChildren.push(child);
  }
  const p = node.props;
  const out: BoxNode = { kind: "box", children: layoutChildren };
  return assignBoxProps(out, p);
}

function collectText(children: HostNode[]): string {
  let out = "";
  for (const child of children) {
    if (child.type === "raw-text") out += child.text;
    else if (child.type === "text") out += collectText(child.children);
  }
  return out;
}

function assignBoxProps(node: BoxNode, props: BoxProps): BoxNode {
  const result: { -readonly [K in keyof BoxNode]: BoxNode[K] } = { ...node };
  const all = props.padding;
  const x = props.paddingX ?? all;
  const y = props.paddingY ?? all;
  const top = props.paddingTop ?? y;
  const bottom = props.paddingBottom ?? y;
  const left = props.paddingLeft ?? x;
  const right = props.paddingRight ?? x;
  if (top !== undefined) result.paddingTop = top;
  if (bottom !== undefined) result.paddingBottom = bottom;
  if (left !== undefined) result.paddingLeft = left;
  if (right !== undefined) result.paddingRight = right;
  if (props.flexDirection !== undefined) result.flexDirection = props.flexDirection;
  if (props.flexGrow !== undefined) result.flexGrow = props.flexGrow;
  if (props.justifyContent !== undefined) result.justifyContent = props.justifyContent;
  if (props.width !== undefined) result.width = props.width;
  if (props.height !== undefined) result.height = props.height;
  if (props.borderStyle !== undefined) result.borderStyle = props.borderStyle;
  if (props.borderTop !== undefined) result.borderTop = props.borderTop;
  if (props.borderBottom !== undefined) result.borderBottom = props.borderBottom;
  if (props.borderLeft !== undefined) result.borderLeft = props.borderLeft;
  if (props.borderRight !== undefined) result.borderRight = props.borderRight;
  if (props.borderColor !== undefined) result.borderColor = props.borderColor;
  if (props.borderTopColor !== undefined) result.borderTopColor = props.borderTopColor;
  if (props.borderBottomColor !== undefined) result.borderBottomColor = props.borderBottomColor;
  if (props.borderLeftColor !== undefined) result.borderLeftColor = props.borderLeftColor;
  if (props.borderRightColor !== undefined) result.borderRightColor = props.borderRightColor;
  return result;
}
