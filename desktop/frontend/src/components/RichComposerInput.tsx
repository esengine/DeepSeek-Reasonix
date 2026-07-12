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
} from "react";
import {
  invocationDisplayForCommand,
  replaceInvocationTextRange,
  sortComposerInvocations,
  type ComposerInvocation,
} from "../lib/invocationDisplay";
import type { CommandInfo } from "../lib/types";
import { InvocationBadge } from "./InvocationBadge";

export type RichComposerSelection = {
  start: number;
  end: number;
  afterInvocationId?: string;
};

export type RichSlashQuery = {
  from: number;
  to: number;
  query: string;
};

export type RichComposerInputHandle = {
  focus: () => void;
  getSelection: () => RichComposerSelection;
  setSelectionRange: (start: number, end?: number) => void;
  replaceRange: (value: string, start: number, end: number) => void;
  insertInvocation: (command: CommandInfo, query: RichSlashQuery) => void;
  scrollHeight: () => number;
};

type PendingSelection = RichComposerSelection | null;

const CARET_SENTINEL = "\u00A0";

function modelFromDom(root: HTMLElement, known: Map<string, ComposerInvocation>) {
  let text = "";
  const invocations: ComposerInvocation[] = [];
  const visit = (node: Node) => {
    if (node instanceof HTMLElement) {
      const id = node.dataset.invocationId;
      if (id) {
        const invocation = known.get(id);
        if (invocation) invocations.push({ ...invocation, offset: text.length });
        return;
      }
      if (node.dataset.composerCaretAnchor) {
        text += (node.textContent ?? "").replace(CARET_SENTINEL, "");
        return;
      }
      if (node.tagName === "BR") {
        text += "\n";
        return;
      }
    }
    if (node.nodeType === Node.TEXT_NODE) {
      text += node.textContent ?? "";
      return;
    }
    node.childNodes.forEach(visit);
  };
  root.childNodes.forEach(visit);
  return {
    text: text.replace(/\u00a0/g, " "),
    invocations: sortComposerInvocations(invocations),
  };
}

function selectionFromDom(root: HTMLElement, known: Map<string, ComposerInvocation>): RichComposerSelection {
  const selection = document.getSelection();
  if (!selection || selection.rangeCount === 0 || !root.contains(selection.anchorNode) || !root.contains(selection.focusNode)) {
    const model = modelFromDom(root, known);
    return { start: model.text.length, end: model.text.length };
  }
  const point = (node: Node, offset: number) => {
    const range = document.createRange();
    range.setStart(root, 0);
    range.setEnd(node, offset);
    const fragment = range.cloneContents();
    const shell = document.createElement("div");
    shell.appendChild(fragment);
    const model = modelFromDom(shell, known);
    const lastInvocation = model.invocations[model.invocations.length - 1];
    return {
      offset: model.text.length,
      afterInvocationId: lastInvocation?.offset === model.text.length ? lastInvocation.id : undefined,
    };
  };
  const anchor = point(selection.anchorNode as Node, selection.anchorOffset);
  const focus = point(selection.focusNode as Node, selection.focusOffset);
  return {
    start: Math.min(anchor.offset, focus.offset),
    end: Math.max(anchor.offset, focus.offset),
    afterInvocationId: selection.isCollapsed ? focus.afterInvocationId : undefined,
  };
}

function setDomSelection(root: HTMLElement, target: RichComposerSelection) {
  const selection = document.getSelection();
  if (!selection) return;
  const range = document.createRange();
  if (target.afterInvocationId) {
    const token = Array.from(root.querySelectorAll<HTMLElement>("[data-invocation-id]"))
      .find((candidate) => candidate.dataset.invocationId === target.afterInvocationId);
    if (token?.parentNode) {
      const index = Array.prototype.indexOf.call(token.parentNode.childNodes, token);
      range.setStart(token.parentNode, index + 1);
      range.collapse(true);
      selection.removeAllRanges();
      selection.addRange(range);
      return;
    }
  }

  let remaining = target.end;
  let found: { node: Node; offset: number } | null = null;
  const visit = (node: Node) => {
    if (found) return;
    if (node instanceof HTMLElement && node.dataset.invocationId) return;
    if (node instanceof HTMLElement && node.dataset.composerCaretAnchor) return;
    if (node.nodeType === Node.TEXT_NODE) {
      const length = node.textContent?.length ?? 0;
      if (remaining <= length) found = { node, offset: remaining };
      else remaining -= length;
      return;
    }
    for (const child of Array.from(node.childNodes)) visit(child);
  };
  visit(root);
  const point = found ?? { node: root, offset: root.childNodes.length };
  range.setStart(point.node, point.offset);
  range.collapse(true);
  selection.removeAllRanges();
  selection.addRange(range);
}

function slashQueryAt(text: string, selection: RichComposerSelection): RichSlashQuery | null {
  if (selection.start !== selection.end) return null;
  const before = text.slice(0, selection.start);
  const match = /(?:^|\s)\/([A-Za-z0-9_.:-]*)$/.exec(before);
  if (!match) return null;
  const slashOffset = before.length - match[1].length - 1;
  return { from: slashOffset, to: selection.start, query: match[1].toLowerCase() };
}

let nextInvocationID = 1;

