import { Box, Text } from "ink";
import { type ReactElement, type ReactNode, isValidElement } from "react";
import type {
  BoxLayout,
  BoxNode,
  Color,
  Dim,
  SceneNode,
  TextNode,
  TextRun,
  TextStyle,
  Wrap,
} from "./types.js";

type AnyProps = Record<string, unknown>;

export function lowerInkToScene(element: ReactElement): SceneNode {
  if (element.type === Text) return lowerText(element);
  if (element.type === Box) return lowerBox(element);
  const name =
    typeof element.type === "function" ? element.type.name || "anonymous" : String(element.type);
  throw new Error(`lowerInkToScene: only Ink Text/Box supported, got ${name}`);
}

function lowerText(el: ReactElement): TextNode {
  const props = el.props as AnyProps;
  const style = textStyleFrom(props);
  const wrap = mapWrap(props.wrap);
  const runs: TextRun[] = [];
  collectRuns(props.children as ReactNode, style, runs);
  return wrap ? { kind: "text", runs, wrap } : { kind: "text", runs };
}

function collectRuns(node: ReactNode, inherited: TextStyle | undefined, out: TextRun[]): void {
  if (node === null || node === undefined || node === false || node === true) return;
  if (typeof node === "string" || typeof node === "number") {
    const text = String(node);
    out.push(inherited ? { text, style: inherited } : { text });
    return;
  }
  if (Array.isArray(node)) {
    for (const c of node) collectRuns(c, inherited, out);
    return;
  }
  if (isValidElement(node) && node.type === Text) {
    const own = textStyleFrom(node.props as AnyProps);
    const merged = mergeStyle(inherited, own);
    collectRuns((node.props as AnyProps).children as ReactNode, merged, out);
    return;
  }
  throw new Error("lowerInkToScene: Text children must be strings, numbers, or nested Text");
}

function textStyleFrom(props: AnyProps): TextStyle | undefined {
  const style: TextStyle = {};
  for (const [key, value] of Object.entries(props)) {
    if (key === "children" || key === "wrap" || value === undefined) continue;
    switch (key) {
      case "color":
        style.color = asColor(value);
        break;
      case "backgroundColor":
        style.bg = asColor(value);
        break;
      case "dimColor":
        if (value) style.dim = true;
        break;
      case "bold":
        if (value) style.bold = true;
        break;
      case "italic":
        if (value) style.italic = true;
        break;
      case "underline":
        if (value) style.underline = true;
        break;
      case "strikethrough":
        if (value) style.strikethrough = true;
        break;
      case "inverse":
        if (value) style.inverse = true;
        break;
      default:
        throw new Error(`lowerInkToScene: unsupported Text prop "${key}"`);
    }
  }
  return hasAnyKey(style) ? style : undefined;
}

const NAMED_COLORS: ReadonlySet<string> = new Set([
  "default",
  "black",
  "red",
  "green",
  "yellow",
  "blue",
  "magenta",
  "cyan",
  "white",
  "gray",
]);

