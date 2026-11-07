import { useState, type CSSProperties, type ReactNode } from "react";
import { t } from "../i18n";
import { CopyButton } from "./CopyButton";
import { StudioIcon } from "./StudioIcon";
import { splitLines } from "./codelines";

// Past this many lines a settled block opens folded to FOLDED_LINES, so one long
// file in a reply does not push the answer around it off the screen.
const FOLD_OVER = 18;
export const FOLDED_LINES = 12;

// The file a downloaded block is saved as. A fence names a language, not a file
// type; the ones missing here save as .txt rather than under a guessed suffix.
const EXT: Record<string, string> = {
  go: "go", python: "py", py: "py", javascript: "js", js: "js", typescript: "ts", ts: "ts",
  tsx: "tsx", jsx: "jsx", html: "html", css: "css", json: "json", bash: "sh", sh: "sh",
  shell: "sh", zsh: "sh", powershell: "ps1", ps1: "ps1", sql: "sql", yaml: "yaml", yml: "yaml",
  toml: "toml", markdown: "md", md: "md", rust: "rs", rs: "rs", java: "java", c: "c",
  cpp: "cpp", "c++": "cpp", csharp: "cs", cs: "cs", ruby: "rb", rb: "rb", php: "php",
  xml: "xml", kotlin: "kt", swift: "swift", lua: "lua", dockerfile: "Dockerfile",
};

export function extensionFor(lang: string): string {
  return EXT[lang.toLowerCase()] ?? "txt";
}

function download(source: string, lang: string) {
  const ext = extensionFor(lang);
  const name = ext === "Dockerfile" ? ext : `snippet.${ext}`;
  const url = URL.createObjectURL(new Blob([source], { type: "text/plain;charset=utf-8" }));
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  a.click();
  window.setTimeout(() => URL.revokeObjectURL(url), 0);
}

/** A fenced block in rendered markdown. `live` is a block still arriving, which
 *  never folds: its height changing under the reader is the stream, not a choice. */
export function CodeBlock({ lang, source, live, children }: { lang: string; source: string; live?: boolean; children: ReactNode }) {
  const lines = source.replace(/\n+$/, "").split("\n").length;
  const foldable = !live && lines > FOLD_OVER;
  const [open, setOpen] = useState(false);
  const folded = foldable && !open;
  const rows = splitLines(children);
  while (rows.length > lines && rows[rows.length - 1].length === 0) rows.pop();
  return (
    <div className="code-wrap" data-lang={lang || undefined} data-folded={folded ? "" : undefined}>
      <div className="code-hd">
        <span className="code-lang">{lang || t("chat.code")}</span>
        <span className="code-lines">{t("chat.n_lines", { n: lines })}</span>
        <span className="code-acts">
          {!live && (
            <button type="button" className="code-act" data-action="code.download" title={t("chat.download_as_file")} aria-label={t("chat.download_as_file")} onClick={() => download(source, lang)}>
              <StudioIcon name="download" />
            </button>
          )}
          {!live && <CopyButton text={source} iconOnly className="code-act" label={t("chat.copy_this_code")} />}
        </span>
      </div>
      <div className="code-body" style={{ "--ln-w": `${String(lines).length + 1}ch` } as CSSProperties}>
        <pre className="term">
          {rows.map((row, i) => (
            <span className="code-ln" key={i}>
              <i className="code-no" aria-hidden="true">{i + 1}</i>
              <span className="code-tx">{row.length ? row : "\n"}</span>
            </span>
          ))}
        </pre>
      </div>
      {foldable && (
        <button type="button" className="code-more" data-action="code.fold" aria-expanded={open} onClick={() => setOpen(!open)}>
          <StudioIcon name="chevron" />
          <span>{open ? t("common.collapse") : t("chat.show_all_n_lines", { n: lines })}</span>
        </button>
      )}
    </div>
  );
}
