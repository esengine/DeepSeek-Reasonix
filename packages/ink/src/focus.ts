import type { DOMElement } from './dom.js';
import { FocusEvent } from './events/focus-event.js';

/** Upper bound on the size of the focus restoration stack. */
const MAX_FOCUS_STACK = 32;

/** DOM-like focus manager. */
export class FocusManager {
  activeElement: DOMElement | null = null;

  private readonly dispatch: (target: DOMElement, event: FocusEvent) => boolean;
  private enabled = true;
  private focusStack: DOMElement[] = [];

  constructor(dispatch: (target: DOMElement, event: FocusEvent) => boolean) {
    this.dispatch = dispatch;
  }

  focus(node: DOMElement): void {
    if (node === this.activeElement) return;
    if (!this.enabled) return;

    const previous = this.activeElement;
    if (previous) {
      // Deduplicate before pushing: tab-cycling between a small set of
      // nodes would otherwise pin the stack at MAX_FOCUS_STACK forever
      // and silently drop earlier entries from real navigation history.
      const existing = this.focusStack.indexOf(previous);
      if (existing !== -1) this.focusStack.splice(existing, 1);
      this.focusStack.push(previous);
      if (this.focusStack.length > MAX_FOCUS_STACK) this.focusStack.shift();
      this.dispatch(previous, new FocusEvent('blur', node));
    }

    this.activeElement = node;
    this.dispatch(node, new FocusEvent('focus', previous));
  }

  blur(): void {
    if (!this.activeElement) return;

    const previous = this.activeElement;
    this.activeElement = null;
    this.dispatch(previous, new FocusEvent('blur', null));
  }

  /** Called from the reconciler when a node leaves the tree. */
  handleNodeRemoved(node: DOMElement, root: DOMElement): void {
    this.focusStack = this.focusStack.filter(
      (entry) => entry !== node && isInTree(entry, root),
    );

    if (!this.activeElement) return;
    if (this.activeElement !== node && isInTree(this.activeElement, root)) {
      return;
    }

    const removed = this.activeElement;
    this.activeElement = null;
    this.dispatch(removed, new FocusEvent('blur', null));

    while (this.focusStack.length > 0) {
      const candidate = this.focusStack.pop()!;
      if (isInTree(candidate, root)) {
        this.activeElement = candidate;
        this.dispatch(candidate, new FocusEvent('focus', removed));
        return;
      }
    }
  }

  handleAutoFocus(node: DOMElement): void {
    this.focus(node);
  }

  /** Click-to-focus. */
  handleClickFocus(node: DOMElement): void {
    const tabIndex = node.attributes['tabIndex'];
    if (typeof tabIndex !== 'number') return;
    this.focus(node);
  }

  enable(): void {
    this.enabled = true;
  }

  disable(): void {
    this.enabled = false;
  }

  focusNext(root: DOMElement): void {
    this.moveFocus(1, root);
  }

  focusPrevious(root: DOMElement): void {
    this.moveFocus(-1, root);
  }

  private moveFocus(direction: 1 | -1, root: DOMElement): void {
    if (!this.enabled) return;

    const tabbable = collectTabbable(root);
    if (tabbable.length === 0) return;

    const currentIndex = this.activeElement ? tabbable.indexOf(this.activeElement) : -1;

    const nextIndex =
      currentIndex === -1
        ? direction === 1
          ? 0
          : tabbable.length - 1
        : (currentIndex + direction + tabbable.length) % tabbable.length;

    const next = tabbable[nextIndex];
    if (next) this.focus(next);
  }
}

function collectTabbable(root: DOMElement): DOMElement[] {
  const collected: DOMElement[] = [];
  walkTree(root, collected);
  return collected;
}

function walkTree(node: DOMElement, collected: DOMElement[]): void {
  const tabIndex = node.attributes['tabIndex'];
  if (typeof tabIndex === 'number' && tabIndex >= 0) {
    collected.push(node);
  }

  for (const child of node.childNodes) {
    if (child.nodeName !== '#text') {
      walkTree(child, collected);
    }
  }
}

function isInTree(node: DOMElement, root: DOMElement): boolean {
  let current: DOMElement | undefined = node;
  while (current) {
    if (current === root) return true;
    current = current.parentNode;
  }
  return false;
}

/** Walk up to the root DOMElement and return it. */
export function getRootNode(node: DOMElement): DOMElement {
  let current: DOMElement | undefined = node;
  while (current) {
    if (current.focusManager) return current;
    current = current.parentNode;
  }
  throw new Error('Node is not attached to a tree that owns a FocusManager');
}

export function getFocusManager(node: DOMElement): FocusManager {
  return getRootNode(node).focusManager!;
}
