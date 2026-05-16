export type Color =
  | "default"
  | "black"
  | "red"
  | "green"
  | "yellow"
  | "blue"
  | "magenta"
  | "cyan"
  | "white"
  | "gray"
  | { hex: string };

export type TextStyle = {
  color?: Color;
  bg?: Color;
  bold?: boolean;
  dim?: boolean;
  italic?: boolean;
  underline?: boolean;
  inverse?: boolean;
  strikethrough?: boolean;
};

export type TextRun = {
  text: string;
  style?: TextStyle;
};

export type FlexDirection = "row" | "column";
export type FlexAlign = "start" | "center" | "end" | "stretch";
export type FlexJustify = "start" | "center" | "end" | "between" | "around";
export type BorderStyle = "single" | "double" | "round" | "bold";
export type Wrap = "wrap" | "truncate" | "truncate-start" | "truncate-middle" | "none";
export type Dim = number | "fill";

export type BoxLayout = {
  direction?: FlexDirection;
  gap?: number;
  paddingX?: number;
  paddingY?: number;
  marginX?: number;
  marginY?: number;
  width?: Dim;
  height?: Dim;
  flexGrow?: number;
  flexShrink?: number;
  align?: FlexAlign;
  justify?: FlexJustify;
  borderStyle?: BorderStyle;
  borderColor?: Color;
  background?: Color;
};

export type TextNode = {
  kind: "text";
  runs: TextRun[];
  wrap?: Wrap;
};

export type BoxNode = {
  kind: "box";
  layout?: BoxLayout;
  children: SceneNode[];
};

export type SceneNode = TextNode | BoxNode;

export type SceneFrame = {
  schemaVersion: 1;
  cols: number;
  rows: number;
  root: SceneNode;
};
