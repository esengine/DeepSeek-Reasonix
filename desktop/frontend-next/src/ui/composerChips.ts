import { t } from "../i18n";
import type { Attachment } from "../port/port";
import type { PasteChip } from "./pastekeep";

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
  | { k: "session"; id: string; session: string; title: string };

export function chipName(c: Chip): string {
  if (c.k === "session") return c.title;
  if (c.k === "quote" && c.source) return t("composer.quote_name", { name: c.source.split("/").at(-1) ?? c.source });
  if (c.k === "quote") return c.turn === undefined ? t("composer.quoting_a_reply") : t("composer.quoting_turn_n", { n: c.turn });
  return c.k === "paste" ? c.name ?? t("composer.pasted_text") : c.name;
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

export function isPicture(c: Chip): boolean {
  return c.k === "attachment" && (c.a?.image === true || (c.a?.image !== false && c.blob?.type.startsWith("image/") === true));
}

export function releaseChip(c: Chip) {
  if (c.k === "attachment" && c.url) URL.revokeObjectURL(c.url);
}

let chipSeq = 0;
export const chipId = () => `c${++chipSeq}`;
