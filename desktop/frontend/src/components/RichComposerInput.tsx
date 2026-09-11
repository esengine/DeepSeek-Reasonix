import {
  forwardRef,
  useImperativeHandle,
  useLayoutEffect,
  useMemo,
  useRef,
  type ClipboardEvent,
  type CSSProperties,
  type FormEvent,
  type KeyboardEvent,
  type MouseEvent,
} from "react";
import {
  invocationDisplayForCommand,
  replaceInvocationTextRange,
  sortComposerInvocations,
  type ComposerInvocation,
} from "../lib/invocationDisplay";
import { activeRefTokenRe } from "../lib/refToken";
import type { CommandInfo } from "../lib/types";
import { InvocationBadge } from "./InvocationBadge";

export type RichComposerSelection = {
  start: number;
  end: number;
  afterInvocationId?: string;
};

export type RichComposerChangeOrigin = {
  source: "browser" | "programmatic";
  inputType?: string;
  beforeSelection: RichComposerSelection;
  afterSelection: RichComposerSelection;
};

export type RichSlashQuery = {
  from: number;
  to: number;
  query: string;
};

export type RichComposerInputHandle = {
  focus: () => void;
  getSelection: () => RichComposerSelection;
  setSelectionRange: (start: number, end?: number, afterInvocationId?: string) => void;
  replaceRange: (value: string, start: number, end: number) => void;
  insertInvocation: (command: CommandInfo, query: RichSlashQuery) => void;
  scrollHeight: () => number;
};

export type DomSelectionRead =
  | { ok: true; selection: RichComposerSelection }
  | { ok: false };

type PendingSelection = RichComposerSelection | null;

type ComposerModel = {
  text: string;
  invocations: ComposerInvocation[];
};

type RenderedComposerModel = ComposerModel & {
  version: number;
};

type DomPoint = {
  node: Node;
  offset: number;
};

type EditSnapshot = {
  text: string;
  selection: RichComposerSelection;
  inputType: string;
  data: string | null;
};

const CARET_SENTINEL = "\u00A0";

function sameComposerModel(left: ComposerModel, right: ComposerModel | null): boolean {
  if (!right || left.text !== right.text || left.invocations.length !== right.invocations.length) return false;
  return left.invocations.every((item, index) => {
    const candidate = right.invocations[index];
    return item.id === candidate.id && item.offset === candidate.offset && item.command === candidate.command;
  });
}

function isHTMLElement(node: Node): node is HTMLElement {
  return node instanceof HTMLElement;
}

function isInvocationToken(node: Node): node is HTMLElement {
  return isHTMLElement(node) && Boolean(node.dataset.invocationId);
}

function isCaretAnchor(node: Node): node is HTMLElement {
  return isHTMLElement(node) && Boolean(node.dataset.composerCaretAnchor);
}

function isBreak(node: Node): node is HTMLElement {
  return isHTMLElement(node) && node.tagName === "BR";
}

/**
 * Shared DOM walk for model text, selection reads, and selection restores.
 *
 * Logical length rules:
 * - invocation tokens are zero-length atoms (children ignored)
 * - the first CARET_SENTINEL inside a caret anchor is zero-length
 * - remaining user text in the anchor counts
 * - <br> is one newline
 * - ordinary / nested text uses JavaScript UTF-16 offsets
 */
