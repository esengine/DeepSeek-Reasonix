import { useEffect, useState } from "react";
import { t } from "../i18n";
import { CopyButton } from "./CopyButton";
import "./mermaid.css";

type Rendered = { source: string; theme: string } & ({ svg: string; error?: never } | { error: string; svg?: never });

/** A streaming fence stays source text until its block settles. */
export function MermaidDiagram({ source, live }: { source: string; live?: boolean }) {
  const [mode, setMode] = useState<"preview" | "source">("preview");
  const [theme, setTheme] = useState<"light" | "dark">(() => document.documentElement.dataset.theme === "dark" ? "dark" : "light");
  const [rendered, setRendered] = useState<Rendered | null>(null);
  useEffect(() => {
    const watch = new MutationObserver(() => setTheme(document.documentElement.dataset.theme === "dark" ? "dark" : "light"));
    watch.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });
    return () => watch.disconnect();
  }, []);
  useEffect(() => {
    if (live) return;
    let alive = true;
    // Ordinary Markdown and unsettled tails never need the renderer module or iframe.
    void import("./mermaid").then((engine) => engine.renderMermaid(source, theme)).then(
      (svg) => { if (alive) setRendered({ source, theme, svg }); },
      (error: unknown) => { if (alive) setRendered({ source, theme, error: error instanceof Error ? error.message : String(error) }); },
    );
    return () => { alive = false; };
  }, [source, theme, live]);
  const current = !live && rendered?.source === source && rendered.theme === theme ? rendered : null;
  const showSource = live || mode === "source" || !!current?.error;
  return (
    <section className="mermaid-wrap" aria-label={t("Mermaid 图表")}>
      <div className="code-hd">
        <span className="code-lang">mermaid</span>
        <span className="code-acts">
          <button type="button" className="mermaid-mode" data-action="mermaid.preview" aria-pressed={!showSource} onClick={() => setMode("preview")}>{t("预览")}</button>
          <button type="button" className="mermaid-mode" data-action="mermaid.source" aria-pressed={!!showSource} onClick={() => setMode("source")}>{t("源代码")}</button>
          {!live && <CopyButton text={source} iconOnly className="code-act" label={t("复制 Mermaid 源代码")} />}
        </span>
      </div>
      {current?.error && <p className="mermaid-error" role="alert">{t("Mermaid 渲染失败")}: {current.error}</p>}
      {showSource ? <pre className="mermaid-source"><code>{source}</code></pre> : current?.svg ? (
        <div className="mermaid-preview"><img alt={t("Mermaid 图表")} src={`data:image/svg+xml;charset=utf-8,${encodeURIComponent(current.svg)}`} /></div>
      ) : <p className="mermaid-loading" role="status">{t("正在渲染 Mermaid 图表…")}</p>}
    </section>
  );
}
