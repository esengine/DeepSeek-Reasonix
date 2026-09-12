import { lazy, Suspense, memo, useCallback, useEffect, useId, useRef, useState, useSyncExternalStore } from "react";
import { FileText, Globe, GitBranch, PackageOpen, Search, Terminal, Users, Wrench, X } from "lucide-react";
import { ChatSource, type ChatNode } from "../lib/chatViewSource";
import type { ChatContentLoader } from "../lib/chatContentLoader";
import type { ChatScrollController } from "../lib/chatScrollController";
import type { CheckpointMeta } from "../lib/types";
import { useT } from "../lib/i18n";
import { AssistantMessage, UserMessage } from "./Message";
import { CopyButton } from "./CopyButton";
import { Markdown } from "./Markdown";
import { ExtensionCard } from "./ExtensionCard";
import { Tooltip } from "./Tooltip";
import { formatMessageClock, TurnTimePanel, TurnUsagePanel } from "./TurnStats";
import { ReasoningRow } from "./harness-chat/ReasoningRow";
import { TurnProcessNodeView } from "./harness-chat/TurnProcessNodeView";
import { ContextInjectionRow } from "./harness-chat/ContextInjectionRow";
import { ToolRow } from "./harness-chat/ToolRow";
import { subjectOf, summarizeFileDiff } from "../lib/tools";
import { classifyTool, shellDisplayName } from "../lib/chatToolPresentation";
const ChatToolBody = lazy(() => import("./ChatToolBody"));
const ToolPayload = lazy(() => import("./ChatToolBody").then(module => ({ default: module.ToolPayload })));
const PresentedFiles = lazy(() => import("./PresentedFiles").then(module => ({ default: module.PresentedFiles })));
const ModifiedFiles = lazy(() => import("./PresentedFiles").then(module => ({ default: module.ModifiedFiles })));

export function useChatNode(source: ChatSource, key: string) {
  const subscribe = useCallback((listener: () => void) => source.subscribeNode(key, listener), [source, key]);
  const snapshot = useCallback(() => source.getNodeSnapshot(key), [source, key]);
  return useSyncExternalStore(subscribe, snapshot, snapshot);
}
export type ChatActions = {
  openDetails: (key: string, trigger: HTMLElement) => void;
  fork?: (turn: number) => void;
  recover: (id: string) => void;
  forkDisabled: boolean;
  checkpoints: readonly CheckpointMeta[];
};
type SeatProps = { source: ChatSource; nodeKey: string; loader: ChatContentLoader; scroll: ChatScrollController; actions: ChatActions; tabId?: string; hostId?: string };
const HISTORY_PREPEND_CHUNK = 96;

export const ChatNodeList = memo(function ChatNodeList(props: Omit<SeatProps, "nodeKey">) {
  const order = useSyncExternalStore(props.source.subscribeOrder, props.source.getOrderSnapshot, props.source.getOrderSnapshot);
  const [visibleOrder, setVisibleOrder] = useState(order);
  const visibleRef = useRef(visibleOrder);
  const prependStart = visibleOrder.length ? order.indexOf(visibleOrder[0]) : -1;
  const progressivePrepend = prependStart > 0
    && visibleOrder.every((key, index) => order[prependStart + index] === key);
  // Initial hydration, replacement and tail growth remain synchronous. Only a
  // leading history addition is eligible for the bounded reveal below.
  const renderedOrder = progressivePrepend ? visibleOrder : order;
  visibleRef.current = renderedOrder;
  useEffect(() => {
    let frame = requestAnimationFrame(function revealPrepend() {
      const current = visibleRef.current;
      if (!current.length) { setVisibleOrder(order); return; }
      const start = order.indexOf(current[0]);
      const contiguous = start >= 0 && current.every((key, index) => order[start + index] === key);
      if (!contiguous) { setVisibleOrder(order); return; }
      // A history page still becomes one natural-flow document, but committing
      // its leading nodes over adjacent frames prevents a large page from
      // monopolising the main thread. Existing rows never unmount.
      const chunkStart = Math.max(0, start - HISTORY_PREPEND_CHUNK);
      const suffix = order.slice(start + current.length);
      if (chunkStart === start && !suffix.length) return;
      const next = [...order.slice(chunkStart, start), ...current, ...suffix];
      visibleRef.current = next;
      setVisibleOrder(next);
      if (chunkStart > 0) frame = requestAnimationFrame(revealPrepend);
    });
    return () => cancelAnimationFrame(frame);
  }, [order]);
  return renderedOrder.map(key => <ChatNodeSeat key={key} {...props} nodeKey={key} />);
});