function walkComposerDom(
  root: Node,
  visitor: {
    onInvocation?: (id: string, element: HTMLElement) => boolean | void;
    onText?: (node: Text, start: number, end: number) => boolean | void;
    onBreak?: (element: HTMLElement) => boolean | void;
  },
): void {
  const visit = (node: Node, inAnchor: boolean, anchorState: { skippedSentinel: boolean }): boolean => {
    if (isInvocationToken(node)) {
      const id = node.dataset.invocationId;
      if (id && visitor.onInvocation?.(id, node)) return true;
      return false;
    }
    if (isCaretAnchor(node)) {
      const state = { skippedSentinel: false };
      for (const child of Array.from(node.childNodes)) {
        if (visit(child, true, state)) return true;
      }
      return false;
    }
    if (isBreak(node)) {
      return Boolean(visitor.onBreak?.(node));
    }
    if (node.nodeType === Node.TEXT_NODE) {
      const textNode = node as Text;
      const value = textNode.textContent ?? "";
      if (!value) return false;
      if (!inAnchor) {
        return Boolean(visitor.onText?.(textNode, 0, value.length));
      }
      let start = 0;
      if (!anchorState.skippedSentinel) {
        const sentinelAt = value.indexOf(CARET_SENTINEL);
        if (sentinelAt === 0) {
          anchorState.skippedSentinel = true;
          start = 1;
        } else if (sentinelAt > 0) {
          // Count text before the first sentinel, then skip the sentinel once.
          if (visitor.onText?.(textNode, 0, sentinelAt)) return true;
          anchorState.skippedSentinel = true;
          start = sentinelAt + 1;
        }
      }
      if (start < value.length) {
        return Boolean(visitor.onText?.(textNode, start, value.length));
      }
      return false;
    }
    if (isHTMLElement(node) || node.nodeType === Node.DOCUMENT_FRAGMENT_NODE) {
      for (const child of Array.from(node.childNodes)) {
        if (visit(child, inAnchor, anchorState)) return true;
      }
    }
    return false;
  };
  visit(root, false, { skippedSentinel: false });
}

function normalizeModelText(text: string): string {
  return text.replace(/\u00a0/g, " ");
}

export function modelFromDom(root: HTMLElement, known: Map<string, ComposerInvocation>): ComposerModel {
  let text = "";
  const invocations: ComposerInvocation[] = [];
  walkComposerDom(root, {
    onInvocation: (id) => {
      const invocation = known.get(id);
      if (invocation) invocations.push({ ...invocation, offset: text.length });
    },
    onText: (node, start, end) => {
      text += (node.textContent ?? "").slice(start, end);
    },
    onBreak: () => {
      text += "\n";
    },
  });
  return {
    text: normalizeModelText(text),
    invocations: sortComposerInvocations(invocations),
  };
}

function logicalLength(root: HTMLElement): number {
  return modelFromDom(root, new Map()).text.length;
}

function pointFromDom(
  root: HTMLElement,
  known: Map<string, ComposerInvocation>,
  node: Node,
  offset: number,
): { offset: number; afterInvocationId?: string } {
  // Build a range [root start, point) and measure with the same walk rules.
  // Walking a cloned fragment preserves nested structure (including caret anchors).
  const range = document.createRange();
  range.setStart(root, 0);
  try {
    range.setEnd(node, offset);
  } catch {
    return { offset: logicalLength(root) };
  }
  const fragment = range.cloneContents();
  const shell = document.createElement("div");
  shell.appendChild(fragment);
  const model = modelFromDom(shell, known);
  const lastInvocation = model.invocations[model.invocations.length - 1];
  return {
    offset: model.text.length,
    afterInvocationId: lastInvocation?.offset === model.text.length ? lastInvocation.id : undefined,
  };
}

export function selectionFromDom(root: HTMLElement, known: Map<string, ComposerInvocation>): DomSelectionRead {
  const selection = document.getSelection();
  if (
    !selection
    || selection.rangeCount === 0
    || !selection.anchorNode
    || !selection.focusNode
    || !root.contains(selection.anchorNode)
    || !root.contains(selection.focusNode)
  ) {
    return { ok: false };
  }
  const anchor = pointFromDom(root, known, selection.anchorNode, selection.anchorOffset);
  const focus = pointFromDom(root, known, selection.focusNode, selection.focusOffset);
  return {
    ok: true,
    selection: {
      start: Math.min(anchor.offset, focus.offset),
      end: Math.max(anchor.offset, focus.offset),
      afterInvocationId: selection.isCollapsed ? focus.afterInvocationId : undefined,
    },
  };
}