function asColor(value: unknown): Color {
  if (typeof value !== "string") throw new Error("lowerInkToScene: color must be a string");
  if (/^#[0-9a-fA-F]{6}$/.test(value)) return { hex: value };
  if (NAMED_COLORS.has(value)) return value as Color;
  throw new Error(`lowerInkToScene: unsupported color "${value}"`);
}

function mergeStyle(
  parent: TextStyle | undefined,
  own: TextStyle | undefined,
): TextStyle | undefined {
  if (!parent) return own;
  if (!own) return parent;
  return { ...parent, ...own };
}

function hasAnyKey(obj: object): boolean {
  for (const _ in obj) return true;
  return false;
}

function mapWrap(value: unknown): Wrap | undefined {
  if (value === undefined) return undefined;
  switch (value) {
    case "wrap":
      return "wrap";
    case "end":
    case "truncate":
    case "truncate-end":
      return "truncate";
    case "middle":
    case "truncate-middle":
      return "truncate-middle";
    case "truncate-start":
      return "truncate-start";
    default:
      throw new Error(`lowerInkToScene: unsupported wrap "${String(value)}"`);
  }
}

function lowerBox(el: ReactElement): BoxNode {
  const props = el.props as AnyProps;
  const layout = boxLayoutFrom(props);
  const children: SceneNode[] = [];
  collectBoxChildren(props.children as ReactNode, children);
  return layout ? { kind: "box", layout, children } : { kind: "box", children };
}

function collectBoxChildren(node: ReactNode, out: SceneNode[]): void {
  if (node === null || node === undefined || node === false || node === true) return;
  if (typeof node === "string" || typeof node === "number") {
    out.push({ kind: "text", runs: [{ text: String(node) }] });
    return;
  }
  if (Array.isArray(node)) {
    for (const c of node) collectBoxChildren(c, out);
    return;
  }
  if (isValidElement(node)) {
    out.push(lowerInkToScene(node));
    return;
  }
  throw new Error("lowerInkToScene: Box children must be strings, numbers, Text, or Box");
}

function boxLayoutFrom(props: AnyProps): BoxLayout | undefined {
  const out: BoxLayout = {};
  for (const [key, value] of Object.entries(props)) {
    if (key === "children" || value === undefined) continue;
    switch (key) {
      case "flexDirection":
        if (value !== "row" && value !== "column")
          throw new Error(`lowerInkToScene: unsupported flexDirection "${String(value)}"`);
        out.direction = value;
        break;
      case "gap":
        out.gap = asInt(value, "gap");
        break;
      case "padding": {
        const n = asInt(value, "padding");
        out.paddingX = n;
        out.paddingY = n;
        break;
      }
      case "paddingX":
        out.paddingX = asInt(value, "paddingX");
        break;
      case "paddingY":
        out.paddingY = asInt(value, "paddingY");
        break;
      case "margin": {
        const n = asInt(value, "margin");
        out.marginX = n;
        out.marginY = n;
        break;
      }
      case "marginX":
        out.marginX = asInt(value, "marginX");
        break;
      case "marginY":
        out.marginY = asInt(value, "marginY");
        break;
      case "width":
        out.width = asDim(value, "width");
        break;
      case "height":
        out.height = asDim(value, "height");
        break;
      case "flexGrow":
        out.flexGrow = asInt(value, "flexGrow");
        break;
      case "flexShrink":
        out.flexShrink = asInt(value, "flexShrink");
        break;
      case "alignItems":
        out.align = mapAlign(value);
        break;
      case "justifyContent":
        out.justify = mapJustify(value);
        break;
      case "borderStyle":
        out.borderStyle = mapBorder(value);
        break;
      case "borderColor":
        out.borderColor = asColor(value);
        break;
      default:
        throw new Error(`lowerInkToScene: unsupported Box prop "${key}"`);
    }
  }
  return hasAnyKey(out) ? out : undefined;
}

function asInt(value: unknown, name: string): number {
  if (typeof value !== "number" || !Number.isInteger(value)) {
    throw new Error(`lowerInkToScene: ${name} must be an integer, got ${typeof value}`);
  }
  return value;
}

function asDim(value: unknown, name: string): Dim {
  if (typeof value === "number" && Number.isInteger(value)) return value;
  if (value === "100%") return "fill";
  throw new Error(`lowerInkToScene: ${name} must be an integer or "100%", got ${String(value)}`);
}

function mapAlign(value: unknown): BoxLayout["align"] {
  switch (value) {
    case "flex-start":
      return "start";
    case "flex-end":
      return "end";
    case "center":
      return "center";
    case "stretch":
      return "stretch";
    default:
      throw new Error(`lowerInkToScene: unsupported alignItems "${String(value)}"`);
  }
}

function mapJustify(value: unknown): BoxLayout["justify"] {
  switch (value) {
    case "flex-start":
      return "start";
    case "flex-end":
      return "end";
    case "center":
      return "center";
    case "space-between":
      return "between";
    case "space-around":
      return "around";
    default:
      throw new Error(`lowerInkToScene: unsupported justifyContent "${String(value)}"`);
  }
}

function mapBorder(value: unknown): BoxLayout["borderStyle"] {
  if (value === "single" || value === "double" || value === "round" || value === "bold")
    return value;
  throw new Error(`lowerInkToScene: unsupported borderStyle "${String(value)}"`);
}
