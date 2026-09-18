import type { Item, LiveStream } from "./useController";
import { canonicalUserConfirmations, matchLocalSubmissions, type LocalSubmission } from "./localSubmissionState";
import type { PresentedFile } from "./types";
import { deriveTurnFiles, fileIdentity, type TurnFileView } from "./turnFiles";
import { recordFrontendDiagnostic } from "./frontendDiagnosticBridge";

export type PresentedFileView = PresentedFile & { toolCallId: string };

export type ChatNodeKey = string;
type Listener = () => void;
type ItemNode = { [K in Item["kind"]]: { kind: K; key: string; turnKey: string; item: Extract<Item, { kind: K }> } }[Item["kind"]];
export type ChatNode = ItemNode
  | { kind: "reasoning"; key: string; turnKey: string; item: Extract<Item, { kind: "assistant" }> }
  | { kind: "process"; key: string; turnKey: string; members: readonly string[]; collapsed: boolean; foldable: boolean; toolCallCount: number; messageCount: number; subagentCount: number; failureCount: number }
  | { kind: "tail"; key: string; turnKey: string; answerKey?: string; turn?: number; latest: boolean; presentedFiles: readonly PresentedFileView[]; modifiedFiles: readonly TurnFileView[] };
export interface ChatStatus { running: boolean; hydrating: boolean; hasOlder: boolean; loadingOlder: boolean; error?: string; startedAt?: number }
export interface ChatInput extends ChatStatus {
  items: readonly Item[];
  localSubmissions?: readonly LocalSubmission[];
  visibleSubmissionHandoffs?: Readonly<Record<string, { submissionId: string }>>;
  live?: LiveStream;
  historyStartTurn?: number;
}
export interface ChatViewSource {
  getOrderSnapshot(): readonly ChatNodeKey[];
  subscribeOrder(listener: Listener): () => void;
  getNodeSnapshot(key: ChatNodeKey): ChatNode | undefined;
  subscribeNode(key: ChatNodeKey, listener: Listener): () => void;
  getStatusSnapshot(): ChatStatus;
  subscribeStatus(listener: Listener): () => void;
  dispose(): void;
}

const sameKeys = (a: readonly string[], b: readonly string[]) => a.length === b.length && a.every((key, i) => key === b[i]);
const sameReferences = (a: readonly unknown[] = [], b: readonly unknown[] = []) =>
  a.length === b.length && a.every((item, index) => item === b[index]);
const shallowSame = (a: object, b: object) => Object.keys(a).length === Object.keys(b).length
  && Object.entries(a).every(([key, value]) => value === (b as Record<string, unknown>)[key]);
const foldViews = new Map<string, Set<string>>();
const emptyChildren: readonly Extract<ChatNode, { kind: "tool" }>[] = [];
function proxyAuditCall(item: Item): string | undefined {
  if (item.kind !== "notice" || item.code !== "capability_proxy_audit") return undefined;
  try { return (JSON.parse(item.detail ?? "{}") as { callId?: string }).callId || undefined; } catch { return undefined; }
}

function submissionItem(submission: LocalSubmission): Extract<Item, { kind: "user" }> {
  return {
    kind: "user",
    id: submission.localId,
    submissionId: submission.submissionId,
    messageId: submission.messageId,
    turnId: submission.turnId,
    submissionState: submission.status === "accepted" ? "confirmed" : submission.status,
    text: submission.text,
    submitText: submission.submitText,
    failed: submission.status === "failed",
    createdAt: submission.createdAt,
    checkpointTurn: submission.checkpointTurn,
  };
}

