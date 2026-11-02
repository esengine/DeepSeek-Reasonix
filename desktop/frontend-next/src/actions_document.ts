import type { UIAction } from "./actions";

export const DOCUMENT_ACTIONS: UIAction[] = [
  { id: "document.zoom", kind: "view", target: "entity", proof: "interaction" },
  { id: "document.page", kind: "view", target: "entity", proof: "interaction" },
  { id: "document.sheet", kind: "view", target: "entity", proof: "interaction" },
  { id: "document.scroll", kind: "view", target: "entity", proof: "interaction" },
  { id: "document.refresh", kind: "repeatable", target: "entity", proof: "interaction" },
  { id: "delivery.preview", kind: "navigation", target: "entity", proof: "interaction" },
  { id: "delivery.more", kind: "view", target: "entity", proof: "interaction" },
  { id: "delivery.menu-navigation", kind: "view", target: "none", proof: "interaction" },
  { id: "delivery.add-to-chat", kind: "view", target: "entity", proof: "interaction" },
];
