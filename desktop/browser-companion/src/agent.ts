// Restricted agent control for remote tabs. The model never supplies
// JavaScript: observations come from Chromium's accessibility tree and input
// is dispatched through DOM/Input CDP domains using opaque, generation-bound
// refs. Human input and sensitive fields revoke the lease immediately.

import { randomBytes } from "node:crypto";
import type { NativeImage, WebContents } from "electron";
import { ProtocolError } from "./protocol";
import { BROWSER_LIMITS } from "./generated/browserProtocol.generated";
import { TabManager, type TabRecord } from "./tabs";

type Params = Record<string, unknown>;

interface AXValue {
  value?: unknown;
}

interface AXNode {
  nodeId?: string;
  backendDOMNodeId?: number;
  ignored?: boolean;
  role?: AXValue;
  name?: AXValue;
  description?: AXValue;
  value?: AXValue;
}

interface RefTarget {
  generation: number;
  backendNodeId: number;
}

interface RefSnapshot {
  generation: number;
  refs: Map<string, RefTarget>;
}

const INTERACTIVE_ROLES = new Set([
  "button",
  "checkbox",
  "combobox",
  "link",
  "listbox",
  "menuitem",
  "option",
  "radio",
  "searchbox",
  "slider",
  "spinbutton",
  "switch",
  "tab",
  "textbox",
]);

const TEXT_ROLES = new Set([
  "heading",
  "link",
  "listitem",
  "paragraph",
  "StaticText",
  "InlineTextBox",
  "textbox",
]);

const ALLOWED_KEYS = new Set([
  "ArrowDown",
  "ArrowLeft",
  "ArrowRight",
  "ArrowUp",
  "Backspace",
  "Delete",
  "End",
  "Enter",
  "Escape",
  "Home",
  "PageDown",
  "PageUp",
  "Space",
  "Tab",
]);

export class AgentController {
  private refsByTab = new Map<string, RefSnapshot>();
  private abortByRequest = new Map<string, AbortController>();
  private attached = new WeakSet<WebContents>();

  constructor(private tabs: TabManager) {}

  cancel(requestId: string): void {
    this.abortByRequest.get(requestId)?.abort();
  }

  async snapshot(params: Params): Promise<unknown> {
    const { ownerId, tabId, record, wc } = this.target(params);
    this.ensureAttached(ownerId, tabId, wc);
    this.tabs.acquireLease(ownerId, tabId);
    const requested = integerParam(params, "maxChars", BROWSER_LIMITS.maxTextChars);
    const maxChars = Math.max(1, Math.min(requested, BROWSER_LIMITS.maxTextChars));
    const response = (await wc.debugger.sendCommand("Accessibility.getFullAXTree")) as {
      nodes?: AXNode[];
    };
    const nodes = response.nodes ?? [];
    const refs = new Map<string, RefTarget>();
    const treeLines: string[] = [];
    const textParts: string[] = [];
    for (const node of nodes) {
      if (node.ignored) continue;
      const role = axString(node.role);
      const name = axString(node.name);
      const description = axString(node.description);
      const value = axString(node.value);
      if (TEXT_ROLES.has(role) && name) textParts.push(name);
      let ref = "";
      if (INTERACTIVE_ROLES.has(role) && node.backendDOMNodeId !== undefined) {
        ref = `r-${randomBytes(9).toString("base64url")}`;
        refs.set(ref, { generation: record.generation, backendNodeId: node.backendDOMNodeId });
      }
      const fields = [role || "node"];
      if (ref) fields.push(`ref=${ref}`);
      if (name) fields.push(`name=${JSON.stringify(name)}`);
      if (value && value !== name) fields.push(`value=${JSON.stringify(value)}`);
      if (description) fields.push(`description=${JSON.stringify(description)}`);
      treeLines.push(fields.join(" "));
    }
    this.refsByTab.set(tabId, { generation: record.generation, refs });
    const combined = boundSnapshot(treeLines.join("\n"), textParts.join("\n"), maxChars);
    return {
      ...tabResult(record),
      snapshot: combined,
    };
  }