type DisplayEntry = { item: Item; local?: LocalSubmission };
function itemsWithLocalSubmissions(input: ChatInput): DisplayEntry[] {
  const submissions = input.localSubmissions ?? [];
  const items: DisplayEntry[] = uniqueUserItems(input.items).map(item => ({ item }));
  const matched = new Set(matchLocalSubmissions(submissions, canonicalUserConfirmations(input.items)).map(match => match.submissionId));
  const insertedAfter = new Map<string, number>();
  for (const submission of [...submissions].sort((a, b) => a.sequence - b.sequence)) {
    if (matched.has(submission.submissionId)) continue;
    const item = { item: submissionItem(submission), local: submission };
    const turnIndex = submission.turnId ? items.findIndex(candidate => candidate.item.turnId === submission.turnId) : -1;
    if (!submission.anchorItemId && submission.placement !== "latest" && !input.hasOlder && (input.historyStartTurn ?? 0) === 0) {
      items.splice(insertedAfter.get("") ?? 0, 0, item);
      insertedAfter.set("", (insertedAfter.get("") ?? 0) + 1);
      continue;
    }
    const anchor = submission.anchorItemId ? items.findIndex(candidate => candidate.item.id === submission.anchorItemId) : -1;
    if (anchor < 0) {
      if (turnIndex >= 0) items.splice(turnIndex, 0, item);
      continue;
    }
    const offset = insertedAfter.get(submission.anchorItemId!) ?? 0;
    items.splice(anchor + 1 + offset, 0, item);
    insertedAfter.set(submission.anchorItemId!, offset + 1);
  }
  return items;
}

