import { t } from "../i18n";
import type { Attachment, ChipCall } from "../port/port";
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

export function isPicture(c: Chip): boolean {
  return c.k === "attachment" && (c.a?.image === true || (c.a?.image !== false && c.blob?.type.startsWith("image/") === true));
}

export function releaseChip(c: Chip) {
  if (c.k === "attachment" && c.url) URL.revokeObjectURL(c.url);
}

let chipSeq = 0;
export const chipId = () => `c${++chipSeq}`;

// lineCall names the two readings of one outgoing line: what the model is
// given, and what the transcript draws back. They differ only when the line
// carries host tokens the person never typed, and then only the drawn one.
export function lineCall(line: string, shown: string, call?: ChipCall): ChipCall | undefined {
  if (!call && shown === line) return undefined;
  return { submit: call?.submit ?? line, invocations: call?.invocations ?? [], ...(shown === line ? {} : { display: shown }) };
}
