import type { PreviewInfo } from "../../port/workspace";

type Kind = PreviewInfo["kind"];
export const renderers: ReadonlyArray<{ kind: Kind; extensions: RegExp; source: boolean }> = [
  { kind: "markdown", extensions: /\.(md|markdown|mdx)$/i, source: true },
  { kind: "image", extensions: /\.(png|jpe?g|gif|webp|avif|bmp|ico|svg)$/i, source: false },
  { kind: "pdf", extensions: /\.pdf$/i, source: false },
  { kind: "table", extensions: /\.(csv|tsv)$/i, source: true },
  { kind: "html", extensions: /\.html?$/i, source: true },
  { kind: "office", extensions: /\.(docx|xlsx)$/i, source: false },
  { kind: "unsupported", extensions: /\.(doc|xls|pptx?|zip|gz|7z|rar|exe|dll|bin)$/i, source: false },
];

export function previewKind(path: string): Kind {
  return renderers.find((r) => r.extensions.test(path))?.kind ?? "text";
}

export const documentView = (path: string) => !["markdown", "text"].includes(previewKind(path));
export const sourceView = (path: string) => renderers.find((r) => r.extensions.test(path))?.source ?? true;
