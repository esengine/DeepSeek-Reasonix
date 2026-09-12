import { TranscriptViewportWriter } from "./transcriptViewportWriter";

type Anchor = { key: string; top: number; turn?: string; previous: string[] };
type Position = { following: boolean; anchor?: Anchor };
const positions = new Map<string, Position>();
let generation = 0;

/** One native writer, no geometry state in React and no recursive measurement publication. */
export class ChatScrollController {
  readonly generation = ++generation;
  private writer = new TranscriptViewportWriter();
  private element?: HTMLElement;
  private content?: HTMLElement;
  private observer?: ResizeObserver;
  private mutations?: MutationObserver;
  private observedRows = new Set<HTMLElement>();
  private anchor?: Anchor;
  private following = true;
  private observedTop = 0;
  private userScrollUntil = 0;
  private disposed = false;
  private opened = false;
  private frame = 0;
  private layoutFrame = 0;
  private attachment = 0;
  private transaction = 0;
  private listeners = new Set<() => void>();
  private snapshot = { following: true, activeKey: "" };
  constructor(readonly sessionKey: string) {}
  getSnapshot = () => this.snapshot;
  subscribe = (listener: () => void) => { this.listeners.add(listener); return () => { this.listeners.delete(listener); }; };
  private publish() {
    const activeKey = this.anchor?.turn ?? this.anchor?.key ?? "";
    if (this.snapshot.following === this.following && this.snapshot.activeKey === activeKey) return;
    this.snapshot = { following: this.following, activeKey };
    this.listeners.forEach(listener => listener());
  }
  attach(element: HTMLElement, content: HTMLElement) {
    const attachment = ++this.attachment;
    this.disposed = false;
    this.element = element;
    this.content = content;
    this.writer.attach(element, this.generation);
    element.addEventListener("scroll", this.onScroll, { passive: true });
    element.addEventListener("wheel", this.onWheel, { passive: true });
    element.addEventListener("touchstart", this.onRead, { passive: true });
    element.addEventListener("keydown", this.onKey);
    element.addEventListener("pointerdown", this.onPointer, { passive: true });
    if (typeof ResizeObserver !== "undefined") {
      this.observer = new ResizeObserver(() => { if (!this.disposed && attachment === this.attachment) this.scheduleLayout(); });
      this.observer.observe(content);
      this.observer.observe(element);
    }
    if (typeof MutationObserver !== "undefined") {
      this.mutations = new MutationObserver(() => {
        if (this.disposed || attachment !== this.attachment) return;
        const rows = new Set(this.rows());
        for (const row of this.observedRows) if (!rows.has(row)) this.observer?.unobserve(row);
        for (const row of rows) if (!this.observedRows.has(row)) this.observer?.observe(row);
        this.observedRows = rows;
        this.scheduleLayout();
      });
      // Chat nodes are direct children of the column. Internal Markdown and
      // tool-body mutations are already covered by the row ResizeObserver;
      // observing the full subtree would rescan every loaded row for each
      // worker parse and turns cumulative history loading into quadratic work.
      this.mutations.observe(content, { childList: true });
    }
  }
  private scheduleLayout() {
    if (this.layoutFrame) return;
    const attachment = this.attachment;
    this.layoutFrame = requestAnimationFrame(() => {
      this.layoutFrame = 0;
      if (!this.disposed && attachment === this.attachment) this.layout();
    });
  }
  ready() {
    if (this.opened || !this.element) return;
    this.opened = true;
    const saved = positions.get(this.sessionKey);
    this.following = saved?.following ?? true;
    this.anchor = saved?.anchor;
    this.layout();
  }
  private rows() {
    const content = this.content;
    if (!content) return [];
    return Array.from(content.children).filter((row): row is HTMLElement =>
      row instanceof HTMLElement && row.hasAttribute("data-chat-anchor-key") && row.childNodes.length > 0);
  }
  private capture() {
    const el = this.element;
    if (!el) return;
    const top = el.getBoundingClientRect().top;
    const rows = this.rows();
    let low = 0, high = rows.length;
    while (low < high) {
      const middle = (low + high) >>> 1;
      if (rows[middle].getBoundingClientRect().bottom <= top + 1) low = middle + 1; else high = middle;
    }
    const index = Math.min(low, rows.length - 1);
    const row = rows[index];
    if (row) this.anchor = { key: row.dataset.chatAnchorKey!, top: row.getBoundingClientRect().top - top,
      turn: row.dataset.chatTurn, previous: rows.slice(0, index).map(row => row.dataset.chatAnchorKey!) };
  }
  private write(offset: number, owner: "tail-follow" | "restore" = "restore") {
    if (this.disposed || !this.element) return;
    this.writer.write({ session: this.sessionKey, generation: this.generation, transactionId: ++this.transaction,
      geometryRevision: 0, owner, intent: this.following ? "tail" : "reader", offset });
    this.observedTop = this.element.scrollTop;
  }
  layout() {
    const el = this.element;
    if (!el || !this.opened || this.disposed) return;
    if (this.following) this.write(el.scrollHeight, "tail-follow");
    else if (this.anchor) {
      const rows = this.rows();
      let row = rows.find(row => row.dataset.chatAnchorKey === this.anchor!.key);
      if (!row) row = rows.find(row => row.dataset.chatAnchorKey === `${this.anchor!.turn}:process`);
      if (!row) {
        const previous = new Set(this.anchor.previous);
        row = [...rows].reverse().find(row => previous.has(row.dataset.chatAnchorKey!)) ?? rows[0];
      }
      if (row) this.write(el.scrollTop + row.getBoundingClientRect().top - el.getBoundingClientRect().top - this.anchor.top);
    }
    this.capture(); this.save(); this.publish();
  }
  private save() {
    positions.delete(this.sessionKey);
    positions.set(this.sessionKey, { following: this.following, anchor: this.anchor });
    // Same lifetime policy as inactive view caches; contains identities, never message bodies.
    if (positions.size > 100) positions.delete(positions.keys().next().value!);
  }
  toBottom = () => { this.userScrollUntil = 0; this.following = true; this.anchor = undefined; this.layout(); };
  jump = (key: string) => {
    const el = this.element;
    const row = this.rows().find(row => row.dataset.chatAnchorKey === key);
    if (!el || !row) return;
    this.following = false;
    this.write(el.scrollTop + row.getBoundingClientRect().top - el.getBoundingClientRect().top);
    this.capture(); this.save(); this.publish();
  };
  beforeChange = () => { if (!this.following) this.capture(); };
  private onRead = () => { this.userScrollUntil = performance.now() + 1000; this.following = false; this.capture(); this.save(); this.publish(); };
  private onWheel = (event: WheelEvent) => { this.userScrollUntil = performance.now() + 1000; if (event.deltaY < 0) this.onRead(); };
  private onKey = (event: KeyboardEvent) => { if (["ArrowUp", "ArrowDown", "PageUp", "PageDown", "Home", "End", " "].includes(event.key)) this.onRead(); };
  private onPointer = (event: PointerEvent) => {
    const el = this.element;
    if (el && (event.target === el || event.shiftKey)) this.onRead();
  };
  private onScroll = () => {
    const el = this.element;
    if (!el || this.disposed) return;
    const floor = Math.max(0, el.scrollHeight - el.clientHeight);
    if (Math.abs(el.scrollTop - Math.min(this.observedTop, floor)) > 0.5) {
      // WebKit can emit a native clamp after asynchronous content replacement.
      // Only actual input may change reader intent; layout is not user input.
      if (!this.following || performance.now() < this.userScrollUntil) {
        this.following = el.scrollTop > this.observedTop && floor - el.scrollTop <= 24;
        this.capture(); this.save();
      } else this.scheduleLayout();
    }
    this.observedTop = el.scrollTop;
    if (!this.frame) this.frame = requestAnimationFrame(() => { this.frame = 0; if (!this.disposed) this.publish(); });
  };
  dispose() {
    this.attachment++;
    this.save(); this.disposed = true;
    const el = this.element;
    el?.removeEventListener("scroll", this.onScroll);
    el?.removeEventListener("wheel", this.onWheel);
    el?.removeEventListener("touchstart", this.onRead);
    el?.removeEventListener("keydown", this.onKey);
    el?.removeEventListener("pointerdown", this.onPointer);
    this.observer?.disconnect();
    this.mutations?.disconnect(); this.observedRows.clear();
    cancelAnimationFrame(this.frame); this.frame = 0;
    cancelAnimationFrame(this.layoutFrame); this.layoutFrame = 0;
    this.writer.attach(null, ++generation);
    this.element = undefined; this.content = undefined; this.opened = false; this.listeners.clear();
  }
}
