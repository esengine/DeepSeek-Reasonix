import { statSync } from "node:fs";
import { isAbsolute } from "node:path";
import type { DocumentBinding, DocumentRegistry, FrameBinding } from "./documents.js";
import { noGrant, staleReference, takenOver } from "./errors.js";
import type { GuestFrame, GuestPage } from "./guestView.js";
import { chordEvents, parseKeySequence } from "./keys.js";
import { FOCUS_SCRIPT_SOURCE, IDENTITY_SCRIPT_SOURCE, scriptCall, SELECT_SCRIPT_SOURCE, type ResolvedElement, type SelectOutput } from "./pageScripts.js";
import { frameForRef, locateRef, resolveRef } from "./refResolver.js";
import { ISOLATED_WORLD, REGISTRY_KEY, runInFrame } from "./snapshot.js";
import type { BrowserSurfaceManager, BrowserTab } from "./surfaceManager.js";
import { uploadFiles } from "./upload.js";

export const ACT_SETTLE_MS = 150;

export interface ActRequest {
  operationId: string;
  tabId: string;
  documentToken: string;
  action: string;
  ref: string;
  text: string;
  keys: string;
  options: string[];
  files: string[];
  submit: boolean;
  deltaX: number;
  deltaY: number;
}

export interface ActResult {
  executed: boolean;
  outcome?: "unknown";
  reason?: string;
  documentToken?: string;
}

export interface ActionDeps {
  surfaces: BrowserSurfaceManager;
  documents: DocumentRegistry;
  settleMs?: number;
  sleep?(ms: number): Promise<void>;
  fileExists?(path: string): boolean;
}

interface Point {
  x: number;
  y: number;
}

const defaultSleep = (ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms));
const defaultFileExists = (path: string) => {
  try {
    return statSync(path).isFile();
  } catch {
    return false;
  }
};

export class ActionExecutor {
  private readonly sleep: (ms: number) => Promise<void>;
  private readonly fileExists: (path: string) => boolean;

  constructor(private readonly deps: ActionDeps) {
    this.sleep = deps.sleep ?? defaultSleep;
    this.fileExists = deps.fileExists ?? defaultFileExists;
  }

  // verify re-checks the grant at every checkpoint, so a revoke or take-over
  // that lands while the action is still resolving cancels it before any
  // input is dispatched; once dispatched, the receipt is honest about it.
  async act(tab: BrowserTab, request: ActRequest, verify: () => void): Promise<ActResult> {
    const binding = this.deps.documents.lookup(request.documentToken);
    if (!binding || binding.tabId !== tab.id) throw staleReference("documentToken is not current for this tab");
    this.checkpoint(tab, binding, verify);
    const page = tab.view.page;
    let outcome: ActResult;
    let dispatched = false;
    const dispatch = () => { dispatched = true; };
    try {
      switch (request.action) {
        case "click":
          outcome = await this.click(tab, binding, request, verify, dispatch);
          break;
        case "type":
          outcome = await this.type(tab, binding, request, verify, dispatch);
          break;
        case "press":
          outcome = await this.press(tab, binding, request, verify, dispatch);
          break;
        case "scroll":
          outcome = await this.scroll(tab, binding, request, verify, dispatch);
          break;
        case "select":
          outcome = await this.select(tab, binding, request, verify, dispatch, () => { dispatched = false; });
          break;
        case "upload":
          outcome = await this.upload(tab, binding, request, verify, dispatch);
          break;
        default:
          return { executed: false, reason: `unsupported action ${JSON.stringify(request.action)}`, documentToken: request.documentToken };
      }
    } catch (error) {
      // A checkpoint failure after any dispatch cannot prove zero effects.
      // Preserve uncertainty so the durable ledger never permits a replay.
      if (dispatched) return { executed: false, outcome: "unknown", reason: String(error) };
      throw error;
    }
    if (!outcome.executed) return { ...outcome, documentToken: request.documentToken };
    await this.sleep(this.deps.settleMs ?? ACT_SETTLE_MS);
    if (tab.epoch !== binding.epoch || page.isDestroyed()) return { executed: true };
    let same = false;
    try {
      same = (await page.executeJavaScriptInIsolatedWorld(ISOLATED_WORLD, [{ code: scriptCall(IDENTITY_SCRIPT_SOURCE, { key: REGISTRY_KEY, docId: binding.frames[0]?.docId ?? "" }) }])) === true;
    } catch {
      same = false;
    }
    if (!same) return { executed: true };
    const token = this.deps.documents.rotate(request.documentToken);
    return token ? { executed: true, documentToken: token } : { executed: true };
  }