const ChatNodeSeat = memo(function ChatNodeSeat({ source, nodeKey, loader, scroll, actions, tabId, hostId }: SeatProps) {
  const node = useChatNode(source, nodeKey);
  const process = useChatNode(source, `${node?.turnKey}:process`);
  const t = useT();
  if (!node) return null;
  const hidden = process?.kind === "process" && process.collapsed && process.members.includes(nodeKey);
  if (hidden) return null;
  let body;
  switch (node.kind) {
    case "user": body = <ChatUser node={node} loader={loader} />; break;
    case "assistant": body = node.item.text || node.item.searchSources?.length || node.item.memoryCitations?.length ? <ChatAnswer node={node} loader={loader} source={source} tabId={tabId} hostId={hostId} /> : null; break;
    case "reasoning": body = node.item.reasoning ? <ChatReasoning node={node} loader={loader} source={source} scroll={scroll} /> : null; break;
    case "process": body = <TurnProcessNodeView node={node} onToggle={() => { scroll.beforeChange(); source.toggleProcess(node.turnKey); }} />; break;
    case "tool": body = <ChatTool node={node} loader={loader} actions={actions} scroll={scroll} />; break;
    case "phase": body = <ContextInjectionRow title={t("chat.activity")} summary={node.item.text} beforeToggle={scroll.beforeChange}>{node.item.text}</ContextInjectionRow>; break;
    case "notice": body = <ChatNotice node={node} actions={actions} scroll={scroll} />; break;
    case "compaction": body = <ChatDisclosure label={t("chat.compaction")}><Markdown text={node.item.summary} /></ChatDisclosure>; break;
    case "extension": body = node.item.card.actions?.length ? <ExtensionCard item={node.item} tabId={tabId} /> :
      <ChatDisclosure label={node.item.card.title || node.item.pluginId}><ExtensionCard item={node.item} tabId={tabId} /></ChatDisclosure>; break;
    case "tail": body = <ChatTurnTail node={node} source={source} actions={actions} loader={loader} tabId={tabId} hostId={hostId} />; break;
  }
  return <div className="chat-node" data-chat-anchor-key={node.key} data-chat-turn={node.turnKey} data-chat-kind={node.kind}
    data-turn-process-answer={node.kind === "assistant" && process?.kind === "process" && process.collapsed && !process.members.includes(node.key) || undefined}>{body}</div>;
});

function ChatNotice({ node, actions, scroll }: { node: Extract<ChatNode, { kind: "notice" }>; actions: ChatActions; scroll: ChatScrollController }) {
  const t = useT();
  const item = node.item;
  const summary = item.completionSummary;
  // Empty delivery accounting is not a chat result. Keep meaningful records in details.
  if (summary && !summary.mutations && !summary.changed_files && !summary.checks_passed && !summary.checks_failed) return null;
  if (item.level === "warn" || item.action === "recover_context") return <div className="chat-notice" role="status" data-level={item.level}>
    {item.title && <strong>{item.title} </strong>}{item.text}
    {summary && <details className="chat-notice__details"><summary>{t("chat.details")}</summary><pre>{JSON.stringify(summary, null, 2)}</pre></details>}
    {item.detail && <ChatDisclosure label={t("chat.details")}><pre>{item.detail}</pre></ChatDisclosure>}
    {item.action === "recover_context" && item.recoveryId && <button className="btn" onClick={() => actions.recover(item.recoveryId!)}>{t("notice.protocolRecoveryAction")}</button>}
  </div>;
  return <ContextInjectionRow title={item.title || t(item.decisionReceipt ? "chat.decision" : summary ? "chat.record" : "chat.notice")}
    summary={item.decisionReceipt ? undefined : item.text.split("\n")[0]} beforeToggle={scroll.beforeChange}>
    <pre>{item.text}{item.detail ? `\n${item.detail}` : ""}{summary ? `\n${JSON.stringify(summary, null, 2)}` : ""}</pre>
  </ContextInjectionRow>;
}

