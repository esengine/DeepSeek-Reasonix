/** Pure model-id → short-label mapping. Tier classification used by header meta. */

export type ModelKind = "flash" | "pro" | "r1" | "unknown";

export interface ModelBadge {
  readonly label: string;
  readonly kind: ModelKind;
}

export function modelBadgeFor(model: string | undefined): ModelBadge {
  if (!model) return { label: "?", kind: "unknown" };
  const stripped = model.replace(/^deepseek-/, "");
  if (stripped === "v4-flash" || stripped === "chat") return { label: "v4-flash", kind: "flash" };
  if (stripped === "v4-pro") return { label: "v4-pro", kind: "pro" };
  if (stripped === "r1" || stripped === "reasoner") return { label: "r1", kind: "r1" };
  return { label: stripped, kind: "unknown" };
}
