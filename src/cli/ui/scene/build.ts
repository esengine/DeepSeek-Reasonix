import type {
  BoxLayout,
  BoxNode,
  SceneFrame,
  SceneNode,
  TextNode,
  TextRun,
  TextStyle,
  Wrap,
} from "./types.js";

export function run(text: string, style?: TextStyle): TextRun {
  return style ? { text, style } : { text };
}

export function text(content: string | TextRun[], wrap?: Wrap): TextNode {
  const runs = typeof content === "string" ? [{ text: content }] : content;
  return wrap ? { kind: "text", runs, wrap } : { kind: "text", runs };
}

export function box(children: SceneNode[], layout?: BoxLayout): BoxNode {
  return layout ? { kind: "box", layout, children } : { kind: "box", children };
}

export function frame(cols: number, rows: number, root: SceneNode): SceneFrame {
  return { schemaVersion: 1, cols, rows, root };
}