  async screenshot(params: Params): Promise<unknown> {
    const { ownerId, tabId, record, wc } = this.target(params);
    this.tabs.acquireLease(ownerId, tabId);
    const wasThrottled = wc.getBackgroundThrottling();
    let image: NativeImage;
    try {
      // Chromium can reject a hidden surface with UnknownVizError until its
      // compositor has produced a frame (especially under Linux/Xvfb). Wake
      // the renderer only for the bounded capture window, then restore the
      // normal hidden-tab throttling policy.
      if (wasThrottled) wc.setBackgroundThrottling(false);
      await delay(16);
      image = await capturePageWithRetry(wc);
    } finally {
      if (!wc.isDestroyed() && wasThrottled) wc.setBackgroundThrottling(true);
    }
    let png = image.toPNG();
    // PNG bytes are capped separately from the 16 MiB wire-frame budget.
    while (png.length > BROWSER_LIMITS.maxScreenshotBytes && image.getSize().width > 320) {
      const size = image.getSize();
      image = image.resize({ width: Math.max(320, Math.floor(size.width * 0.8)) });
      png = image.toPNG();
    }
    if (png.length > BROWSER_LIMITS.maxScreenshotBytes) {
      throw new ProtocolError("frame_too_large", "screenshot exceeds byte budget");
    }
    const size = image.getSize();
    return {
      ...tabResult(record),
      width: size.width,
      height: size.height,
      imageDataUrl: `data:image/png;base64,${png.toString("base64")}`,
    };
  }

  async wait(requestId: string, params: Params): Promise<unknown> {
    const { record, wc } = this.target(params);
    const waitUntil = stringParam(params, "waitUntil");
    if (!["load", "network_idle", "dom_content_loaded", "navigation"].includes(waitUntil)) {
      throw new ProtocolError("invalid_params", "invalid waitUntil");
    }
    const timeoutMs = Math.max(1, Math.min(integerParam(params, "timeoutMs", 30_000), 30_000));
    if (waitUntil !== "navigation" && !wc.isLoading()) return tabResult(record);
    const controller = new AbortController();
    this.abortByRequest.set(requestId, controller);
    try {
      await waitForWebContents(wc, waitUntil, timeoutMs, controller.signal);
      return tabResult(record);
    } finally {
      this.abortByRequest.delete(requestId);
    }
  }

  async act(params: Params): Promise<unknown> {
    const { ownerId, tabId, record, wc } = this.target(params);
    const expectedOrigin = stringParam(params, "expectedOrigin");
    const expectedGeneration = record.generation;
    this.ensureCurrentTarget(wc, record, expectedGeneration, expectedOrigin);
    this.ensureAttached(ownerId, tabId, wc);
    this.tabs.acquireLease(ownerId, tabId);
    const action = stringParam(params, "action");
    switch (action) {
      case "click":
      case "hover": {
        const target = this.resolveRef(tabId, record, stringParam(params, "ref"));
        const { x, y } = await nodeCenter(wc, target.backendNodeId);
        this.ensureCurrentTarget(wc, record, expectedGeneration, expectedOrigin);
        if (action === "click") {
          await wc.debugger.sendCommand("Input.dispatchMouseEvent", { type: "mousePressed", x, y, button: "left", clickCount: 1 });
          await wc.debugger.sendCommand("Input.dispatchMouseEvent", { type: "mouseReleased", x, y, button: "left", clickCount: 1 });
        } else {
          await wc.debugger.sendCommand("Input.dispatchMouseEvent", { type: "mouseMoved", x, y });
        }
        break;
      }
      case "scroll": {
        const delta = Math.max(-10_000, Math.min(integerParam(params, "delta", 0), 10_000));
        if (delta === 0) throw new ProtocolError("invalid_params", "scroll delta is required");
        this.ensureCurrentTarget(wc, record, expectedGeneration, expectedOrigin);
        await wc.debugger.sendCommand("Input.dispatchMouseEvent", { type: "mouseWheel", x: 1, y: 1, deltaX: 0, deltaY: delta });
        break;
      }
      case "type": {
        const target = this.resolveRef(tabId, record, stringParam(params, "ref"));
        await this.rejectSensitiveField(ownerId, tabId, wc, target.backendNodeId);
        const text = stringParam(params, "text");
        this.ensureCurrentTarget(wc, record, expectedGeneration, expectedOrigin);
        await wc.debugger.sendCommand("DOM.focus", { backendNodeId: target.backendNodeId });
        this.ensureCurrentTarget(wc, record, expectedGeneration, expectedOrigin);
        await wc.debugger.sendCommand("Input.insertText", { text });
        break;
      }
      case "press": {
        const key = stringParam(params, "key");
        if (!ALLOWED_KEYS.has(key)) throw new ProtocolError("invalid_params", `key ${JSON.stringify(key)} is not allowed`);
        this.ensureCurrentTarget(wc, record, expectedGeneration, expectedOrigin);
        await wc.debugger.sendCommand("Input.dispatchKeyEvent", { type: "keyDown", key });
        await wc.debugger.sendCommand("Input.dispatchKeyEvent", { type: "keyUp", key });
        break;
      }
      case "select": {
        const target = this.resolveRef(tabId, record, stringParam(params, "ref"));
        await this.rejectSensitiveField(ownerId, tabId, wc, target.backendNodeId);
        const value = stringParam(params, "value");
        if (!value) throw new ProtocolError("invalid_params", "select value is required");
        this.ensureCurrentTarget(wc, record, expectedGeneration, expectedOrigin);
        await wc.debugger.sendCommand("DOM.focus", { backendNodeId: target.backendNodeId });
        this.ensureCurrentTarget(wc, record, expectedGeneration, expectedOrigin);
        // Native selects implement incremental keyboard search; dispatch char
        // events instead of mutating DOM/JavaScript state directly.
        for (const char of value) {
          await wc.debugger.sendCommand("Input.dispatchKeyEvent", { type: "char", key: char, text: char });
        }
        await wc.debugger.sendCommand("Input.dispatchKeyEvent", { type: "keyDown", key: "Enter" });
        await wc.debugger.sendCommand("Input.dispatchKeyEvent", { type: "keyUp", key: "Enter" });
        break;
      }
      default:
        throw new ProtocolError("invalid_params", `unsupported action ${JSON.stringify(action)}`);
    }
    return tabResult(record);
  }

