import { Fragment, type ReactElement, type ReactNode, isValidElement } from "react";
import { type RenderPools, renderToScreen } from "../layout/layout.js";
import type { BoxNode, LayoutNode, TextNode } from "../layout/node.js";
import type { Screen } from "../screen/screen.js";
import { Box, Text, type TextProps } from "./components.js";

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

  if (type === Box) {
    const children = childrenToLayoutNodes(props.children);
    return { kind: "box", children } satisfies BoxNode;
  }

  if (type === Text) {
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

function notNull<T>(v: T | null): v is T {
  return v !== null;
}
