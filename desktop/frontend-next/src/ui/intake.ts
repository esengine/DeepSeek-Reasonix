// intake.ts — what the composer does with something dropped or pasted on it.
//
// The whole decision is one pure function: payload in, action out. "What
// happens if I drop a folder" is then a test rather than the third branch of an
// onDrop handler. The rule it encodes: this agent reads files itself, so a
// dropped source file becomes a reference it can open at its own budget, not a
// copy of the bytes it must swallow whole. Only pixels have to be carried.

import { t } from "../i18n";

export type IntakeAction = "attach" | "ref" | "insert" | "reject";

// One thing being dropped, described as much as the platform will say. The
// desktop host reports a path, a browser tab reports bytes and never a path,
// and a drag still in the air reports neither — pending marks that third case,
// where the kind is known and the payload only arrives on release.
export interface IntakeItem {
  kind: "file" | "dir" | "text";
  mime?: string;
  path?: string;
  name?: string;
  text?: string;
  blob?: Blob;
  pending?: boolean;
}

export interface IntakePlan {
  // Files whose bytes have to travel, in the order they were dropped.
  attach: IntakeItem[];
  // Workspace-relative paths, ready to become @refs.
  ref: string[];
  // References the drop will produce once the host names them. A preview knows
  // how many files are coming and not yet what they are called.
  pendingRef: number;
  // Text to put at the caret.
  insert: string;
  // Dropped things nothing can be done with — a file outside the workspace
  // cannot be referenced, and copying it in is not what the drop asked for.
  reject: number;
}

const SEP = /[\\/]/;

export function actionFor(item: IntakeItem, root?: string): IntakeAction {
  if (item.kind === "text") return item.text ? "insert" : "reject";
  if (isImage(item)) return "attach";
  // Still in the air: the kind is all a dragover reports, and a file that is
  // not an image is one this agent would rather be handed a name for.
  if (item.pending) return "ref";
  if (relativize(item.path, root) !== null) return "ref";
  // No path to point at. Bytes in hand are still worth carrying; nothing in
  // hand is a drop there is no honest answer for.
  if (item.kind === "file" && item.blob) return "attach";
  return "reject";
}

function isImage(item: IntakeItem): boolean {
  return item.kind === "file" && (item.mime ?? item.blob?.type ?? "").startsWith("image/");
}

// relativize turns an absolute path into the workspace-relative form an @ref
// uses, or null when it is outside — the reference would not resolve, and a
// reference that does not resolve is worse than refusing the drop.
export function relativize(path?: string, root?: string): string | null {
  const p = normalize(path);
  const r = normalize(root);
  if (!p) return null;
  if (!r) return null;
  const base = r.endsWith("/") ? r : r + "/";
  if (p === r) return ".";
  if (!p.toLowerCase().startsWith(base.toLowerCase())) return null;
  return p.slice(base.length) || ".";
}

function normalize(value?: string): string {
  return (value ?? "").trim().replace(/[\\]/g, "/").replace(/\/+$/, "");
}

export function planIntake(items: IntakeItem[], root?: string): IntakePlan {
  const plan: IntakePlan = { attach: [], ref: [], pendingRef: 0, insert: "", reject: 0 };
  const text: string[] = [];
  for (const item of items) {
    switch (actionFor(item, root)) {
      case "attach":
        plan.attach.push(item);
        break;
      case "ref": {
        // A drag still in the air has no path to resolve yet. It is counted, not
        // dropped: a preview that resolved nothing used to come out empty, so a
        // file that was about to become a reference showed no hint at all.
        const rel = relativize(item.path, root);
        if (!rel) {
          plan.pendingRef++;
          break;
        }
        plan.ref.push(item.kind === "dir" && !rel.endsWith("/") ? rel + "/" : rel);
        break;
      }
      case "insert":
        text.push(item.text ?? "");
        break;
      default:
        plan.reject++;
    }
  }
  plan.insert = text.join("\n");
  return plan;
}

export function planIsEmpty(plan: IntakePlan): boolean {
  return (
    plan.attach.length === 0 &&
    plan.ref.length === 0 &&
    plan.pendingRef === 0 &&
    plan.insert === "" &&
    plan.reject === 0
  );
}

function refCount(plan: IntakePlan): number {
  return plan.ref.length + plan.pendingRef;
}

// tone is which of the three actions the drop leads with, so the composer can
// colour itself by what it is about to do rather than by "a drag is happening".
export function planTone(plan: IntakePlan): IntakeAction {
  if (plan.attach.length > 0) return "attach";
  if (refCount(plan) > 0) return "ref";
  if (plan.insert !== "") return "insert";
  return "reject";
}

// planVerb says what letting go will do, before it happens. A drop that only
// reports afterwards is the pattern this replaces.
export function planVerb(plan: IntakePlan): string {
  const images = plan.attach.length;
  const refs = refCount(plan);
  if (images > 0 && refs > 0) return t("松手 → 附上 {a} 张图，引用 {r} 个文件", { a: images, r: refs });
  if (images > 0) return t("松手 → 附上 {n} 张图", { n: images });
  if (refs === 1 && plan.ref.length === 1) return t("松手 → 引用 {name}", { name: basename(plan.ref[0]) });
  if (refs === 1) return t("松手 → 引用 1 个文件");
  if (refs > 1) return t("松手 → 引用 {n} 个文件", { n: refs });
  if (plan.insert !== "") return t("松手 → 插入到光标处");
  return t("工作区外的文件引用不到 · 这里放不下");
}

function basename(path: string): string {
  const parts = path.replace(/\/$/, "").split(SEP);
  return parts[parts.length - 1] || path;
}

// refToken is the same token the @ completion produces, so a dropped file and a
// completed one leave the composer identical.
export function refToken(rel: string): string {
  return "@" + rel;
}

// A paste long enough to bury the composer becomes a chip instead. Lines and
// characters both count, whichever comes first: eight hundred lines of narrow
// log is under the character cap and has already eaten the window.
export const PASTE_CHARS = 4000;
export const PASTE_LINES = 80;

export function pasteIsLong(text: string): boolean {
  return text.length > PASTE_CHARS || countLines(text) > PASTE_LINES;
}

export function countLines(text: string): number {
  if (text === "") return 0;
  return text.split("\n").length;
}