function ChatTool({ node, loader, actions, scroll }: { node: Extract<ChatNode, { kind: "tool" }>; loader: ChatContentLoader; actions: ChatActions; scroll: ChatScrollController }) {
  const t = useT();
  const item = node.item;
  let args: Record<string, unknown> = {};
  try { args = JSON.parse(item.args) || {}; } catch { /* Arguments may still be streaming. */ }
  const description = typeof args.description === "string" ? args.description : "";
  const presentCount = Array.isArray(args.files) ? args.files.length : item.presentedFiles?.length ?? 0;
  const presentSummary = item.name === "present"
    ? t(item.status === "running" ? "present.presenting" : item.status === "done" ? "present.presented" : "present.failed", { count: presentCount })
    : "";
  const summary = presentSummary || description || subjectOf(item.name, item.args) || item.subject || item.summary || "";
  const toolKind = classifyTool(item);
  const Icon = toolKind === "present" ? PackageOpen : { search: Search, web: Globe, shell: Terminal, agent: Users, file: FileText, tool: Wrench }[toolKind];
  const title = item.name === "web_search" ? t("chat.tool.search") : item.name === "web_fetch" ? t("chat.tool.web")
    : item.name === "present" ? t("present.toolTitle") : toolKind === "shell" ? shellDisplayName(item) : item.resolvedName || item.name;
  return <ToolRow icon={<Icon size={14} />} title={title} summary={[summary, summarizeFileDiff(item.fileDiff)].filter(Boolean).join(" · ")}
    state={item.status} statusLabel={t(item.status === "running" ? "chat.running" : item.status === "stopped" ? "chat.stopped" : "chat.failed")}
    errorSummary={item.error?.trim().split("\n")[0]} beforeToggle={scroll.beforeChange}
    inspectLabel={t("chat.details")} inspect={trigger => actions.openDetails(node.key, trigger)}>
    <Suspense fallback={<p role="status">{t("chat.loading")}</p>}><ChatToolBody item={item} loader={loader} /></Suspense>
  </ToolRow>;
}


function ChatDisclosure({ label, children }: { label: string; children: import("react").ReactNode }) {
  const [open, setOpen] = useState(false);
  return <details className="chat-notice" open={open} onToggle={event => setOpen(event.currentTarget.open)}><summary>{label}</summary>{open && children}</details>;
}

function BodyLoadError({ item, loader }: { item: Extract<ChatNode, { kind: "assistant" | "user" }>["item"]; loader: ChatContentLoader }) {
  const [error, setError] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const t = useT();
  useEffect(() => {
    let cancelled = false;
    if (item.kind !== "assistant" || !item.streaming) void loader.load(item, "content").then(
      () => { if (!cancelled) setError(false); }, () => { if (!cancelled) setError(true); });
    return () => { cancelled = true; };
  }, [loader, item, attempt]);
  return error && <button className="btn" onClick={() => { setError(false); setAttempt(value => value + 1); }}>{t("chat.loadFailed")}</button>;
}
function ChatUser({ node, loader }: { node: Extract<ChatNode, { kind: "user" }>; loader: ChatContentLoader }) {
  return <><UserMessage id={node.item.id} text={node.item.text} submitText={node.item.submitText} failed={node.item.failed} createdAt={node.item.createdAt} /><BodyLoadError item={node.item} loader={loader} /></>;
}
function ChatAnswer({ node, loader, source, tabId, hostId }: { node: Extract<ChatNode, { kind: "assistant" }>; loader: ChatContentLoader; source: ChatSource; tabId?: string; hostId?: string }) {
  const tail = useChatNode(source, `${node.turnKey}:tail`);
  const presentedFiles = tail?.kind === "tail" ? tail.presentedFiles : [];
  return <><AssistantMessage item={node.item} presentedFiles={presentedFiles} tabId={tabId} hostId={hostId} /><BodyLoadError item={node.item} loader={loader} /></>;
}