  private target(params: Params): { ownerId: string; tabId: string; record: TabRecord; wc: WebContents } {
    const ownerId = requiredString(params, "ownerId");
    const tabId = requiredString(params, "tabId");
    const target = this.tabs.targetForAgent(ownerId, tabId);
    return { ownerId, tabId, record: target.record, wc: target.view.webContents };
  }

  private ensureAttached(ownerId: string, tabId: string, wc: WebContents): void {
    if (!wc.debugger.isAttached()) {
      try {
        wc.debugger.attach("1.3");
      } catch (err) {
        throw new ProtocolError("tab_busy", `cannot attach browser controller: ${(err as Error).message}`);
      }
    }
    if (!this.attached.has(wc)) {
      this.attached.add(wc);
      wc.debugger.once("detach", (_event, reason) => {
        this.attached.delete(wc);
        this.refsByTab.delete(tabId);
        this.tabs.emitAgentEvent("cdp.detach", ownerId, { ownerId, tabId, reason });
        this.tabs.revokeLease(ownerId, tabId, "user", `CDP detached: ${reason}`);
      });
    }
  }

  private ensureCurrentTarget(wc: WebContents, record: TabRecord, generation: number, expectedOrigin: string): void {
    this.ensureOrigin(wc.getURL(), expectedOrigin);
    if (record.generation !== generation) {
      throw new ProtocolError("stale_ref", "tab navigated while the action was being prepared");
    }
  }

  private ensureOrigin(url: string, expected: string): void {
    let actual: string;
    try {
      actual = new URL(url).origin;
    } catch {
      throw new ProtocolError("stale_ref", "tab no longer has an http(s) origin");
    }
    if (!expected || expected !== actual) {
      throw new ProtocolError("stale_ref", `expected origin ${JSON.stringify(expected)} does not match ${actual}`);
    }
  }

  private resolveRef(tabId: string, record: TabRecord, ref: string): RefTarget {
    const snapshot = this.refsByTab.get(tabId);
    const target = snapshot?.refs.get(ref);
    if (!ref || !snapshot || snapshot.generation !== record.generation || !target || target.generation !== record.generation) {
      throw new ProtocolError("stale_ref", "ref is missing or belongs to an older page generation");
    }
    return target;
  }

  private async rejectSensitiveField(ownerId: string, tabId: string, wc: WebContents, backendNodeId: number): Promise<void> {
    const result = (await wc.debugger.sendCommand("DOM.describeNode", { backendNodeId, depth: 0 })) as {
      node?: { nodeName?: string; attributes?: string[] };
    };
    const attrs = attributesToMap(result.node?.attributes ?? []);
    const type = (attrs.get("type") ?? "").toLowerCase();
    const autocomplete = (attrs.get("autocomplete") ?? "").toLowerCase();
    const sensitive =
      type === "password" ||
      /(?:cc-|one-time-code|current-password|new-password)/.test(autocomplete);
    if (!sensitive) return;
    this.tabs.revokeLease(ownerId, tabId, "sensitive_field", "sensitive field requires human input");
    throw new ProtocolError("user_takeover_required", "sensitive field requires human input");
  }
}

