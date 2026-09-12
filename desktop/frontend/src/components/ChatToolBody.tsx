import { useEffect, useRef, useState } from "react";
import type { ChatNode } from "../lib/chatViewSource";
import type { ChatContentLoader } from "../lib/chatContentLoader";
import { useT } from "../lib/i18n";
import { diffsFor, subjectOf } from "../lib/tools";
import { TerminalBlock } from "./harness-chat/TerminalBlock";
import { DiffBlock } from "./harness-chat/DiffBlock";
import { WebBlock } from "./harness-chat/WebBlock";
import { normalizeSearchSources } from "../lib/searchSourcesPresentation";
import { parseSearchSources, searchOutputMetadata } from "../lib/searchSources";
import toolCss from "./harness-chat/ToolRow.styles";
import { classifyTool } from "../lib/chatToolPresentation";
import "./harness-chat/TerminalBlock.css";
import "./harness-chat/DiffBlock.css";
import "./harness-chat/Pill.css";
import "./harness-chat/ToolBody.css";
import "./harness-chat/WebBlock.css";

export default function ChatToolBody({ item, loader }: { item: Extract<ChatNode, { kind: "tool" }>["item"]; loader: ChatContentLoader }) {
  const t = useT();
  const [loaded, setLoaded] = useState<{ item: typeof item; text: string }>();
  const [pendingItem, setPendingItem] = useState<typeof item>();
  const [errorItem, setErrorItem] = useState<typeof item>();
  const busy = pendingItem === item;
  const error = errorItem === item;
  const epoch = useRef(0);
  useEffect(() => { return () => { epoch.current++; }; }, [item]);
  const full = loaded?.item === item ? loaded.text : undefined;
  const preview = JSON.stringify({ args: item.args, output: item.output, error: item.error, diff: item.fileDiff });
  const limited = full === undefined && (loader.needsFullContent(item, "tool") || preview.length > 8000);
  const load = async () => {
    const ticket = ++epoch.current; setPendingItem(item); setErrorItem(undefined);
    try {
      const text = await loader.load(item, "tool");
      if (ticket !== epoch.current) throw new Error("Tool changed or closed");
      const payload: unknown = JSON.parse(text);
      if (!payload || typeof payload !== "object" || Array.isArray(payload)) throw new Error("Invalid tool content");
      for (const key of ["args", "output", "error"] as const) {
        if (key in payload && (payload as Record<string, unknown>)[key] != null && typeof (payload as Record<string, unknown>)[key] !== "string") throw new Error("Invalid tool content");
      }
      setLoaded({ item, text });
    } catch { if (ticket === epoch.current) setErrorItem(item); }
    finally { if (ticket === epoch.current) setPendingItem(undefined); }
  };
  let value: { args?: string; output?: string; error?: string } = item;
  if (full !== undefined) { try { value = JSON.parse(full); } catch { /* Keep the available preview. */ } }
  let args: Record<string, unknown> = {};
  try { args = JSON.parse(value.args || "{}") || {}; } catch { /* Streaming arguments. */ }
  const labels = { copy: t("msg.copy"), copied: t("msg.copied"), collapse: t("common.collapse"), collapseAria: t("common.collapse"),
    expand: (hidden: number) => t("chat.expandLines", { count: hidden }), expandAria: (hidden: number) => t("chat.expandLines", { count: hidden }) };
  const diffs = !limited ? diffsFor(item.name, value.args || "{}") : [];
  const kind = classifyTool(item);
  const search = kind === "search" && !limited ? normalizeSearchSources(item.searchSources ?? parseSearchSources(value.output || "")) : undefined;
  const searchMeta = searchOutputMetadata(value.output);
  return <>
    {!limited && kind === "shell" ? <TerminalBlock command={typeof args.command === "string" ? args.command : value.args || ""}
      output={value.error || value.output} running={item.status === "running"} exitCode={item.execution?.exitCode}
      signal={item.status === "error" && item.execution?.exitCode == null ? t("chat.failed") : item.status === "stopped" ? t("chat.stopped") : undefined}
      maxLines={200} className={toolCss.terminalBody}
      labels={{ ...labels, signal: signal => signal, exitCode: code => `${code}`, running: t("chat.running"), failed: t("chat.failed"), done: t("chat.done"), noOutput: t("chat.noOutput") }} />
      : search && item.status === "done" ? <WebBlock kind="search" answer={searchMeta.summary ?? item.searchSummary}
        sources={search.visible.map(source => ({ url: source.href, title: source.title }))} truncated={search.hiddenCount > 0}
        labels={{ noResults: t("sources.notProvided"), sourcesTruncated: t("sources.hidden", { n: search.hiddenCount }), http: "HTTP", contentTruncated: t("chat.loadFull") }} className={toolCss.webBody} />
      : diffs.length ? <DiffBlock diffs={diffs.map(diff => ({ path: subjectOf(item.name, value.args || "{}"), oldText: diff.original, newText: diff.modified }))}
        maxLines={200} labels={{ ...labels, files: count => t("chat.files", { count }) }} className={toolCss.diffBody} />
        : <div className={toolCss.ioCard}><ToolPayload text={full ?? preview} preview={limited} /></div>}
    {limited && <button className="btn" disabled={busy} onClick={() => void load()}>{t(error ? "chat.loadFailed" : busy ? "chat.loading" : "chat.loadFull")}</button>}
  </>;
}

export function ToolPayload({ text, preview }: { text: string; preview: boolean }) {
  const t = useT();
  let value: unknown;
  try { value = JSON.parse(text); } catch { return <pre>{preview ? text.slice(0, 8000) : text}</pre>; }
  if (!value || typeof value !== "object" || Array.isArray(value)) return <pre>{preview ? text.slice(0, 8000) : text}</pre>;
  let remaining = preview ? 8000 : Infinity;
  return <>{Object.entries(value).map(([key, content]) => {
    if (content == null || remaining <= 0) return null;
    const body = typeof content === "string" ? content : JSON.stringify(content, null, 2);
    const shown = body.slice(0, remaining); remaining -= shown.length;
    const label = key === "args" ? t("chat.tool.input") : key === "output" ? t("chat.tool.output") : key;
    return <section key={key} className={toolCss.ioSection}><span className={toolCss.ioLabel}>{label}</span><pre className={toolCss.ioText} data-error={key === "error" || undefined}>{shown}</pre></section>;
  })}</>;
}
