import type { SurfaceImage, SurfaceLocation, SurfaceFact, SurfaceDataset } from "./surface-rich";

// Extension UI surfaces. A sidecar publishes data, never markup — the host
// decides how to draw it, which is why the same extension shows up as a card
// here, as text in the CLI, and as a client-rendered block over ACP.
export interface ExtensionStatus {
  label: string;
  detail?: string;
  severity?: string;
  progress?: number;
}

export interface ExtensionKeyValue {
  key: string;
  value: string;
}

export interface ExtensionActionRef {
  actionId: string;
  label: string;
}

export interface ExtensionCard {
  title?: string;
  markdown?: string;
  text?: string;
  fields?: ExtensionKeyValue[];
  progress?: number;
  actions?: ExtensionActionRef[];
}

export interface ExtensionFormField {
  key: string;
  label?: string;
  kind?: "confirm" | "input" | "select" | "multiselect";
  options?: string[];
  default?: unknown;
  required?: boolean;
}

export interface ExtensionForm {
  title?: string;
  message?: string;
  fields: ExtensionFormField[];
}

// A panel has no markdown on purpose — see the protocol DTO: the side rail is
// a narrow column, and a rendered document there costs more than it tells.
export interface ExtensionPanel {
  title?: string;
  text?: string;
  fields?: ExtensionKeyValue[];
  progress?: number;
  actions?: ExtensionActionRef[];
}

export interface ExtensionNotification {
  title: string;
  body?: string;
  severity?: string;
}

// A view is composed rather than filled in: the extension sends a tree of
// primitives and this side renders them with its own components. Tone says
// what a node means, never what colour it is — the palette stays ours.
export type ExtensionViewTone = "dim" | "strong" | "ok" | "warn" | "err" | "accent";

export interface ExtensionViewNode {
  kind: "text" | "markdown" | "row" | "stack" | "kv" | "meter" | "pip" | "button" | "divider" | "field" | "choice_card" | "editable_text" | "diff_preview" | "map" | "comparison" | "dashboard";
  value?: string;
  key?: string;
  label?: string;
  tone?: ExtensionViewTone;
  progress?: number;
  actionId?: string;
  children?: ExtensionViewNode[];
  field?: SurfaceField;
  diff?: SurfaceDiff;
  dataset?: SurfaceDataset;
}

export interface SurfaceDiff {
  before: string;
  after: string;
}

export interface SurfaceField {
  type: "string" | "number" | "bool" | "choice" | "multichoice";
  required?: boolean;
  default?: string | number | boolean | string[];
  min?: number;
  max?: number;
  max_length?: number;
  options?: SurfaceOption[];
}

export interface SurfaceOption {
  value: string;
  label: string;
  description?: string;
  preview?: string;
  group?: string;
  location?: SurfaceLocation;
  price?: number;
  currency?: string;
  facts?: SurfaceFact[];
  image?: SurfaceImage;
}

export interface ExtensionView {
  // Where the extension would like to stand. A name we do not know renders
  // where we put views we have no place for, rather than not at all.
  slot?: string;
  // "tool:<callId>" when this view replaces a card's body instead of standing
  // on its own. Only tool calls can be anchored — an approval prompt or an
  // error state is not addressable, which is what keeps a takeover from being
  // able to redraw a decision.
  anchor?: string;
  body: ExtensionViewNode[];
}

export interface ExtensionSurface {
  code?: string;
  pluginId: string;
  surfaceId: string;
  sessionId?: string;
  generation?: number;
  kind: "status" | "card" | "form" | "notification" | "panel" | "view" | "frame";
  status?: ExtensionStatus;
  card?: ExtensionCard;
  form?: ExtensionForm;
  notification?: ExtensionNotification;
  panel?: ExtensionPanel;
  view?: ExtensionView;
  frame?: ExtensionFrame;
}

// A document the plugin package ships, drawn in an isolated frame. Props is
// the JSON the document is handed; the page never reads it.
export interface ExtensionFrame {
  frame: string;
  slot?: string;
  title: string;
  fallback: string;
  props?: Record<string, unknown>;
}