const CAPTURE_RETRY_DELAYS_MS = [50, 100, 200, 400] as const;

async function capturePageWithRetry(wc: WebContents): Promise<NativeImage> {
  for (let attempt = 0; ; attempt += 1) {
    try {
      return await wc.capturePage(undefined, { stayHidden: true, stayAwake: true });
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      if (!/UnknownVizError/i.test(message)) {
        throw new ProtocolError("internal", `screenshot failed: ${message}`);
      }
      const waitMs = CAPTURE_RETRY_DELAYS_MS[attempt];
      if (waitMs === undefined) {
        throw new ProtocolError("internal", "screenshot compositor did not become ready");
      }
      await delay(waitMs);
    }
  }
}

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function tabResult(record: TabRecord): { tabId: string; url: string; title: string; generation: number } {
  return { tabId: record.id, url: record.url, title: record.title, generation: record.generation };
}

function axString(value: AXValue | undefined): string {
  return typeof value?.value === "string" ? value.value.replace(/\s+/g, " ").trim() : "";
}

function boundSnapshot(tree: string, text: string, maxChars: number): { tree: string; text: string; truncated: boolean } {
  const total = tree.length + text.length;
  if (total <= maxChars) return { tree, text, truncated: false };
  const treeBudget = Math.min(tree.length, Math.floor(maxChars * 0.7));
  const textBudget = Math.max(0, maxChars - treeBudget);
  return { tree: tree.slice(0, treeBudget), text: text.slice(0, textBudget), truncated: true };
}

async function nodeCenter(wc: WebContents, backendNodeId: number): Promise<{ x: number; y: number }> {
  const result = (await wc.debugger.sendCommand("DOM.getBoxModel", { backendNodeId })) as {
    model?: { border?: number[]; content?: number[] };
  };
  const quad = result.model?.border ?? result.model?.content;
  if (!quad || quad.length < 8) throw new ProtocolError("stale_ref", "element no longer has a visible box");
  const xs = [quad[0]!, quad[2]!, quad[4]!, quad[6]!];
  const ys = [quad[1]!, quad[3]!, quad[5]!, quad[7]!];
  return { x: xs.reduce((a, b) => a + b, 0) / 4, y: ys.reduce((a, b) => a + b, 0) / 4 };
}

function waitForWebContents(wc: WebContents, waitUntil: string, timeoutMs: number, signal: AbortSignal): Promise<void> {
  const event = waitUntil === "dom_content_loaded" ? "dom-ready" : waitUntil === "navigation" ? "did-navigate" : "did-stop-loading";
  const events = wc as unknown as NodeJS.EventEmitter;
  return new Promise((resolve, reject) => {
    const cleanup = (): void => {
      clearTimeout(timer);
      signal.removeEventListener("abort", onAbort);
      events.removeListener(event, onDone);
    };
    const onDone = (): void => {
      cleanup();
      resolve();
    };
    const onAbort = (): void => {
      cleanup();
      reject(new ProtocolError("cancelled", "wait cancelled"));
    };
    const timer = setTimeout(() => {
      cleanup();
      reject(new ProtocolError("timeout", `wait timed out after ${timeoutMs}ms`));
    }, timeoutMs);
    timer.unref();
    events.once(event, onDone);
    signal.addEventListener("abort", onAbort, { once: true });
  });
}

function attributesToMap(raw: string[]): Map<string, string> {
  const result = new Map<string, string>();
  for (let i = 0; i + 1 < raw.length; i += 2) result.set(raw[i]!.toLowerCase(), raw[i + 1]!);
  return result;
}

function stringParam(params: Params, key: string): string {
  return typeof params[key] === "string" ? params[key] as string : "";
}

function requiredString(params: Params, key: string): string {
  const value = stringParam(params, key);
  if (!value) throw new ProtocolError("invalid_params", `${key} is required`);
  return value;
}

function integerParam(params: Params, key: string, fallback: number): number {
  const value = params[key];
  if (value === undefined) return fallback;
  if (!Number.isSafeInteger(value)) throw new ProtocolError("invalid_params", `${key} must be an integer`);
  return value as number;
}