function ChatReasoning({ node, loader, source, scroll }: { node: Extract<ChatNode, { kind: "reasoning" }>; loader: ChatContentLoader; source: ChatSource; scroll: ChatScrollController }) {
  const t = useT();
  const [result, setResult] = useState<{ item: unknown; text: string }>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(false);
  const epoch = useRef(0);
  useEffect(() => { return () => { epoch.current++; }; }, []);
  const text = node.item.reasoning;
  const full = result && (result.item === node.item || result.text === node.item.reasoning) ? result.text : undefined;
  if (!text) return null;
  const needsFull = full === undefined && (text.length > 8000 || loader.needsFullContent(node.item, "reasoning"));
  const load = async () => {
    const ticket = ++epoch.current; setBusy(true); setError(false);
    try {
      const value = await loader.load(node.item, "reasoning");
      const current = source.getNodeSnapshot(node.key);
      if (ticket !== epoch.current || current?.kind !== "reasoning" || (current.item !== node.item && current.item.reasoning !== value)) throw new Error("Reasoning content changed; retry");
      setResult({ item: current.item, text: value }); return value;
    }
    catch (error) { if (ticket === epoch.current) setError(true); throw error; }
    finally { if (ticket === epoch.current) setBusy(false); }
  };
  return <div className="chat-reasoning"><ReasoningRow text={text} running={node.item.streaming} t={t} beforeToggle={scroll.beforeChange}
    duration={node.item.reasoningDurationMs != null ? `${(node.item.reasoningDurationMs / 1000).toFixed(1)}s` : undefined}>
    <div className="chat-reasoning__body">{full ?? text.slice(0, 8000)}
    {needsFull && <button className="btn" disabled={busy} onClick={() => void load().catch(() => {})}>{t(error ? "chat.loadFailed" : busy ? "chat.loading" : "chat.loadFull")}</button>}
    <CopyButton getText={() => full === undefined ? load() : full} label={t("chat.copyFull")} />
  </div></ReasoningRow></div>;
}

function ChatTurnTail({ node, source, actions, loader, tabId, hostId }: { node: Extract<ChatNode, { kind: "tail" }>; source: ChatSource; actions: ChatActions; loader: ChatContentLoader; tabId?: string; hostId?: string }) {
  const answer = useChatNode(source, node.answerKey ?? "");
  const t = useT();
  const reasonId = useId();
  const hasAnswer = answer?.kind === "assistant" && Boolean(answer.item.text.trim());
  if (!hasAnswer && !node.presentedFiles.length && !node.modifiedFiles.length) return null;
  const checkpoint = actions.checkpoints.find(checkpoint => checkpoint.turn === node.turn);
  const unavailable = actions.forkDisabled || !checkpoint?.canConversation || node.turn == null || !actions.fork;
  return <div className="chat-turn-tail">
    {node.presentedFiles.length > 0 && <Suspense fallback={null}>
      <PresentedFiles files={node.presentedFiles} tabId={tabId} hostId={hostId} />
    </Suspense>}
    {node.modifiedFiles.length > 0 && <Suspense fallback={null}>
      <ModifiedFiles files={node.modifiedFiles} tabId={tabId} hostId={hostId} />
    </Suspense>}
    {hasAnswer && <div className="chat-actions" data-actions-reveal={node.latest ? "always" : "hover"}><CopyButton getText={async () => {
    const text = answer.item.streaming ? answer.item.text : await loader.load(answer.item, "content");
    const current = source.getNodeSnapshot(answer.key);
    if (current?.kind !== "assistant" || (current.item !== answer.item && current.item.text !== text)) throw new Error("Answer changed; retry");
    return text;
  }} label={t("msg.copy")} showInlineLabel={false} className="chat-action-icon" />
    <Tooltip label={unavailable ? t("chat.branchUnavailable") : t("chat.branch")} side="bottom">
      <button
        type="button"
        className="chat-action-icon"
        aria-label={t("chat.branch")}
        aria-disabled={unavailable || undefined}
        aria-describedby={unavailable ? reasonId : undefined}
        data-unavailable={unavailable || undefined}
        onClick={unavailable ? undefined : () => actions.fork?.(node.turn!)}
      ><GitBranch aria-hidden="true" /></button>
    </Tooltip>
    {unavailable && <span id={reasonId} className="sr-only">{t("chat.branchUnavailable")}</span>}
    {answer!.item.turnUsage && answer!.item.turnUsage.totalTokens > 0 && <TurnUsagePanel usage={answer!.item.turnUsage} />}
    {(answer!.item.turnDurationMs ?? answer!.item.workDurationMs) != null && <TurnTimePanel
      durationMs={(answer!.item.turnDurationMs ?? answer!.item.workDurationMs)!}
      tokensPerSecond={answer!.item.tokensPerSecond}
    />}
    {answer!.item.createdAt != null && <time className="chat-actions__time" dateTime={new Date(answer!.item.createdAt).toISOString()}>{formatMessageClock(answer!.item.createdAt)}</time>}
  </div>}
  </div>;
}