function locateDomPoint(
  root: HTMLElement,
  targetOffset: number,
  options: { afterInvocationId?: string } = {},
): DomPoint {
  if (options.afterInvocationId) {
    const token = Array.from(root.querySelectorAll<HTMLElement>("[data-invocation-id]"))
      .find((candidate) => candidate.dataset.invocationId === options.afterInvocationId);
    if (token?.parentNode) {
      const index = Array.prototype.indexOf.call(token.parentNode.childNodes, token);
      return { node: token.parentNode, offset: index + 1 };
    }
  }

  const total = logicalLength(root);
  let remaining = Math.max(0, Math.min(targetOffset, total));
  let found: DomPoint | null = null;
  let lastTextPoint: DomPoint | null = null;

  walkComposerDom(root, {
    onText: (node, start, end) => {
      const length = end - start;
      if (remaining <= length) {
        found = { node, offset: start + remaining };
        return true;
      }
      remaining -= length;
      lastTextPoint = { node, offset: end };
      return false;
    },
    onBreak: (element) => {
      if (remaining === 0) {
        // Caret sits on the break boundary: place before the BR when possible.
        if (element.parentNode) {
          const index = Array.prototype.indexOf.call(element.parentNode.childNodes, element);
          found = { node: element.parentNode, offset: index };
          return true;
        }
      }
      if (remaining <= 1) {
        if (element.parentNode) {
          const index = Array.prototype.indexOf.call(element.parentNode.childNodes, element);
          found = { node: element.parentNode, offset: index + 1 };
          return true;
        }
      }
      remaining -= 1;
      if (element.parentNode) {
        const index = Array.prototype.indexOf.call(element.parentNode.childNodes, element);
        lastTextPoint = { node: element.parentNode, offset: index + 1 };
      }
      return false;
    },
  });

  if (found) return found;
  if (lastTextPoint) return lastTextPoint;
  return { node: root, offset: root.childNodes.length };
}

export function setDomSelection(root: HTMLElement, target: RichComposerSelection) {
  const selection = document.getSelection();
  if (!selection) return;

  const total = logicalLength(root);
  const start = Math.max(0, Math.min(target.start, total));
  const end = Math.max(0, Math.min(target.end, total));
  const collapsed = start === end;

  if (collapsed && target.afterInvocationId) {
    const point = locateDomPoint(root, start, { afterInvocationId: target.afterInvocationId });
    const range = document.createRange();
    range.setStart(point.node, point.offset);
    range.collapse(true);
    selection.removeAllRanges();
    selection.addRange(range);
    return;
  }

  const startPoint = locateDomPoint(root, start);
  const endPoint = collapsed ? startPoint : locateDomPoint(root, end);
  const range = document.createRange();
  try {
    range.setStart(startPoint.node, startPoint.offset);
    range.setEnd(endPoint.node, endPoint.offset);
  } catch {
    range.selectNodeContents(root);
    range.collapse(false);
  }
  selection.removeAllRanges();
  selection.addRange(range);
}

/**
 * Recover a caret using the pre-edit selection as the edit locus.
 * Prefer this over a maximal prefix/suffix diff: repeated characters make
 * the longest common prefix claim the whole string and push the caret to the
 * end (e.g. insert "a" at offset 1 in "aaa" → "aaaa").
 */
function selectionAnchoredCaret(
  beforeText: string,
  afterText: string,
  from: number,
  to: number,
): number | null {
  const head = beforeText.slice(0, from);
  const tail = beforeText.slice(to);
  if (!afterText.startsWith(head)) return null;
  if (tail.length === 0) {
    // Caret was at (or replaced through) the end: everything after `head` is new.
    return afterText.length;
  }
  if (afterText.length < head.length + tail.length) return null;
  if (afterText.slice(afterText.length - tail.length) !== tail) return null;
  const middle = afterText.slice(head.length, afterText.length - tail.length);
  if (head + middle + tail !== afterText) return null;
  return head.length + middle.length;
}