  private checkpoint(tab: BrowserTab, binding: DocumentBinding, verify: () => void): void {
    verify();
    if (this.deps.surfaces.get(tab.id) !== tab) throw noGrant(`browser tab ${tab.id} is closed`);
    if (tab.mode !== "agent") throw takenOver(`tab ${tab.id} is in human mode`);
    if (tab.epoch !== binding.epoch) throw staleReference(`tab ${tab.id} changed since the snapshot (epoch ${binding.epoch} → ${tab.epoch})`);
    if (tab.view.page.isDestroyed()) throw staleReference(`tab ${tab.id} has no page`);
  }

  private async target(tab: BrowserTab, binding: DocumentBinding, ref: string, verify: () => void): Promise<{ ok: true; element: ResolvedElement; centre: Point; frame: GuestFrame; binding: FrameBinding } | { ok: false; reason: string }> {
    if (ref === "") return { ok: false, reason: "this action needs a ref" };
    const resolved = await resolveRef(tab.view.page, binding, ref, true);
    if (!resolved.ok) return resolved;
    this.checkpoint(tab, binding, verify);
    const zoom = tab.view.page.getZoomFactor() || 1;
    const { element } = resolved.value;
    const centre = { x: Math.round((element.x + element.width / 2) * zoom), y: Math.round((element.y + element.height / 2) * zoom) };
    return { ok: true, element, centre, frame: resolved.value.frame, binding: resolved.value.binding };
  }

  private mouseClick(tab: BrowserTab, at: Point): void {
    const page = tab.view.page;
    this.deps.surfaces.markAgentInput(tab);
    page.sendInputEvent({ type: "mouseMove", x: at.x, y: at.y });
    page.sendInputEvent({ type: "mouseDown", x: at.x, y: at.y, button: "left", clickCount: 1 });
    page.sendInputEvent({ type: "mouseUp", x: at.x, y: at.y, button: "left", clickCount: 1 });
  }

  private sendKeys(tab: BrowserTab, keys: string): string | null {
    let chords;
    try {
      chords = parseKeySequence(keys);
    } catch (error) {
      return error instanceof Error ? error.message : String(error);
    }
    if (chords.length === 0) return "no keys given";
    this.deps.surfaces.markAgentInput(tab);
    for (const chord of chords) {
      for (const event of chordEvents(chord)) tab.view.page.sendInputEvent(event);
    }
    return null;
  }

  private async click(tab: BrowserTab, binding: DocumentBinding, request: ActRequest, verify: () => void, dispatch: () => void): Promise<ActResult> {
    const target = await this.target(tab, binding, request.ref, verify);
    if (!target.ok) return { executed: false, reason: target.reason };
    if (target.element.disabled) return { executed: false, reason: "element is disabled" };
    if (target.element.tag === "option") return { executed: false, reason: "use the select action for <option> elements" };
    dispatch();
    this.mouseClick(tab, target.centre);
    return { executed: true };
  }

  private async type(tab: BrowserTab, binding: DocumentBinding, request: ActRequest, verify: () => void, dispatch: () => void): Promise<ActResult> {
    const target = await this.target(tab, binding, request.ref, verify);
    if (!target.ok) return { executed: false, reason: target.reason };
    if (target.element.disabled) return { executed: false, reason: "element is disabled" };
    if (!target.element.editable) return { executed: false, reason: "element is not editable" };
    dispatch();
    this.mouseClick(tab, target.centre);
    await this.sleep(30);
    this.checkpoint(tab, binding, verify);
    this.deps.surfaces.markAgentInput(tab);
    if (request.text !== "") await tab.view.page.insertText(request.text);
    if (request.submit) {
      this.checkpoint(tab, binding, verify);
      const failure = this.sendKeys(tab, "Enter");
      if (failure) return { executed: false, reason: failure };
    }
    return { executed: true };
  }

  private async press(tab: BrowserTab, binding: DocumentBinding, request: ActRequest, verify: () => void, dispatch: () => void): Promise<ActResult> {
    if (request.keys.trim() === "") return { executed: false, reason: "press needs keys" };
    // Validate before focusing: focusing itself dispatches a physical click.
    try { parseKeySequence(request.keys); } catch (error) { return { executed: false, reason: String(error) }; }
    if (request.ref !== "") {
      const target = await this.target(tab, binding, request.ref, verify);
      if (!target.ok) return { executed: false, reason: target.reason };
      if (target.element.editable) {
        dispatch();
        this.mouseClick(tab, target.centre);
        await this.sleep(30);
        this.checkpoint(tab, binding, verify);
      } else {
        this.checkpoint(tab, binding, verify);
        const focused = await runInFrame(tab.view.page, target.frame, scriptCall(FOCUS_SCRIPT_SOURCE, {
          key: REGISTRY_KEY, snapshotId: binding.snapshotId, docId: target.binding.docId, ref: request.ref,
        }));
        if (focused !== true) return { executed: false, reason: "element could not be focused" };
        this.checkpoint(tab, binding, verify);
      }
    }
    dispatch();
    const failure = this.sendKeys(tab, request.keys);
    return failure ? { executed: false, reason: failure } : { executed: true };
  }

