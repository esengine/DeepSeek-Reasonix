import type { Item, LiveStream } from "./useController";
import type { PresentedFile } from "./types";
import { deriveTurnFiles, fileIdentity, type TurnFileView } from "./turnFiles";

export type PresentedFileView = PresentedFile & { toolCallId: string };

export type ChatNodeKey = string;
type Listener = () => void;
type ItemNode = { [K in Item["kind"]]: { kind: K; key: string; turnKey: string; item: Extract<Item, { kind: K }> } }[Item["kind"]];
export type ChatNode = ItemNode
  | { kind: "reasoning"; key: string; turnKey: string; item: Extract<Item, { kind: "assistant" }> }
  | { kind: "process"; key: string; turnKey: string; members: readonly string[]; collapsed: boolean; foldable: boolean; toolCallCount: number; messageCount: number; subagentCount: number; failureCount: number }
  | { kind: "tail"; key: string; turnKey: string; answerKey?: string; turn?: number; latest: boolean; presentedFiles: readonly PresentedFileView[]; modifiedFiles: readonly TurnFileView[] };
export interface ChatStatus { running: boolean; hydrating: boolean; hasOlder: boolean; loadingOlder: boolean; error?: string; startedAt?: number }
export interface ChatInput extends ChatStatus { items: readonly Item[]; live?: LiveStream; historyStartTurn?: number }
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
const shallowSame = (a: object, b: object) => Object.keys(a).length === Object.keys(b).length
  && Object.entries(a).every(([key, value]) => value === (b as Record<string, unknown>)[key]);
const foldViews = new Map<string, Set<string>>();
const emptyChildren: readonly Extract<ChatNode, { kind: "tool" }>[] = [];

/** A reconstructable presentation projection. Controller/history remain authoritative. */
export class ChatSource implements ChatViewSource {
  private order: readonly string[] = [];
  private nodes = new Map<string, ChatNode>();
  private children = new Map<string, readonly Extract<ChatNode, { kind: "tool" }>[]>();
  private orderListeners = new Set<Listener>();
  private statusListeners = new Set<Listener>();
  private nodeListeners = new Map<string, Set<Listener>>();
  private dirty = new Set<string>();
  private orderDirty = false;
  private statusDirty = false;
  private scheduled = false;
  private epoch = 0;
  private input?: ChatInput;
  private status: ChatStatus = { running: false, hydrating: true, hasOlder: false, loadingOlder: false };
  private opened = new Set<string>();
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
    if (!previous || previous.items !== input.items || previous.running !== running || previous.hasOlder !== hasOlder) this.project(input);
    this.updateLive(input.live, false);
    this.schedule();
  }
  private project(input: ChatInput) {
    const order: string[] = [];
    const present = new Set<string>();
    const groups: Array<{ key: string; turn?: number; user?: Extract<Item, { kind: "user" }>; items: Item[] }> = [];
    let group: (typeof groups)[number] = { key: "history-head", items: [] };
    groups.push(group);
    for (const item of input.items) {
      if (item.kind === "user") {
        group = { key: item.id, user: item, turn: item.checkpointTurn ?? item.historyTurn, items: [] };
        groups.push(group);
      } else group.items.push(item);
    }
    const add = (node: ChatNode, visible = true) => { this.put(node); present.add(node.key); if (visible) order.push(node.key); };
    for (const current of groups) {
      if (!current.user && !current.items.length) continue;
      const turnKey = current.key;
      if (current.user) add({ kind: "user", key: current.user.id, turnKey, item: current.user });
      const active = current === groups[groups.length - 1] && input.running;
      const answer = [...current.items].reverse().find(item => item.kind === "assistant" && item.text.trim()) as Extract<Item, { kind: "assistant" }> | undefined;
      const answerIndex = answer ? current.items.indexOf(answer) : -1;
      // Harness folds the completed process range, including recovered call errors.
      // Terminal failures/recovery prompts remain independent and prevent auto-fold.
      const failed = current.items.some((item, index) => item.kind === "assistant" && item.streaming
        || item.kind === "tool" && item.status === "stopped"
        || item.kind === "notice" && (item.action === "recover_context"
          || index > answerIndex && !item.decisionReceipt && !item.completionSummary));
      const members = current.items.flatMap(item => item.kind === "assistant"
        ? [...(item !== answer ? [item.id] : []), `${item.id}:reasoning`]
        : item.kind === "notice" && (item.level === "warn" || item.action === "recover_context")
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
        add({ kind: item.kind, key: item.id, turnKey, item } as ItemNode, !(item.kind === "tool" && item.parentId));
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
        latest: current === groups[groups.length - 1], presentedFiles: stablePresented, modifiedFiles: stableModified });
    }
    for (const key of this.nodes.keys()) if (!present.has(key)) { this.nodes.delete(key); this.dirty.add(key); }
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
    for (const key of this.opened) if (!groups.some(group => group.key === key)) this.opened.delete(key);
    if (!sameKeys(this.order, order)) { this.order = order; this.orderDirty = true; }
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
    this.nodes.clear(); this.children.clear(); this.order = []; this.input = undefined;
  }
}
