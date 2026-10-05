import { useState, type ReactNode } from "react";
import { t } from "../i18n";
import { CopyButton } from "./CopyButton";
import { StudioIcon } from "./StudioIcon";

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
  return (
    <div className="code-wrap" data-lang={lang || undefined} data-folded={folded ? "" : undefined}>
      <div className="code-hd">
        <span className="code-lang">{lang || t("代码")}</span>
        <span className="code-lines">{t("{n} 行", { n: lines })}</span>
        <span className="code-acts">
          {!live && (
            <button type="button" className="code-act" data-action="code.download" title={t("下载为文件")} aria-label={t("下载为文件")} onClick={() => download(source, lang)}>
              <StudioIcon name="download" />
            </button>
          )}
          {!live && <CopyButton text={source} iconOnly className="code-act" label={t("复制这段代码")} />}
        </span>
      </div>
      <div className="code-body">
        <div className="code-gutter" aria-hidden="true">
          {Array.from({ length: lines }, (_, i) => <span key={i}>{i + 1}</span>)}
        </div>
        <pre className="term">{children}</pre>
      </div>
      {foldable && (
        <button type="button" className="code-more" data-action="code.fold" aria-expanded={open} onClick={() => setOpen(!open)}>
          <StudioIcon name="chevron" />
          <span>{open ? t("收起") : t("展开全部 {n} 行", { n: lines })}</span>
        </button>
      )}
    </div>
  );
}