  // Blink negates WebMouseWheelEvent deltas when it builds the DOM WheelEvent,
  // so the request keeps DOM semantics (positive deltaY scrolls down) and the
  // sign flips here.
  private async scroll(tab: BrowserTab, binding: DocumentBinding, request: ActRequest, verify: () => void, dispatch: () => void): Promise<ActResult> {
    let at: Point;
    if (request.ref !== "") {
      const target = await this.target(tab, binding, request.ref, verify);
      if (!target.ok) return { executed: false, reason: target.reason };
      at = target.centre;
    } else {
      const size = await this.viewport(tab);
      at = { x: Math.round(size.width / 2), y: Math.round(size.height / 2) };
      this.checkpoint(tab, binding, verify);
    }
    if (!Number.isFinite(request.deltaX) || !Number.isFinite(request.deltaY)) return { executed: false, reason: "scroll deltas must be numbers" };
    if (request.deltaX === 0 && request.deltaY === 0) return { executed: false, reason: "scroll deltas are both zero" };
    this.deps.surfaces.markAgentInput(tab);
    dispatch();
    tab.view.page.sendInputEvent({ type: "mouseWheel", x: at.x, y: at.y, deltaX: -request.deltaX, deltaY: -request.deltaY, canScroll: true });
    return { executed: true };
  }

  private async viewport(tab: BrowserTab): Promise<{ width: number; height: number }> {
    const zoom = tab.view.page.getZoomFactor() || 1;
    const raw = await tab.view.page.executeJavaScriptInIsolatedWorld(ISOLATED_WORLD, [{ code: "({ width: window.innerWidth, height: window.innerHeight })" }]);
    const size = typeof raw === "object" && raw !== null ? (raw as { width?: unknown; height?: unknown }) : {};
    return { width: (typeof size.width === "number" ? size.width : 0) * zoom, height: (typeof size.height === "number" ? size.height : 0) * zoom };
  }

  private async select(tab: BrowserTab, binding: DocumentBinding, request: ActRequest, verify: () => void, dispatch: () => void, noEffects: () => void): Promise<ActResult> {
    if (request.ref === "") return { executed: false, reason: "select needs a ref" };
    const page: GuestPage = tab.view.page;
    const target = frameForRef(page, binding, request.ref);
    const code = scriptCall(SELECT_SCRIPT_SOURCE, { key: REGISTRY_KEY, snapshotId: binding.snapshotId, docId: target.binding.docId, ref: request.ref, options: request.options });
    let raw: unknown;
    this.checkpoint(tab, binding, verify);
    dispatch();
    try {
      raw = await runInFrame(page, target.frame, code);
    } catch (error) {
      throw staleReference(`frame of ${request.ref} cannot run scripts: ${String(error)}`);
    }
    const out = raw as SelectOutput | null;
    if (!out || typeof out.ok !== "boolean") throw staleReference("select script returned nothing");
    if (!out.ok) {
      noEffects();
      if (out.reason === "stale") throw staleReference(`${request.ref} predates the current document`);
      return { executed: false, reason: out.reason };
    }
    return { executed: true };
  }

  private async upload(tab: BrowserTab, binding: DocumentBinding, request: ActRequest, verify: () => void, dispatch: () => void): Promise<ActResult> {
    if (request.files.length === 0) return { executed: false, reason: "upload needs files" };
    for (const file of request.files) {
      if (!isAbsolute(file)) return { executed: false, reason: `file path must be absolute: ${file}` };
      if (!this.fileExists(file)) return { executed: false, reason: `file not found: ${file}` };
    }
    if (request.ref === "") return { executed: false, reason: "upload needs a ref" };
    const located = await locateRef(tab.view.page, binding, request.ref);
    if (!located.ok) return { executed: false, reason: located.reason };
    this.checkpoint(tab, binding, verify);
    return uploadFiles(tab.view.page, located.value, request.files, () => this.checkpoint(tab, binding, verify), dispatch);
  }
}
