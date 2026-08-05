const MESSAGE_SELECTION_SELECTOR = ".msg__body, .reasoning__body";

export interface MessageSelectionCopyState {
  text: string;
  isCollapsed: boolean;
  targetIsEditable: boolean;
  intersectsMessage: boolean;
  canWriteClipboard: boolean;
}

export function messageSelectionCopyText(state: MessageSelectionCopyState): string | null {
  if (state.isCollapsed) return null;
  if (state.targetIsEditable) return null;
  if (!state.intersectsMessage) return null;
  if (!state.canWriteClipboard) return null;
  if (state.text.trim() === "") return null;
  return state.text;
}

// Gates the transcript right-click menu the same way the copy interceptor
// gates ⌘C — a non-collapsed, non-editable selection touching a message or
// reasoning body — plus one menu-only rule: the right-click must land inside
// a message body that the selection itself touches. A selection outlives
// clicks elsewhere: surfaces like the project tree and tab bar own their
// right-click menus, and offering Copy on message B while message A holds
// the selection would copy text from somewhere other than the click. A
// selection spanning several messages accepts a right-click on any of them.
// Returns the text the menu would copy, or null when the menu should not
// open. canWriteClipboard is true because the menu writes through
// writeClipboardText rather than a ClipboardEvent's clipboardData.
export function messageSelectionContextText(doc: Document, target: EventTarget | null): string | null {
  const selection = doc.getSelection();
  if (!targetWithinSelectedMessage(target, selection)) return null;
  return messageSelectionCopyText({
    text: restoreLatexInSelection(selection),
    isCollapsed: selection == null || selection.isCollapsed,
    targetIsEditable: isEditableTarget(target),
    intersectsMessage: selectionIntersectsMessage(selection, doc),
    canWriteClipboard: true,
  });
}

function targetWithinSelectedMessage(target: EventTarget | null, selection: Selection | null): boolean {
  const el = elementFromEventTarget(target);
  const message = el?.closest(MESSAGE_SELECTION_SELECTOR);
  if (!message || !selection || selection.rangeCount === 0) return false;
  for (let i = 0; i < selection.rangeCount; i += 1) {
    if (rangeIntersectsNode(selection.getRangeAt(i), message)) return true;
  }
  return false;
}

// Range.intersectsNode with a boundary-point fallback for DOM implementations
// that lack it (jsdom). Ranges intersect a node when the range starts before
// the node's end and ends after the node's start.
function rangeIntersectsNode(range: Range, node: Node): boolean {
  try {
    if (typeof range.intersectsNode === "function") return range.intersectsNode(node);
    const doc = node.ownerDocument;
    if (!doc) return false;
    const nodeRange = doc.createRange();
    nodeRange.selectNodeContents(node);
    return range.compareBoundaryPoints(nodeRange.END_TO_START, nodeRange) < 0
      && range.compareBoundaryPoints(nodeRange.START_TO_END, nodeRange) > 0;
  } catch {
    return false;
  }
}

export function installMessageSelectionCopy(doc: Document = document): () => void {
  const onCopy = (event: ClipboardEvent) => {
    const selection = doc.getSelection();
    const text = messageSelectionCopyText({
      text: restoreLatexInSelection(selection),
      isCollapsed: selection == null || selection.isCollapsed,
      targetIsEditable: isEditableTarget(event.target),
      intersectsMessage: selectionIntersectsMessage(selection, doc),
      canWriteClipboard: event.clipboardData != null,
    });
    if (text == null || event.clipboardData == null) return;

    event.clipboardData.setData("text/plain", text);
    event.preventDefault();
  };

  doc.addEventListener("copy", onCopy);
  return () => doc.removeEventListener("copy", onCopy);
}

