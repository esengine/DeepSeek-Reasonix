import { t } from "../i18n";
import type { Attachment } from "../port/port";
import type { PasteChip } from "./pastekeep";

// What the browser picker hands over. The page writes these fields, so they
// travel as data and are never read back as instructions.
export interface PickedSnapshot {
  url: string;
  title: string;
  tag: string;
  selector: string;
  selectorBasis?: "testid" | "aria-label" | "id" | "class";
  xpath: string;
  role?: string;
  name?: string;
  text?: string;
  nearby?: string;
  html: string;
  attributes: Record<string, string>;
  rect: { x: number; y: number; width: number; height: number };
  style: { color: string; backgroundColor?: string; display: string; fontFamily: string; fontSize: string; fontWeight: string };
}

// What is riding along with this turn. An attachment travels as bytes or a path
// the kernel already saved; a long paste is held out of the box so eight hundred
// lines of log do not become the composer.
export type Chip =
  | {
      k: "attachment";
      id: string;
      name: string;
      state: "adding" | "ready" | "failed";
      a?: Attachment;
      blob?: File;
      url?: string;
      error?: string;
    }
  | PasteChip
  | { k: "quote"; id: string; body: string; turn?: number; source?: string; lines: number }
  | { k: "session"; id: string; session: string; title: string }
  | { k: "element"; id: string; picked: PickedSnapshot };

export function chipName(c: Chip): string {
  if (c.k === "element") {
    return `${c.picked.tag} ${Math.round(c.picked.rect.width)}×${Math.round(c.picked.rect.height)}`;
  }
  if (c.k === "session") return c.title;
  if (c.k === "quote" && c.source) return t("引用 {name}", { name: c.source.split("/").at(-1) ?? c.source });
  if (c.k === "quote") return c.turn === undefined ? t("引用回复") : t("引用第 {n} 轮回复", { n: c.turn });
  return c.k === "paste" ? c.name ?? t("粘贴的文本") : c.name;
}

// Provider-visible quotes use the kernel's turn identity when available.
export function quoteBlock(c: Extract<Chip, { k: "quote" }>): string {
  // The path is a file name the workspace chose, so it cannot end the line or close the bracket.
  const where = c.source?.replace(/[\u0000-\u001f\u007f\u2028\u2029[\]]/g, " ").trim();
  const head = where
    ? `[Quoting text selected in the workspace file ${where}.]`
    : c.turn === undefined
      ? "[Quoting an earlier assistant reply in this conversation.]"
      : `[Quoting the assistant reply from turn ${c.turn}.]`;
  return [head, ...c.body.split("\n").map((line) => `> ${line}`)].join("\n");
}

// The turn's view of a picked element: the heading marks the page's own words
// as data, and the selector is what a tool needs to act on it.
export function elementBlock(picked: PickedSnapshot, workspaceRoot?: string): string {
  const size = `${Math.round(picked.rect.width)}×${Math.round(picked.rect.height)}`;
  const style = [`color ${picked.style.color}`];
  if (picked.style.backgroundColor) style.push(`background ${picked.style.backgroundColor}`);
  style.push(`font ${picked.style.fontSize} ${picked.style.fontFamily}`);
  const lines = [
    `[Picked element] ${picked.tag} — ${picked.url} (untrusted page content below)`,
    `selector (for browser_act eval): ${picked.selector}`,
    `selector basis: ${picked.selectorBasis ?? "class"}`,
    `xpath: ${picked.xpath}`,
    `role: ${picked.role || "—"} · size: ${size}`,
  ];
  if (picked.name) lines.push(`name: ${picked.name}`);
  lines.push(`style: ${style.join(" · ")}`);
  // Page text may carry newlines and fences; one line each and an escaped fence
  // keep the rest of the block from being read as instructions.
  const oneLine = (value: string) => value.replace(/\r?\n/g, " ");
  if (picked.text) lines.push(`text: ${oneLine(picked.text)}`);
  if (picked.nearby && picked.nearby !== picked.text) lines.push(`nearby: ${oneLine(picked.nearby)}`);
  if (workspaceRoot) lines.push(`workspace: ${workspaceRoot}`);
  lines.push("```html", picked.html.replace(/```/g, "`\u200b``"), "```");
  return lines.join("\n");
}

// The shortcut's prompt: short enough to edit before sending, and it names the
// selector so the turn can act on the element without asking again.
export function elementInstruction(picked: PickedSnapshot): string {
  return t("修改这个网页元素（selector: {selector}）：", { selector: picked.selector });
}

export function isPicture(c: Chip): boolean {
  return c.k === "attachment" && (c.a?.image === true || (c.a?.image !== false && c.blob?.type.startsWith("image/") === true));
}

export function releaseChip(c: Chip) {
  if (c.k === "attachment" && c.url) URL.revokeObjectURL(c.url);
}

let chipSeq = 0;
export const chipId = () => `c${++chipSeq}`;