export const RichComposerInput = forwardRef<RichComposerInputHandle, {
  text: string;
  invocations: ComposerInvocation[];
  placeholder: string;
  disabled: boolean;
  style?: CSSProperties;
  onChange: (text: string, invocations: ComposerInvocation[]) => void;
  onSelectionChange: (selection: RichComposerSelection, slashQuery: RichSlashQuery | null) => void;
  onKeyDown: (event: KeyboardEvent<HTMLDivElement>) => void;
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
  onPaste,
  onCompositionStart,
  onCompositionEnd,
}, ref) => {
  const rootRef = useRef<HTMLDivElement>(null);
  const pendingSelectionRef = useRef<PendingSelection>(null);
  // True between compositionstart and compositionend. While an IME is
  // composing, the browser owns the DOM text node and the selection: syncing
  // the controlled model (a re-render patches the composing text node) or
  // restoring the selection (removeAllRanges/addRange) cancels or commits the
  // composition mid-word, so every model→DOM and DOM→model path below stays
  // silent until compositionend performs one authoritative resync.
  const composingRef = useRef(false);
  const known = useMemo(() => new Map(invocations.map((invocation) => [invocation.id, invocation])), [invocations]);
  const ordered = useMemo(() => sortComposerInvocations(invocations), [invocations]);

  const reportSelection = () => {
    if (composingRef.current) return;
    const root = rootRef.current;
    if (!root) return;
    const selection = selectionFromDom(root, known);
    onSelectionChange(selection, slashQueryAt(text, selection));
  };

  const replaceRange = (value: string, start: number, end: number) => {
    const next = replaceInvocationTextRange(text, invocations, start, end, value);
    pendingSelectionRef.current = { start: start + value.length, end: start + value.length };
    onChange(next.text, next.invocations);
  };

  useImperativeHandle(ref, () => ({
    focus: () => rootRef.current?.focus(),
    getSelection: () => rootRef.current ? selectionFromDom(rootRef.current, known) : { start: text.length, end: text.length },
    setSelectionRange: (start, end = start) => {
      pendingSelectionRef.current = { start, end };
      requestAnimationFrame(() => {
        const root = rootRef.current;
        if (!root) return;
        root.focus();
        setDomSelection(root, { start, end });
        reportSelection();
      });
    },
    replaceRange,
    insertInvocation: (command, query) => {
      const next = replaceInvocationTextRange(text, invocations, query.from, query.to, "");
      const id = `invocation-${nextInvocationID++}`;
      const invocation: ComposerInvocation = { id, offset: query.from, command };
      pendingSelectionRef.current = { start: query.from, end: query.from, afterInvocationId: id };
      onChange(next.text, sortComposerInvocations([...next.invocations, invocation]));
    },
    scrollHeight: () => rootRef.current?.scrollHeight ?? 0,
  }), [invocations, known, text]);

  useLayoutEffect(() => {
    if (composingRef.current) return;
    const pending = pendingSelectionRef.current;
    const root = rootRef.current;
    if (!pending || !root) return;
    pendingSelectionRef.current = null;
    root.focus();
    setDomSelection(root, pending);
    reportSelection();
  }, [ordered, text]);

  // Selection is reported before onChange so the parent sees the fresh caret
  // when handling the model change — it matters when a change empties the
  // invocation list and the parent must hand focus/caret to the plain
  // textarea that replaces this component.
  const syncFromDom = () => {
    const root = rootRef.current;
    if (!root) return;
    const selection = selectionFromDom(root, known);
    const next = modelFromDom(root, known);
    pendingSelectionRef.current = selection;
    onSelectionChange(selection, slashQueryAt(next.text, selection));
    onChange(next.text, next.invocations);
  };

  const onInput = (event: FormEvent<HTMLDivElement>) => {
    // Chrome fires the commit's final input event before compositionend with
    // isComposing still true; the compositionend handler owns that resync.
    if (composingRef.current || (event.nativeEvent as InputEvent).isComposing) return;
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
      composingRef.current = true;
      onCompositionStart();
    },
    end: () => {
      composingRef.current = false;
      onCompositionEnd();
      syncFromDom();
    },
  };
  useLayoutEffect(() => {
    const root = rootRef.current;
    if (!root) return;
    const start = () => compositionHandlersRef.current.start();
    const end = () => compositionHandlersRef.current.end();
    root.addEventListener("compositionstart", start);
    root.addEventListener("compositionend", end);
    return () => {
      root.removeEventListener("compositionstart", start);
      root.removeEventListener("compositionend", end);
    };
  }, []);

  const handleKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key === "Backspace" && !event.nativeEvent.isComposing) {
      const root = rootRef.current;
      if (root) {
        const selection = selectionFromDom(root, known);
        if (selection.start === selection.end && selection.afterInvocationId) {
          const target = known.get(selection.afterInvocationId);
          if (target && target.offset === selection.start) {
            event.preventDefault();
            const next = invocations.filter((invocation) => invocation.id !== target.id);
            pendingSelectionRef.current = { start: selection.start, end: selection.start };
            onSelectionChange({ start: selection.start, end: selection.start }, null);
            onChange(text, next);
            return;
          }
        }
      }
    }
    onKeyDown(event);
  };

  const children: React.ReactNode[] = [];
  let cursor = 0;
  ordered.forEach((item) => {
    const offset = Math.max(cursor, Math.min(text.length, item.offset));
    if (offset > cursor) children.push(text.slice(cursor, offset));
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
            pendingSelectionRef.current = { start: offset, end: offset };
            onSelectionChange({ start: offset, end: offset }, null);
            onChange(text, invocations.filter((candidate) => candidate.id !== item.id));
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
  if (cursor < text.length) children.push(text.slice(cursor));

  return (
    <div
      id="composer-input"
      ref={rootRef}
      className="composer__rich-input"
      contentEditable={!disabled}
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
      onPaste={onPaste}
    >
      {children}
    </div>
  );
});

RichComposerInput.displayName = "RichComposerInput";