function restoreLatexInSelection(selection: Selection | null): string {
  if (!selection || selection.isCollapsed || selection.rangeCount === 0) return "";
  const range = selection.getRangeAt(0);
  const baseText = selection.toString();
  if (baseText.trim() === "") return "";

  const formulas = selectedKatexRoots(range);
  if (formulas.length === 0) return baseText;

  const doc = range.commonAncestorContainer.ownerDocument;
  if (!doc) return baseText;

  let cursorNode = range.startContainer;
  let cursorOffset = range.startOffset;
  let text = "";

  for (const formula of formulas) {
    const source = katexSource(formula);
    if (source == null) continue;

    const formulaRange = doc.createRange();
    formulaRange.selectNode(formula);
    if (comparePoints(
      doc,
      formulaRange.endContainer,
      formulaRange.endOffset,
      range.startContainer,
      range.startOffset,
    ) <= 0) continue;
    if (comparePoints(
      doc,
      formulaRange.startContainer,
      formulaRange.startOffset,
      range.endContainer,
      range.endOffset,
    ) >= 0) break;

    if (comparePoints(
      doc,
      cursorNode,
      cursorOffset,
      formulaRange.startContainer,
      formulaRange.startOffset,
    ) < 0) {
      const before = doc.createRange();
      before.setStart(cursorNode, cursorOffset);
      before.setEnd(formulaRange.startContainer, formulaRange.startOffset);
      text += before.toString();
    }

    text += source;
    if (comparePoints(
      doc,
      formulaRange.endContainer,
      formulaRange.endOffset,
      range.endContainer,
      range.endOffset,
    ) >= 0) {
      return text;
    }
    cursorNode = formulaRange.endContainer;
    cursorOffset = formulaRange.endOffset;
  }

  if (comparePoints(
    doc,
    cursorNode,
    cursorOffset,
    range.endContainer,
    range.endOffset,
  ) < 0) {
    const after = doc.createRange();
    after.setStart(cursorNode, cursorOffset);
    after.setEnd(range.endContainer, range.endOffset);
    text += after.toString();
  }
  return text || baseText;
}

function selectedKatexRoots(range: Range): HTMLElement[] {
  const common = elementFromNode(range.commonAncestorContainer);
  if (!common) return [];

  const roots = new Set<HTMLElement>();
  const ancestor = katexRoot(common);
  if (ancestor && rangeIntersectsNode(range, ancestor)) roots.add(ancestor);

  const candidates = common.matches(".katex, .katex-display")
    ? [common, ...Array.from(common.querySelectorAll(".katex, .katex-display"))]
    : Array.from(common.querySelectorAll(".katex, .katex-display"));
  for (const candidate of candidates) {
    const root = katexRoot(candidate);
    if (root && rangeIntersectsNode(range, root)) roots.add(root);
  }

  return Array.from(roots).sort((a, b) => {
    if (a === b) return 0;
    const position = a.compareDocumentPosition(b);
    return position & Node.DOCUMENT_POSITION_FOLLOWING ? -1 : 1;
  });
}

function katexRoot(node: Element): HTMLElement | null {
  const display = node.closest<HTMLElement>(".katex-display");
  if (display) return display;
  return node.closest<HTMLElement>(".katex");
}

function katexSource(root: HTMLElement): string | null {
  const annotation = root.querySelector<HTMLElement>(
    'annotation[encoding="application/x-tex"]',
  );
  const tex = annotation?.textContent ?? root.getAttribute("data-latex-source");
  if (!tex) return null;
  return root.classList.contains("katex-display")
    ? `$$\n${tex}\n$$`
    : `$${tex}$`;
}

function comparePoints(
  doc: Document,
  leftNode: Node,
  leftOffset: number,
  rightNode: Node,
  rightOffset: number,
): number {
  const left = doc.createRange();
  left.setStart(leftNode, leftOffset);
  left.collapse(true);
  const right = doc.createRange();
  right.setStart(rightNode, rightOffset);
  right.collapse(true);
  return left.compareBoundaryPoints(0, right);
}

function selectionIntersectsMessage(selection: Selection | null, root: ParentNode): boolean {
  if (selection == null || selection.rangeCount === 0 || selection.isCollapsed) return false;
  for (let i = 0; i < selection.rangeCount; i += 1) {
    if (rangeIntersectsMessage(selection.getRangeAt(i), root)) return true;
  }
  return false;
}

function rangeIntersectsMessage(range: Range, root: ParentNode): boolean {
  const common = elementFromNode(range.commonAncestorContainer);
  const directMessage = common?.closest(MESSAGE_SELECTION_SELECTOR);
  if (directMessage) return true;

  const scope = common ?? root;
  const candidates = scope instanceof Element && scope.matches(MESSAGE_SELECTION_SELECTOR)
    ? [scope, ...Array.from(scope.querySelectorAll(MESSAGE_SELECTION_SELECTOR))]
    : Array.from(scope.querySelectorAll(MESSAGE_SELECTION_SELECTOR));

  return candidates.some((node) => rangeIntersectsNode(range, node));
}

function isEditableTarget(target: EventTarget | null): boolean {
  const el = elementFromEventTarget(target);
  if (!el) return false;
  if (el.closest("input, textarea, select")) return true;
  for (let node: Element | null = el; node; node = node.parentElement) {
    if (node instanceof HTMLElement && node.isContentEditable) return true;
  }
  return false;
}

function elementFromEventTarget(target: EventTarget | null): Element | null {
  return target instanceof Element ? target : null;
}

function elementFromNode(node: Node | null): Element | null {
  if (!node) return null;
  return node.nodeType === Node.ELEMENT_NODE ? node as Element : node.parentElement;
}