export function recoverSelectionAfterEdit(
  before: EditSnapshot,
  afterText: string,
  fallback: RichComposerSelection,
): RichComposerSelection {
  const clamp = (value: number) => Math.max(0, Math.min(value, afterText.length));
  const collapsedAt = (offset: number): RichComposerSelection => {
    const caret = clamp(offset);
    return { start: caret, end: caret };
  };

  const { start, end } = before.selection;
  const from = Math.max(0, Math.min(start, end, before.text.length));
  const to = Math.max(from, Math.min(Math.max(start, end), before.text.length));
  const inputType = before.inputType;
  const data = before.data;
  const hasData = data !== null && data !== undefined && data.length > 0;

  const matches = (candidate: string) => candidate === afterText;

  if (
    hasData
    && (
      inputType === "insertText"
      || inputType === "insertCompositionText"
      || inputType === "insertFromPaste"
      || inputType === "insertFromDrop"
      || inputType === "insertReplacementText"
      || inputType === "insertFromYank"
      || inputType === ""
    )
  ) {
    const candidate = before.text.slice(0, from) + data + before.text.slice(to);
    if (matches(candidate)) return collapsedAt(from + data.length);
  }

  if (inputType === "insertLineBreak" || inputType === "insertParagraph") {
    const candidate = before.text.slice(0, from) + "\n" + before.text.slice(to);
    if (matches(candidate)) return collapsedAt(from + 1);
  }

  if (inputType === "deleteContentBackward" || inputType === "deleteByCut" || inputType === "deleteByDrag") {
    if (from === to) {
      const delFrom = Math.max(0, from - 1);
      const candidate = before.text.slice(0, delFrom) + before.text.slice(from);
      if (matches(candidate)) return collapsedAt(delFrom);
    } else {
      const candidate = before.text.slice(0, from) + before.text.slice(to);
      if (matches(candidate)) return collapsedAt(from);
    }
  }

  if (inputType === "deleteContentForward" || inputType === "deleteContent") {
    if (from === to) {
      const candidate = before.text.slice(0, from) + before.text.slice(Math.min(before.text.length, from + 1));
      if (matches(candidate)) return collapsedAt(from);
    } else {
      const candidate = before.text.slice(0, from) + before.text.slice(to);
      if (matches(candidate)) return collapsedAt(from);
    }
  }

  // Selection-anchored reconstruction: works for data=null replacement /
  // dictation / composition commits and for repeated-character inserts where a
  // pure prefix/suffix diff would jump to the end.
  const anchored = selectionAnchoredCaret(before.text, afterText, from, to);
  if (anchored !== null) return collapsedAt(anchored);

  // Collapsed delete without a reliable inputType (some WebView paths).
  if (from === to && afterText.length < before.text.length) {
    const delCount = before.text.length - afterText.length;
    const delFrom = Math.max(0, from - delCount);
    if (before.text.slice(0, delFrom) + before.text.slice(from) === afterText) {
      return collapsedAt(delFrom);
    }
    if (before.text.slice(0, from) + before.text.slice(from + delCount) === afterText) {
      return collapsedAt(from);
    }
  }

  // Last-resort prefix/suffix diff. Prefer selectionAnchoredCaret above for
  // repeated-character inserts; this path is for edits that cannot be explained
  // as a single splice at the pre-edit selection.
  let prefix = 0;
  const minLen = Math.min(before.text.length, afterText.length);
  while (prefix < minLen && before.text.charCodeAt(prefix) === afterText.charCodeAt(prefix)) {
    prefix += 1;
  }
  let suffix = 0;
  while (
    suffix < before.text.length - prefix
    && suffix < afterText.length - prefix
    && before.text.charCodeAt(before.text.length - 1 - suffix) === afterText.charCodeAt(afterText.length - 1 - suffix)
  ) {
    suffix += 1;
  }
  if (prefix + suffix <= Math.max(before.text.length, afterText.length)) {
    return collapsedAt(afterText.length - suffix);
  }

  return collapsedAt(fallback.end);
}

export function slashQueryAt(text: string, selection: RichComposerSelection): RichSlashQuery | null {
  if (selection.start !== selection.end) return null;
  const before = text.slice(0, selection.start);
  if (activeRefTokenRe.test(before)) return null;
  const match = /\/([A-Za-z0-9_.:-]*)$/.exec(before);
  if (!match) return null;
  const slashOffset = before.length - match[1].length - 1;
  let tokenEnd = selection.start;
  while (tokenEnd < text.length && /[A-Za-z0-9_.:-]/.test(text[tokenEnd])) tokenEnd += 1;
  return { from: slashOffset, to: tokenEnd, query: match[1].toLowerCase() };
}

let nextInvocationID = 1;