export function ChatDetails({ source, nodeKey, loader, onClose, onNavigate }: { source: ChatSource; nodeKey: string; loader: ChatContentLoader; onClose: () => void; onNavigate: (key: string) => void }) {
  const node = useChatNode(source, nodeKey);
  const subscribeChildren = useCallback((listener: () => void) => source.subscribeNode(`${nodeKey}:children`, listener), [source, nodeKey]);
  const getChildren = useCallback(() => source.toolChildren(nodeKey), [source, nodeKey]);
  const children = useSyncExternalStore(subscribeChildren, getChildren, getChildren);
  const t = useT();
  const [result, setResult] = useState<{ item: unknown; text: string }>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(false);
  const [selectedTab, setSelectedTab] = useState<"result" | "parameters" | "children" | "raw">("result");
  const root = useRef<HTMLElement>(null);
  const epoch = useRef(0);
  useEffect(() => { root.current?.focus(); return () => { epoch.current++; }; }, []);
  const load = async () => {
    if (node?.kind !== "tool") throw new Error("Tool unavailable");
    const ticket = ++epoch.current; setBusy(true); setError(false);
    try { const text = await loader.load(node.item, "tool"); if (ticket === epoch.current) {
      const current = source.getNodeSnapshot(nodeKey);
      if (current?.kind !== "tool" || (current.item !== node.item && !matchesToolContent(current.item, text))) throw new Error("Tool content changed; retry");
      setResult({ item: current.item, text });
      return text;
    } throw new Error("Detail closed"); }
    catch (error) { if (ticket === epoch.current) setError(true); throw error; }
    finally { if (ticket === epoch.current) setBusy(false); }
  };
  if (node?.kind !== "tool") return null;
  const full = result && (result.item === node.item || matchesToolContent(node.item, result.text)) ? result.text : undefined;
  const preview = JSON.stringify({ args: node.item.args, output: node.item.output, error: node.item.error, diff: node.item.fileDiff }, null, 2);
  const hasResult = Boolean(node.item.output || node.item.error || node.item.fileDiff);
  const hasParameters = Boolean(node.item.args.trim() && node.item.args.trim() !== "{}");
  const hasRelations = Boolean(node.item.parentId || children.length);
  const tabs = [hasResult && "result", hasParameters && "parameters", hasRelations && "children", "raw"].filter(Boolean) as Array<"result" | "parameters" | "children" | "raw">;
  const activeTab = tabs.includes(selectedTab) ? selectedTab : tabs[0];
  const title = classifyTool(node.item) === "shell" ? shellDisplayName(node.item) : node.item.resolvedName ?? node.item.name;
  let formattedArgs = node.item.args;
  try { formattedArgs = JSON.stringify(JSON.parse(node.item.args), null, 2); } catch { /* Streaming or unknown arguments stay copyable. */ }
  const resultPreview = JSON.stringify({ output: node.item.output, error: node.item.error, diff: node.item.fileDiff }, null, 2);
  let resultText = resultPreview;
  if (full !== undefined) {
    try {
      const payload = JSON.parse(full) as Record<string, unknown>;
      resultText = JSON.stringify({ output: payload.output, error: payload.error, diff: payload.diff }, null, 2);
    } catch { resultText = full; }
  }
  return <aside ref={root} className="chat-details" role="dialog" aria-modal="true" aria-label={t("chat.details")} tabIndex={-1} onKeyDown={event => {
    if (event.key === "Escape") { event.stopPropagation(); onClose(); }
    if (event.key === "Tab") {
      const buttons = Array.from(root.current?.querySelectorAll<HTMLElement>("button:not(:disabled), [href], [tabindex='0']") ?? []);
      const first = buttons[0], last = buttons[buttons.length - 1];
      if (event.shiftKey && (document.activeElement === first || document.activeElement === root.current)) { event.preventDefault(); last?.focus(); }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus(); }
    }
  }}>
    <header><strong>{title}</strong><button className="btn" aria-label={t("common.close")} onClick={onClose}><X size={16} /></button></header>
    <div className="chat-details__tabs" role="tablist" aria-label={t("chat.details")}>
      {tabs.map(tab => <button key={tab} type="button" role="tab" aria-selected={activeTab === tab} onClick={() => setSelectedTab(tab)}>{t(`chat.details.${tab}`)}</button>)}
    </div>
    <div className="chat-details__body">
      {(activeTab === "result" || activeTab === "raw") && full === undefined && <button className="btn" disabled={busy} onClick={() => void load().catch(() => {})}>{t(error ? "chat.loadFailed" : busy ? "chat.loading" : "chat.loadFull")}</button>}
      {(activeTab === "result" || activeTab === "raw") && <CopyButton getText={() => full === undefined ? load() : full} label={t("chat.copyFull")} />}
      {activeTab === "result" && <Suspense fallback={<p role="status">{t("chat.loading")}</p>}><ToolPayload text={resultText} preview={full === undefined} /></Suspense>}
      {activeTab === "parameters" && <pre>{formattedArgs}</pre>}
      {activeTab === "children" && <div className="chat-details__relations">
        {node.item.parentId && source.getNodeSnapshot(node.item.parentId)?.kind === "tool" && <button className="btn" onClick={() => onNavigate(node.item.parentId!)}>← {t("chat.details.parent")}</button>}
        {children.map(child => <button className="chat-tool" key={child.key} onClick={() => onNavigate(child.key)}>{child.item.resolvedName ?? child.item.name} · {child.item.status}</button>)}
      </div>}
      {activeTab === "raw" && <pre>{full ?? preview}</pre>}
    </div>
  </aside>;
}

function matchesToolContent(item: Extract<ChatNode, { kind: "tool" }>["item"], text: string): boolean {
  try { const value = JSON.parse(text); return value.args === item.args && value.output === item.output && value.error === item.error && JSON.stringify(value.diff) === JSON.stringify(item.fileDiff); }
  catch { return false; }
}

export function ChatRunning({ source }: { source: ChatSource }) {
  const status = useSyncExternalStore(source.subscribeStatus, source.getStatusSnapshot, source.getStatusSnapshot);
  const t = useT();
  const [now, setNow] = useState(Date.now);
  useEffect(() => { if (!status.running) return; const timer = setInterval(() => setNow(Date.now()), 1000); return () => clearInterval(timer); }, [status.running]);
  return status.running ? <div className="chat-running" role="status">{t("chat.running")} {status.startedAt ? `${Math.max(0, Math.floor((now - status.startedAt) / 1000))}s` : ""}</div> : null;
}
