import { Fragment, type ReactElement, type ReactNode, isValidElement } from "react";
import { type RenderPools, renderToScreen } from "../layout/layout.js";
import type { BoxNode, LayoutNode, TextNode } from "../layout/node.js";
import type { Screen } from "../screen/screen.js";
import { type BoxProps, HOST_BOX, HOST_TEXT, type TextProps } from "./components.js";

export interface RenderOptions {
  readonly width: number;
  readonly pools: RenderPools;
}

export function render(element: ReactNode, opts: RenderOptions): Screen {
  const node = treeToLayoutNode(element) ?? emptyBox();
  return renderToScreen(node, opts.width, opts.pools);
}

function treeToLayoutNode(element: ReactNode): LayoutNode | null {
  if (element === null || element === undefined || typeof element === "boolean") return null;
  if (typeof element === "string" || typeof element === "number") {
    return { kind: "text", content: String(element) };
  }
  if (Array.isArray(element)) {
    const children = element.map(treeToLayoutNode).filter(notNull);
    return children.length > 0 ? { kind: "box", children } : null;
  }
  if (isValidElement(element)) {
    return elementToLayoutNode(element);
  }
  return null;
}

function elementToLayoutNode(element: ReactElement): LayoutNode | null {
  const type = element.type;
  const props = element.props as { children?: ReactNode };

  if (type === HOST_BOX) {
    const boxProps = element.props as BoxProps;
    const children = childrenToLayoutNodes(props.children);
    const padding = resolvePadding(boxProps);
    const flex: {
      flexDirection?: "column" | "row";
      flexGrow?: number;
      justifyContent?: BoxProps["justifyContent"];
      width?: number;
      height?: number;
    } = {};
    if (boxProps.flexDirection !== undefined) flex.flexDirection = boxProps.flexDirection;
    if (boxProps.flexGrow !== undefined) flex.flexGrow = boxProps.flexGrow;
    if (boxProps.justifyContent !== undefined) flex.justifyContent = boxProps.justifyContent;
    if (boxProps.width !== undefined) flex.width = boxProps.width;
    if (boxProps.height !== undefined) flex.height = boxProps.height;
    const border = resolveBorder(boxProps);
    return { kind: "box", children, ...flex, ...padding, ...border } satisfies BoxNode;
  }

  if (type === HOST_TEXT) {
    const textProps = element.props as TextProps;
    return {
      kind: "text",
      content: collectText(textProps.children),
      ...(textProps.style ? { style: textProps.style } : {}),
      ...(textProps.hyperlink ? { hyperlink: textProps.hyperlink } : {}),
    } satisfies TextNode;
  }

  if (type === Fragment) {
    const children = childrenToLayoutNodes(props.children);
    return children.length === 1 ? children[0]! : { kind: "box", children };
  }

  if (typeof type === "function") {
    const Component = type as (props: unknown) => ReactNode;
    return treeToLayoutNode(Component(props));
  }

  return null;
}

function childrenToLayoutNodes(children: ReactNode): LayoutNode[] {
  if (children === null || children === undefined || typeof children === "boolean") return [];
  if (Array.isArray(children)) {
    return children.map(treeToLayoutNode).filter(notNull);
  }
  const node = treeToLayoutNode(children);
  return node ? [node] : [];
}

function collectText(children: ReactNode): string {
  if (children === null || children === undefined || typeof children === "boolean") return "";
  if (typeof children === "string") return children;
  if (typeof children === "number") return String(children);
  if (Array.isArray(children)) {
    return children.map(collectText).join("");
  }
  if (isValidElement(children)) {
    const inner = (children.props as { children?: ReactNode }).children;
    return collectText(inner);
  }
  return "";
}

function emptyBox(): BoxNode {
  return { kind: "box", children: [] };
}

interface ResolvedPadding {
  paddingTop?: number;
  paddingBottom?: number;
  paddingLeft?: number;
  paddingRight?: number;
}

interface ResolvedBorder {
  borderStyle?: BoxProps["borderStyle"];
  borderTop?: boolean;
  borderBottom?: boolean;
  borderLeft?: boolean;
  borderRight?: boolean;
  borderColor?: BoxProps["borderColor"];
  borderTopColor?: BoxProps["borderTopColor"];
  borderBottomColor?: BoxProps["borderBottomColor"];
  borderLeftColor?: BoxProps["borderLeftColor"];
  borderRightColor?: BoxProps["borderRightColor"];
}

function resolveBorder(props: BoxProps): ResolvedBorder {
  const out: ResolvedBorder = {};
  if (props.borderStyle !== undefined) out.borderStyle = props.borderStyle;
  if (props.borderTop !== undefined) out.borderTop = props.borderTop;
  if (props.borderBottom !== undefined) out.borderBottom = props.borderBottom;
  if (props.borderLeft !== undefined) out.borderLeft = props.borderLeft;
  if (props.borderRight !== undefined) out.borderRight = props.borderRight;
  if (props.borderColor !== undefined) out.borderColor = props.borderColor;
  if (props.borderTopColor !== undefined) out.borderTopColor = props.borderTopColor;
  if (props.borderBottomColor !== undefined) out.borderBottomColor = props.borderBottomColor;
  if (props.borderLeftColor !== undefined) out.borderLeftColor = props.borderLeftColor;
  if (props.borderRightColor !== undefined) out.borderRightColor = props.borderRightColor;
  return out;
}

function resolvePadding(props: BoxProps): ResolvedPadding {
  const all = props.padding;
  const x = props.paddingX ?? all;
  const y = props.paddingY ?? all;
  const top = props.paddingTop ?? y;
  const bottom = props.paddingBottom ?? y;
  const left = props.paddingLeft ?? x;
  const right = props.paddingRight ?? x;
  const out: ResolvedPadding = {};
  if (top !== undefined) out.paddingTop = top;
  if (bottom !== undefined) out.paddingBottom = bottom;
  if (left !== undefined) out.paddingLeft = left;
  if (right !== undefined) out.paddingRight = right;
  return out;
}

function notNull<T>(v: T | null): v is T {
  return v !== null;
}