/** A reconstructable presentation projection. Controller/history remain authoritative. */
export class ChatSource implements ChatViewSource {
  private order: readonly string[] = [];
  private nodes = new Map<string, ChatNode>();
  private children = new Map<string, readonly Extract<ChatNode, { kind: "tool" }>[]>();
  private orderListeners = new Set<Listener>();
  private statusListeners = new Set<Listener>();
  private nodeListeners = new Map<string, Set<Listener>>();
  private projectedGroups = new Map<string, {
    user?: Extract<Item, { kind: "user" }>;
    items: readonly Item[];
    active: boolean;
    latest: boolean;
    present: readonly string[];
    order: readonly string[];
  }>();
  private dirty = new Set<string>();
  private orderDirty = false;
  private statusDirty = false;
  private scheduled = false;
  private epoch = 0;
  private input?: ChatInput;
  private status: ChatStatus = { running: false, hydrating: true, hasOlder: false, loadingOlder: false };
  private opened = new Set<string>();
  private displayKeyBySubmission = new Map<string, string>();
  private displayMessageBySubmission = new Map<string, string>();
  private displayKeyByMessage = new Map<string, string>();
  constructor(readonly sessionKey: string) { this.opened = new Set(foldViews.get(sessionKey)); }
  getOrderSnapshot = () => this.order;
  getNodeSnapshot = (key: string) => this.nodes.get(key);
  getStatusSnapshot = () => this.status;
  subscribeOrder = (listener: Listener) => { this.orderListeners.add(listener); return () => { this.orderListeners.delete(listener); }; };
  subscribeStatus = (listener: Listener) => { this.statusListeners.add(listener); return () => { this.statusListeners.delete(listener); }; };
  subscribeNode = (key: string, listener: Listener) => {
    let listeners = this.nodeListeners.get(key);
    if (!listeners) { listeners = new Set(); this.nodeListeners.set(key, listeners); }
    listeners.add(listener);
    return () => { listeners.delete(listener); if (!listeners.size) this.nodeListeners.delete(key); };
  };
  private put(node: ChatNode) {
    const previous = this.nodes.get(node.key);
    if (previous && shallowSame(previous, node)) return;
    this.nodes.set(node.key, node);
    this.dirty.add(node.key);
  }
  update(input: ChatInput) {
    const previous = this.input;
    this.input = input;
    const { running, hydrating, hasOlder, loadingOlder, error, startedAt } = input;
    const status = { running, hydrating, hasOlder, loadingOlder, error, startedAt };
    if (!shallowSame(status, this.status)) { this.status = status; this.statusDirty = true; }
    if (!previous || previous.items !== input.items || previous.visibleSubmissionHandoffs !== input.visibleSubmissionHandoffs || !sameReferences(previous.localSubmissions, input.localSubmissions)
      || previous.running !== running || previous.hasOlder !== hasOlder) this.project(input);
    this.updateLive(input.live, false);
    this.schedule();
  }
  private project(input: ChatInput) {
    for (const local of input.localSubmissions ?? []) if (local.messageId) this.displayMessageBySubmission.set(local.submissionId, local.messageId);
    for (const [messageId, handoff] of Object.entries(input.visibleSubmissionHandoffs ?? {})) {
      this.displayMessageBySubmission.set(handoff.submissionId, messageId);
    }
    const order: string[] = [];
    const present = new Set<string>();
    const groups: Array<{ key: string; turn?: number; user?: Extract<Item, { kind: "user" }>; items: Item[] }> = [];
    let group: (typeof groups)[number] = { key: "history-head", items: [] };
    groups.push(group);
    for (const { item, local } of itemsWithLocalSubmissions(input)) {
      if (item.kind === "user") {
        const key = this.userDisplayKey(item, local, input.visibleSubmissionHandoffs);
        group = { key, user: item, turn: item.checkpointTurn ?? item.historyTurn, items: [] };
        groups.push(group);
      } else group.items.push(item);
    }
    const groupKeys = new Set(groups.map(current => current.key));
    for (const current of groups) {
      if (!current.user && !current.items.length) continue;
      const turnKey = current.key;
      const active = current === groups[groups.length - 1] && input.running;
      const latest = current === groups[groups.length - 1];
      const cached = this.projectedGroups.get(turnKey);
      if (cached && cached.user === current.user && cached.active === active && cached.latest === latest && sameReferences(cached.items, current.items)) {
        cached.present.forEach(key => present.add(key));
        order.push(...cached.order);
        continue;
      }
      const groupPresent: string[] = [];
      const groupOrderStart = order.length;
      const add = (node: ChatNode, visible = true) => {
        this.put(node); present.add(node.key); groupPresent.push(node.key); if (visible) order.push(node.key);
      };
      if (current.user) add({ kind: "user", key: turnKey, turnKey, item: current.user });
      const answer = (current.items.find(item => item.kind === "assistant" && item.turnFinal)
        ?? [...current.items].reverse().find(item => item.kind === "assistant" && item.turnFinal === undefined && item.text.trim())) as Extract<Item, { kind: "assistant" }> | undefined;
      const answerIndex = answer ? current.items.indexOf(answer) : -1;
      // Harness folds the completed process range, including recovered call errors.
      // Terminal failures/recovery prompts remain independent and prevent auto-fold.
      const failed = current.items.some((item, index) => item.kind === "assistant" && item.streaming
        || item.kind === "tool" && item.status === "stopped"
        || item.kind === "notice" && (item.action === "recover_context" || item.action === "isolate_images"
          || index > answerIndex && !item.decisionReceipt && !item.completionSummary));
      const mergedAudits = new Set(current.items.filter(item => {
        const call = proxyAuditCall(item);
        return call && current.items.some(tool => tool.kind === "tool" && tool.id === call);
      }).map(item => item.id));
      const members = current.items.flatMap(item => mergedAudits.has(item.id) ? [] : item.kind === "assistant"
        ? [...(item !== answer ? [item.id] : []), `${item.id}:reasoning`]
        : item.kind === "notice" && (item.level === "warn" || item.action === "recover_context" || item.action === "isolate_images")
          || item.kind === "extension" && item.card.actions?.length ? [] : [item.id]);
      const processKey = `${turnKey}:process`;
      const old = this.nodes.get(processKey);
      const stableMembers = old?.kind === "process" && sameKeys(old.members, members) ? old.members : members;
      const hasProcess = current.items.some(item => item.kind === "tool" || item.kind === "phase" ||
        item.kind === "assistant" && (item !== answer && item.text.trim() || item.reasoning.trim()));
      const hasTrailingWork = current.items.slice(answerIndex + 1).some(item => item.kind === "tool" || item.kind === "phase" || item.kind === "assistant");
      const foldable = Boolean(hasProcess && current.user && answer && !hasTrailingWork && !active && !failed && !current.user.failed);
      const allCalls = current.items.filter((item): item is Extract<Item, { kind: "tool" }> => item.kind === "tool");
      const calls = allCalls.filter(item => !item.parentId);
      const subagentCount = calls.filter(item => ["task", "read_only_task", "parallel_tasks", "fleet", "subagent"].includes(item.name)).length;
      add({ kind: "process", key: processKey, turnKey, members: stableMembers, foldable, collapsed: foldable && !this.opened.has(turnKey),
        toolCallCount: calls.length - subagentCount, subagentCount,
        messageCount: current.items.filter(item => item.kind === "assistant" && item !== answer && item.text.trim()).length,
        failureCount: calls.filter(item => item.status === "error" || item.error).length });
      for (const item of current.items) {
        if (item.kind === "assistant") add({ kind: "reasoning", key: `${item.id}:reasoning`, turnKey, item });
        add({ kind: item.kind, key: item.id, turnKey, item } as ItemNode, !mergedAudits.has(item.id) && !(item.kind === "tool" && item.parentId));
      }
      const declarations = allCalls.filter(item => item.name === "present" && item.status === "done" && !item.error && item.presentedFiles?.length);
      const latestByPath = new Map<string, PresentedFileView>();
      for (const call of declarations) {
        for (const file of call.presentedFiles ?? []) latestByPath.set(file.path, { ...file, toolCallId: call.id });
      }
      const nextPresented = [...latestByPath.values()];
      const presentedPaths = new Set(nextPresented.map(file => fileIdentity(file.path)));
      const nextModified = deriveTurnFiles(allCalls).filter(file => !presentedPaths.has(fileIdentity(file.path)));
      const tailKey = `${turnKey}:tail`;
      const oldTail = this.nodes.get(tailKey);
      const stablePresented = oldTail?.kind === "tail"
        && oldTail.presentedFiles.length === nextPresented.length
        && oldTail.presentedFiles.every((file, index) => file.path === nextPresented[index]?.path && file.description === nextPresented[index]?.description && file.toolCallId === nextPresented[index]?.toolCallId)
        ? oldTail.presentedFiles : nextPresented;
      const stableModified = oldTail?.kind === "tail"
        && oldTail.modifiedFiles.length === nextModified.length
        && oldTail.modifiedFiles.every((file, index) => file.path === nextModified[index]?.path && file.operation === nextModified[index]?.operation && file.toolCallId === nextModified[index]?.toolCallId)
        ? oldTail.modifiedFiles : nextModified;
      add({ kind: "tail", key: tailKey, turnKey, answerKey: answer?.id, turn: current.turn,
        latest, presentedFiles: stablePresented, modifiedFiles: stableModified });
      this.projectedGroups.set(turnKey, {
        user: current.user, items: current.items, active, latest, present: groupPresent, order: order.slice(groupOrderStart),
      });
    }
    for (const key of this.nodes.keys()) if (!present.has(key)) { this.nodes.delete(key); this.dirty.add(key); }
    for (const key of this.projectedGroups.keys()) if (!groupKeys.has(key)) this.projectedGroups.delete(key);
    for (const [submissionId, key] of this.displayKeyBySubmission) if (!groupKeys.has(key)) this.displayKeyBySubmission.delete(submissionId);
    for (const submissionId of this.displayMessageBySubmission.keys()) if (!this.displayKeyBySubmission.has(submissionId)) this.displayMessageBySubmission.delete(submissionId);
    for (const [messageId, key] of this.displayKeyByMessage) if (!groupKeys.has(key)) this.displayKeyByMessage.delete(messageId);
    const children = new Map<string, Extract<ChatNode, { kind: "tool" }>[]>();
    for (const node of this.nodes.values()) if (node.kind === "tool" && node.item.parentId) {
      const list = children.get(node.item.parentId) ?? [];
      list.push(node); children.set(node.item.parentId, list);
    }
    for (const key of new Set([...children.keys(), ...this.children.keys()])) {
      const next = children.get(key) ?? emptyChildren;
      const old = this.children.get(key) ?? emptyChildren;
      if (next.length !== old.length || next.some((node, index) => node !== old[index])) {
        this.children.set(key, next); this.dirty.add(`${key}:children`);
      }
    }
    for (const key of this.opened) if (!groupKeys.has(key)) this.opened.delete(key);
    if (!sameKeys(this.order, order)) { this.order = order; this.orderDirty = true; }
    recordFrontendDiagnostic("transcript", "presentation", this.presentationStats());
  }
  private userDisplayKey(item: Extract<Item, { kind: "user" }>, local?: LocalSubmission, handoffs?: ChatInput["visibleSubmissionHandoffs"]): string {
    if (local) {
      const key = `submission:${local.submissionId}`;
      this.displayKeyBySubmission.set(local.submissionId, key);
      return key;
    }
    let key = item.messageId ? this.displayKeyByMessage.get(item.messageId) : undefined;
    const submissionId = item.messageId ? handoffs?.[item.messageId]?.submissionId ?? item.submissionId : undefined;
    const boundMessageId = submissionId ? this.displayMessageBySubmission.get(submissionId) : undefined;
    if (!key && submissionId && (!boundMessageId || boundMessageId === item.messageId)) {
      key = this.displayKeyBySubmission.get(submissionId);
    }
    key ??= item.id;
    if (submissionId && this.displayKeyBySubmission.get(submissionId) === key && item.messageId) {
      this.displayMessageBySubmission.set(submissionId, item.messageId);
    }
    if (item.messageId) this.displayKeyByMessage.set(item.messageId, key);
    return key;
  }
  presentationStats() {
    return { nodes: this.nodes.size, submissions: this.displayKeyBySubmission.size, messages: this.displayKeyByMessage.size };
  }
  /** Already frame-batched by the controller; no additional frame queue. */
  updateLive(live: LiveStream | undefined, publish = true) {
    if (live && this.status.running) {
      const node = this.nodes.get(live.id);
      if (node?.kind === "assistant") {
        const item = { ...node.item, text: live.text, reasoning: live.reasoning, reasoningComplete: live.reasoningComplete, streaming: true };
        if (!shallowSame(node.item, item)) {
          this.put({ ...node, item });
          const reasoning = this.nodes.get(`${node.key}:reasoning`);
          if (reasoning?.kind === "reasoning") this.put({ ...reasoning, item });
        }
      }
    }
    if (publish) this.flush();
  }
  toggleProcess(turnKey: string) {
    const node = this.nodes.get(`${turnKey}:process`);
    if (node?.kind !== "process") return;
    if (node.collapsed) this.opened.add(turnKey); else this.opened.delete(turnKey);
    foldViews.delete(this.sessionKey); foldViews.set(this.sessionKey, new Set(this.opened));
    if (foldViews.size > 100) foldViews.delete(foldViews.keys().next().value!);
    this.put({ ...node, collapsed: node.foldable && !this.opened.has(turnKey) });
    this.flush();
  }
  toolChildren(id: string) { return this.children.get(id) ?? emptyChildren; }
  toolAudits(id: string): string[] {
    return [...this.nodes.values()].flatMap(node => node.kind === "notice" && proxyAuditCall(node.item) === id
      ? [`${node.item.text}\n${node.item.detail ?? ""}`] : []);
  }
  private schedule() {
    if (this.scheduled) return;
    this.scheduled = true;
    const epoch = this.epoch;
    queueMicrotask(() => { if (epoch === this.epoch) this.flush(); });
  }
  private flush() {
    this.scheduled = false;
    const dirty = this.dirty; this.dirty = new Set();
    const orderDirty = this.orderDirty; this.orderDirty = false;
    const statusDirty = this.statusDirty; this.statusDirty = false;
    if (orderDirty) this.orderListeners.forEach(listener => listener());
    if (statusDirty) this.statusListeners.forEach(listener => listener());
    dirty.forEach(key => this.nodeListeners.get(key)?.forEach(listener => listener()));
  }
  dispose() {
    this.epoch++; this.scheduled = false; this.dirty.clear();
    this.orderListeners.clear(); this.statusListeners.clear(); this.nodeListeners.clear();
    this.nodes.clear(); this.children.clear(); this.projectedGroups.clear();
    this.displayKeyBySubmission.clear(); this.displayMessageBySubmission.clear(); this.displayKeyByMessage.clear();
    this.order = []; this.input = undefined;
  }
}
import { uniqueUserItems } from "./transcriptUserIdentity";