export const RichComposerInput = forwardRef<RichComposerInputHandle, {
  text: string;
  invocations: ComposerInvocation[];
  placeholder: string;
  disabled: boolean;
  style?: CSSProperties;
  onChange: (
    text: string,
    invocations: ComposerInvocation[],
    origin: RichComposerChangeOrigin,
  ) => void;
  onSelectionChange: (selection: RichComposerSelection, slashQuery: RichSlashQuery | null) => void;
  onKeyDown: (event: KeyboardEvent<HTMLDivElement>) => void;
  onContextMenu: (event: MouseEvent<HTMLDivElement>) => void;
  onPaste: (event: ClipboardEvent<HTMLDivElement>) => void;
  onCompositionStart: () => void;
  onCompositionEnd: () => void;
}>(({
  text,
  invocations,
  placeholder,
  disabled,
  style,
  onChange,
  onSelectionChange,
  onKeyDown,
  onContextMenu,
  onPaste,
  onCompositionStart,
  onCompositionEnd,
}, ref) => {
  const rootRef = useRef<HTMLDivElement>(null);
  const pendingSelectionRef = useRef<PendingSelection>(null);
  // contentEditable mutates its DOM before input fires. Keep that browser-owned
  // DOM for the matching controlled-state echo; rendering the same text again
  // would append a duplicate node because React does not own the browser's
  // mutation. External model changes bump the root key and rebuild a clean DOM.
  const domModelRef = useRef<ComposerModel | null>(null);
  const renderedModelRef = useRef<RenderedComposerModel>({ text, invocations, version: 0 });
  const incomingModel: ComposerModel = { text, invocations };
  // The rendered snapshot intentionally lags accepted browser echoes.
  const acceptedModelRef = useRef<ComposerModel>(incomingModel);
  if (sameComposerModel(incomingModel, domModelRef.current)) {
    acceptedModelRef.current = incomingModel;
  } else if (!sameComposerModel(incomingModel, acceptedModelRef.current)) {
    renderedModelRef.current = {
      text,
      invocations,
      version: renderedModelRef.current.version + 1,
    };
    acceptedModelRef.current = incomingModel;
    domModelRef.current = null;
  }
  const renderedModel = renderedModelRef.current;
  // True between compositionstart and compositionend. While an IME is
  // composing, the browser owns the DOM text node and the selection: syncing
  // the controlled model (a re-render patches the composing text node) or
  // restoring the selection (removeAllRanges/addRange) cancels or commits the
  // composition mid-word, so every model→DOM and DOM→model path below stays
  // silent until compositionend performs one authoritative resync.
  const composingRef = useRef(false);
  const lastValidSelectionRef = useRef<RichComposerSelection>({ start: 0, end: 0 });
  const beforeInputRef = useRef<EditSnapshot | null>(null);
  const compositionFinalizePendingRef = useRef(false);
  const compositionFinalizeFrameRef = useRef<number | null>(null);
  const known = useMemo(() => new Map(invocations.map((invocation) => [invocation.id, invocation])), [invocations]);
  const ordered = useMemo(() => sortComposerInvocations(invocations), [invocations]);

  const readSelection = (root: HTMLElement): RichComposerSelection => {
    const read = selectionFromDom(root, known);
    if (read.ok) {
      lastValidSelectionRef.current = read.selection;
      return read.selection;
    }
    return lastValidSelectionRef.current;
  };

  const reportSelection = () => {
    if (composingRef.current) return;
    const root = rootRef.current;
    if (!root) return;
    const selection = readSelection(root);
    const liveText = modelFromDom(root, known).text;
    // A real selection event supersedes any browser-echo caret restoration
    // that has not reached the layout effect yet.
    if (pendingSelectionRef.current) pendingSelectionRef.current = selection;
    // A keyup can arrive before React has echoed the preceding browser input
    // back through props. Read the live DOM so slash completion sees the
    // just-typed token instead of one render behind.
    onSelectionChange(selection, slashQueryAt(liveText, selection));
  };

  const replaceRange = (value: string, start: number, end: number) => {
    const next = replaceInvocationTextRange(text, invocations, start, end, value);
    const afterSelection = { start: start + value.length, end: start + value.length };
    pendingSelectionRef.current = afterSelection;
    onChange(next.text, next.invocations, {
      source: "programmatic",
      beforeSelection: { start, end },
      afterSelection,
    });
  };

  useImperativeHandle(ref, () => ({
    focus: () => rootRef.current?.focus(),
    getSelection: () => {
      const root = rootRef.current;
      if (!root) return { start: text.length, end: text.length };
      return readSelection(root);
    },
    setSelectionRange: (start, end = start, afterInvocationId) => {
      const target = { start, end, afterInvocationId };
      pendingSelectionRef.current = target;
      requestAnimationFrame(() => {
        const pending = pendingSelectionRef.current;
        if (
          !pending
          || pending.start !== target.start
          || pending.end !== target.end
          || pending.afterInvocationId !== target.afterInvocationId
        ) {
          return;
        }
        const root = rootRef.current;
        if (!root) return;
        root.focus();
        setDomSelection(root, target);
        lastValidSelectionRef.current = target;
        reportSelection();
      });
    },
    replaceRange,
    insertInvocation: (command, query) => {
      const next = replaceInvocationTextRange(text, invocations, query.from, query.to, "");
      const id = `invocation-${nextInvocationID++}`;
      const invocation: ComposerInvocation = { id, offset: query.from, command };
      const afterSelection = { start: query.from, end: query.from, afterInvocationId: id };
      pendingSelectionRef.current = afterSelection;
      onChange(next.text, sortComposerInvocations([...next.invocations, invocation]), {
        source: "programmatic",
        beforeSelection: { start: query.from, end: query.to },
        afterSelection,
      });
    },
    scrollHeight: () => rootRef.current?.scrollHeight ?? 0,
  }), [invocations, known, text]);

  useLayoutEffect(() => {
    if (composingRef.current) return;
    const pending = pendingSelectionRef.current;
    const root = rootRef.current;
    if (!pending || !root) return;
    pendingSelectionRef.current = null;
    // Only restore an explicit pending caret when this editor owns focus, or
    // when nothing else is focused. External draft replacements must not steal
    // focus from other controls.
    const active = document.activeElement;
    const ownsFocus = !active || active === document.body || root === active || root.contains(active);
    if (ownsFocus) {
      root.focus();
      setDomSelection(root, pending);
      lastValidSelectionRef.current = {
        start: pending.start,
        end: pending.end,
        afterInvocationId: pending.afterInvocationId,
      };
      reportSelection();
    } else {
      lastValidSelectionRef.current = {
        start: pending.start,
        end: pending.end,
        afterInvocationId: pending.afterInvocationId,
      };
    }
  }, [ordered, text]);

  // Selection is reported before onChange so the parent sees the fresh caret
  // when handling the model change — it matters when a change empties the
  // invocation list and the parent must hand focus/caret to the plain
  // textarea that replaces this component.
  const syncFromDom = () => {
    const root = rootRef.current;
    if (!root) return;
    const next = modelFromDom(root, known);
    const live = selectionFromDom(root, known);
    const snapshot = beforeInputRef.current;
    let selection: RichComposerSelection;

    const recoverFromSnapshot = (): RichComposerSelection | null => {
      if (!snapshot || snapshot.text === next.text) return null;
      return recoverSelectionAfterEdit(snapshot, next.text, lastValidSelectionRef.current);
    };

    // WebView2 sometimes keeps a live selection but parks it at the end after an
    // edit that started mid-text (or drops selection entirely). Prefer the
    // beforeinput / compositionstart snapshot whenever the live caret is not a
    // plausible post-edit position.
    if (live.ok) {
      selection = live.selection;
      const recovered = recoverFromSnapshot();
      if (recovered) {
        const liveCollapsed = live.selection.start === live.selection.end;
        const liveAtEnd = liveCollapsed && live.selection.start === next.text.length;
        const preCollapsed = snapshot!.selection.start === snapshot!.selection.end;
        const preAtEnd = preCollapsed && snapshot!.selection.start === snapshot!.text.length;
        const recoveredDiffers = recovered.start !== live.selection.start
          || recovered.end !== live.selection.end;
        if (liveAtEnd && !preAtEnd && recoveredDiffers) {
          selection = recovered;
          setDomSelection(root, selection);
        }
      }
      lastValidSelectionRef.current = selection;
    } else if (snapshot) {
      selection = recoverFromSnapshot() ?? {
        start: Math.min(snapshot.selection.start, next.text.length),
        end: Math.min(snapshot.selection.end, next.text.length),
        afterInvocationId: snapshot.selection.afterInvocationId,
      };
      lastValidSelectionRef.current = selection;
      // Re-apply so the visible caret matches the recovered model offset.
      setDomSelection(root, selection);
    } else {
      selection = {
        start: Math.min(lastValidSelectionRef.current.start, next.text.length),
        end: Math.min(lastValidSelectionRef.current.end, next.text.length),
        afterInvocationId: lastValidSelectionRef.current.afterInvocationId,
      };
      lastValidSelectionRef.current = selection;
    }
    beforeInputRef.current = null;
    domModelRef.current = next;
    pendingSelectionRef.current = selection;
    onSelectionChange(selection, slashQueryAt(next.text, selection));
    onChange(next.text, next.invocations, {
      source: "browser",
      inputType: snapshot?.inputType,
      beforeSelection: snapshot?.selection ?? lastValidSelectionRef.current,
      afterSelection: selection,
    });
  };

  const cancelCompositionFinalize = () => {
    compositionFinalizePendingRef.current = false;
    if (compositionFinalizeFrameRef.current !== null) {
      cancelAnimationFrame(compositionFinalizeFrameRef.current);
      compositionFinalizeFrameRef.current = null;
    }
  };

  const onInput = (event: FormEvent<HTMLDivElement>) => {
    // Chromium variants disagree about final IME event order. Some fire the
    // commit input before compositionend with isComposing=true; WebView2 may
    // fire compositionend first and expose the committed DOM in a following
    // non-composing input. In the latter case this input owns the resync and
    // cancels the deferred compositionend fallback.
    if (composingRef.current || (event.nativeEvent as InputEvent).isComposing) return;
    cancelCompositionFinalize();
    syncFromDom();
  };

  // Composition tracking uses native listeners rather than React's synthetic
  // onCompositionStart/onCompositionEnd: React's composition plugin decides at
  // module load whether CompositionEvent exists and otherwise synthesizes from
  // key events, and the IME guard must not depend on that fallback. The ref
  // indirection keeps the mount-once listeners reading the current props and
  // model instead of a stale first-render closure.
  const compositionHandlersRef = useRef({ start: () => {}, end: () => {} });
  compositionHandlersRef.current = {
    start: () => {
      cancelCompositionFinalize();
      // Freeze the pre-composition model and caret. Intermediate beforeinput
      // events are ignored while composing (provisional DOM text would poison
      // the snapshot), so compositionend blackout recovery depends on this.
      const root = rootRef.current;
      if (root) {
        const live = selectionFromDom(root, known);
        const selection = live.ok ? live.selection : lastValidSelectionRef.current;
        if (live.ok) lastValidSelectionRef.current = live.selection;
        const model = modelFromDom(root, known);
        beforeInputRef.current = {
          text: model.text,
          selection,
          inputType: "insertCompositionText",
          data: null,
        };
      }
      composingRef.current = true;
      onCompositionStart();
    },
    end: () => {
      composingRef.current = false;
      onCompositionEnd();
      const root = rootRef.current;
      const snapshot = beforeInputRef.current;
      // Most browsers expose the committed DOM by compositionend. Preserve the
      // synchronous path in that case so submit/read-after-composition observes
      // the final text immediately. Windows WebView2 can instead dispatch
      // compositionend while the DOM still matches the pre-composition snapshot;
      // only that blackout needs to wait for the following non-composing input
      // (or the next-frame fallback).
      if (!root || !snapshot || modelFromDom(root, known).text !== snapshot.text) {
        cancelCompositionFinalize();
        syncFromDom();
        return;
      }
      cancelCompositionFinalize();
      compositionFinalizePendingRef.current = true;
      compositionFinalizeFrameRef.current = requestAnimationFrame(() => {
        compositionFinalizeFrameRef.current = null;
        if (!compositionFinalizePendingRef.current) return;
        compositionFinalizePendingRef.current = false;
        syncFromDom();
      });
    },
  };
  useLayoutEffect(() => () => {
    cancelCompositionFinalize();
  }, []);
  useLayoutEffect(() => {
    const root = rootRef.current;
    if (!root) return;
    const start = () => compositionHandlersRef.current.start();
    const end = () => compositionHandlersRef.current.end();
    const onBeforeInput = (event: Event) => {
      // Keep the compositionstart baseline intact. Provisional composition DOM
      // must not replace the snapshot used when compositionend has no selection.
      if (composingRef.current) return;
      const inputEvent = event as InputEvent;
      const live = selectionFromDom(root, known);
      const selection = live.ok ? live.selection : lastValidSelectionRef.current;
      if (live.ok) lastValidSelectionRef.current = live.selection;
      const model = modelFromDom(root, known);
      beforeInputRef.current = {
        text: model.text,
        selection,
        inputType: inputEvent.inputType || "",
        data: inputEvent.data ?? null,
      };
    };
    root.addEventListener("compositionstart", start);
    root.addEventListener("compositionend", end);
    root.addEventListener("beforeinput", onBeforeInput);
    return () => {
      root.removeEventListener("compositionstart", start);
      root.removeEventListener("compositionend", end);
      root.removeEventListener("beforeinput", onBeforeInput);
    };
  }, [known, renderedModel.version]);

  const handleKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key === "Backspace" && !event.nativeEvent.isComposing) {
      const root = rootRef.current;
      if (root) {
        const selection = readSelection(root);
        if (selection.start === selection.end && selection.afterInvocationId) {
          const target = known.get(selection.afterInvocationId);
          if (target && target.offset === selection.start) {
            event.preventDefault();
            const next = invocations.filter((invocation) => invocation.id !== target.id);
            const afterSelection = { start: selection.start, end: selection.start };
            pendingSelectionRef.current = afterSelection;
            onSelectionChange(afterSelection, null);
            onChange(text, next, {
              source: "programmatic",
              beforeSelection: selection,
              afterSelection,
            });
            return;
          }
        }
      }
    }
    onKeyDown(event);
  };

  const renderedOrdered = sortComposerInvocations(renderedModel.invocations);
  const children: React.ReactNode[] = [];
  let cursor = 0;
  renderedOrdered.forEach((item) => {
    const offset = Math.max(cursor, Math.min(renderedModel.text.length, item.offset));
    if (offset > cursor) children.push(renderedModel.text.slice(cursor, offset));
    const invocation = invocationDisplayForCommand(item.command);
    children.push(
      <span
        key={item.id}
        className="composer-invocation-token"
        contentEditable={false}
        data-invocation-id={item.id}
      >
        <InvocationBadge
          invocation={invocation}
          kind={invocation.kind}
          description={item.command.description}
          onRemove={() => {
            const current = known.get(item.id);
            const currentOffset = current?.offset ?? offset;
            const afterSelection = { start: currentOffset, end: currentOffset };
            pendingSelectionRef.current = afterSelection;
            onSelectionChange(afterSelection, null);
            onChange(text, invocations.filter((candidate) => candidate.id !== item.id), {
              source: "programmatic",
              beforeSelection: lastValidSelectionRef.current,
              afterSelection,
            });
          }}
          variant="composer"
        />
      </span>,
    );
    // WebKit needs a hit-testable editable position after a non-editable token.
    // The sentinel is stripped from the composer value by modelFromDom.
    children.push(
      <span
        key={`${item.id}-caret`}
        className="composer-invocation-caret-anchor"
        data-composer-caret-anchor="true"
        aria-hidden="true"
      >
        {CARET_SENTINEL}
      </span>,
    );
    cursor = offset;
  });
  if (cursor < renderedModel.text.length) children.push(renderedModel.text.slice(cursor));

  return (
    <div
      key={renderedModel.version}
      id="composer-input"
      ref={rootRef}
      className="composer__rich-input"
      contentEditable={!disabled} spellCheck={false} autoCorrect="off" autoCapitalize="off"
      suppressContentEditableWarning
      role="textbox"
      aria-multiline="true"
      aria-label={placeholder}
      data-placeholder={placeholder}
      data-empty={text === "" && invocations.length === 0 ? "true" : undefined}
      style={style}
      onInput={onInput}
      onKeyDown={handleKeyDown}
      onKeyUp={reportSelection}
      onClick={reportSelection}
      onFocus={reportSelection}
      onContextMenu={onContextMenu}
      onPaste={onPaste}
    >
      {children}
    </div>
  );
});

RichComposerInput.displayName = "RichComposerInput";
