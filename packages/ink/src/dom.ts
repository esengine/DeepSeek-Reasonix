import type { FocusManager } from './focus.js';
import { createLayoutNode } from './layout/engine.js';
import type { LayoutNode } from './layout/node.js';
import { LayoutDisplay, LayoutMeasureMode } from './layout/node.js';
import measureText from './measure-text.js';
import { addPendingClear, nodeCache } from './node-cache.js';
import squashTextNodes from './squash-text-nodes.js';
import type { Styles, TextStyles } from './styles.js';
import { expandTabs } from './tabstops.js';
import wrapText from './wrap-text.js';

type InkNode = {
  parentNode: DOMElement | undefined;
  yogaNode?: LayoutNode;
  style: Styles;
};

export type TextName = '#text';
export type ElementNames =
  | 'ink-root'
  | 'ink-box'
  | 'ink-text'
  | 'ink-virtual-text'
  | 'ink-link'
  | 'ink-progress'
  | 'ink-raw-ansi';

export type NodeNames = ElementNames | TextName;

// eslint-disable-next-line @typescript-eslint/naming-convention
export type DOMElement = {
  nodeName: ElementNames;
  attributes: Record<string, DOMNodeAttribute>;
  childNodes: DOMNode[];
  textStyles?: TextStyles;

  // -- Renderer wiring (set by the reconciler / layout pass) ---------------
  onComputeLayout?: () => void;
  onRender?: () => void;
  onImmediateRender?: () => void;
  // Guard against React 19's strict-mode effect double-invoke firing an
  // empty render before the real content commits.
  hasRenderedContent?: boolean;

  /** True when the subtree under this node needs to be re-painted. */
  dirty: boolean;
  isHidden?: boolean;
  /** Event handlers held off to one side rather than in `attributes`. */
  _eventHandlers?: Record<string, unknown>;

  // -- ScrollBox state -----------------------------------------------------
  /** Vertical scroll offset in rows. */
  scrollTop?: number;
  pendingScrollDelta?: number;
  /** Clamp bounds written by useVirtualScroll. */
  scrollClampMin?: number;
  scrollClampMax?: number;
  scrollHeight?: number;
  scrollViewportHeight?: number;
  scrollViewportTop?: number;
  stickyScroll?: boolean;
  /** One-shot scroll target set by ScrollBox.scrollToElement. */
  scrollAnchor?: { el: DOMElement; offset: number };
  /** Only set on ink-root. */
  focusManager?: FocusManager;
  /** Only set on ink-root. */
  absoluteNodeRemoved?: boolean;
  /** React component stack captured at createInstance time, e.g. */
  debugOwnerChain?: string[];
} & InkNode;

export type TextNode = {
  nodeName: TextName;
  nodeValue: string;
} & InkNode;

// eslint-disable-next-line @typescript-eslint/naming-convention
export type DOMNode<T = { nodeName: NodeNames }> = T extends {
  nodeName: infer U;
}
  ? U extends '#text'
    ? TextNode
    : DOMElement
  : never;

// eslint-disable-next-line @typescript-eslint/naming-convention
export type DOMNodeAttribute = boolean | string | number;

export const createNode = (nodeName: ElementNames): DOMElement => {
  // ink-virtual-text, ink-link, and ink-progress live inside a text node
  // for measurement purposes — they contribute characters, not yoga boxes.
  const needsYogaNode =
    nodeName !== 'ink-virtual-text' &&
    nodeName !== 'ink-link' &&
    nodeName !== 'ink-progress';
  const node: DOMElement = {
    nodeName,
    style: {},
    attributes: {},
    childNodes: [],
    parentNode: undefined,
    yogaNode: needsYogaNode ? createLayoutNode() : undefined,
    dirty: false,
  };

  if (nodeName === 'ink-text') {
    node.yogaNode?.setMeasureFunc(measureTextNode.bind(null, node));
  } else if (nodeName === 'ink-raw-ansi') {
    node.yogaNode?.setMeasureFunc(measureRawAnsiNode.bind(null, node));
  }

  return node;
};

export const appendChildNode = (
  node: DOMElement,
  childNode: DOMElement,
): void => {
  if (childNode.parentNode) {
    removeChildNode(childNode.parentNode, childNode);
  }

  childNode.parentNode = node;
  node.childNodes.push(childNode);

  if (childNode.yogaNode) {
    node.yogaNode?.insertChild(
      childNode.yogaNode,
      node.yogaNode.getChildCount(),
    );
  }

  markDirty(node);
};

export const insertBeforeNode = (
  node: DOMElement,
  newChildNode: DOMNode,
  beforeChildNode: DOMNode,
): void => {
  if (newChildNode.parentNode) {
    removeChildNode(newChildNode.parentNode, newChildNode);
  }

  newChildNode.parentNode = node;

  const index = node.childNodes.indexOf(beforeChildNode);

  if (index >= 0) {
    // Yoga indices and DOM indices drift apart because some node kinds
    // (ink-progress, ink-link, ink-virtual-text) don't have yogaNodes —
    // they participate in text layout, not box layout. Recompute the
    // yoga insertion index by counting yoga-bearing siblings up to the
    // DOM index BEFORE splicing into childNodes.
    let yogaIndex = 0;
    if (newChildNode.yogaNode && node.yogaNode) {
      for (let i = 0; i < index; i++) {
        if (node.childNodes[i]?.yogaNode) {
          yogaIndex++;
        }
      }
    }

    node.childNodes.splice(index, 0, newChildNode);

    if (newChildNode.yogaNode && node.yogaNode) {
      node.yogaNode.insertChild(newChildNode.yogaNode, yogaIndex);
    }

    markDirty(node);
    return;
  }

  node.childNodes.push(newChildNode);

  if (newChildNode.yogaNode) {
    node.yogaNode?.insertChild(
      newChildNode.yogaNode,
      node.yogaNode.getChildCount(),
    );
  }

  markDirty(node);
};

export const removeChildNode = (
  node: DOMElement,
  removeNode: DOMNode,
): void => {
  if (removeNode.yogaNode) {
    removeNode.parentNode?.yogaNode?.removeChild(removeNode.yogaNode);
  }

  // Walk the removed subtree once and stash its cached rects so the
  // renderer can erase them on the next frame.
  collectRemovedRects(node, removeNode);

  removeNode.parentNode = undefined;

  const index = node.childNodes.indexOf(removeNode);
  if (index >= 0) {
    node.childNodes.splice(index, 1);
  }

  markDirty(node);
};

function collectRemovedRects(
  parent: DOMElement,
  removed: DOMNode,
  underAbsolute = false,
): void {
  if (removed.nodeName === '#text') return;
  const elem = removed as DOMElement;
  // Track whether anything in the removed chain was absolutely positioned.
  // Absolute nodes can paint over non-siblings (e.g. an overlay covering
  // a ScrollBox earlier in tree order), so their disappearance forces a
  // global blit disable on the next frame. Normal-flow removals only
  // affect direct siblings, which `hasRemovedChild` already handles.
  const isAbsolute = underAbsolute || elem.style.position === 'absolute';
  const cached = nodeCache.get(elem);
  if (cached) {
    addPendingClear(parent, cached, isAbsolute);
    nodeCache.delete(elem);
  }
  for (const child of elem.childNodes) {
    collectRemovedRects(parent, child, isAbsolute);
  }
}

export const setAttribute = (
  node: DOMElement,
  key: string,
  value: DOMNodeAttribute,
): void => {
  // React funnels `children` through appendChild/removeChild rather than
  // attributes. The children reference changes on every render even when
  // the children themselves don't, so treating it as an attribute would
  // mark the subtree dirty unconditionally.
  if (key === 'children') {
    return;
  }
  if (node.attributes[key] === value) {
    return;
  }
  node.attributes[key] = value;
  markDirty(node);
};

export const setStyle = (node: DOMNode, style: Styles): void => {
  // React allocates a new style object on every render — comparing by
  // value rather than identity keeps unchanged styles from defeating the
  // blit fast path.
  if (stylesEqual(node.style, style)) {
    return;
  }
  node.style = style;
  markDirty(node);
};

export const setTextStyles = (
  node: DOMElement,
  textStyles: TextStyles,
): void => {
  // Same rationale as setStyle: buildTextStyles in Text.tsx returns a
  // fresh object each render, and a dirty mark here would force a yoga
  // remeasurement on every Text re-render even when nothing changed.
  if (shallowEqual(node.textStyles, textStyles)) {
    return;
  }
  node.textStyles = textStyles;
  markDirty(node);
};

function stylesEqual(a: Styles, b: Styles): boolean {
  return shallowEqual(a, b);
}

function shallowEqual<T extends object>(
  a: T | undefined,
  b: T | undefined,
): boolean {
  if (a === b) return true;
  if (a === undefined || b === undefined) return false;

  // Hot path during reconciliation: called once per style/textStyles prop
  // diff per node per frame. Keep allocations to a single `Object.keys`
  // call — counting `b`'s keys via `for…in` avoids the second array
  // while still catching the "b has extra keys" case.
  const aKeys = Object.keys(a) as (keyof T)[];
  let bCount = 0;
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  for (const _ in b) bCount++;
  if (aKeys.length !== bCount) return false;

  for (let i = 0; i < aKeys.length; i++) {
    const key = aKeys[i]!;
    if (a[key] !== b[key]) return false;
  }

  return true;
}

export const createTextNode = (text: string): TextNode => {
  const node: TextNode = {
    nodeName: '#text',
    nodeValue: text,
    yogaNode: undefined,
    parentNode: undefined,
    style: {},
  };

  setTextNodeValue(node, text);

  return node;
};

const measureTextNode = function (
  node: DOMNode,
  width: number,
  widthMode: LayoutMeasureMode,
): { width: number; height: number } {
  const rawText =
    node.nodeName === '#text' ? node.nodeValue : squashTextNodes(node);

  // Worst-case expansion (every tab takes 8 cells) is fine for measurement;
  // the renderer in output.ts computes the actual expansion based on the
  // tab's column position at paint time.
  const text = expandTabs(rawText);

  const dimensions = measureText(text, width);

  if (dimensions.width <= width) {
    return dimensions;
  }

  // Edge case: Yoga is shrink-fitting children and asks "can you fit in
  // less than one cell?". The honest answer is no — report our intrinsic
  // width back so Yoga can decide what to clip.
  if (dimensions.width >= 1 && width > 0 && width < 1) {
    return dimensions;
  }

  // Pre-wrapped content (embedded newlines): when Yoga is probing for
  // intrinsic size in Undefined mode, don't let the probe width force
  // additional wrapping — that would inflate height during the min/max
  // size pass and ripple through to the parent box. We respect actual
  // constraints (Exactly / AtMost), because under those modes the
  // rendered width really will be `width` and ignoring it would truncate.
  if (text.includes('\n') && widthMode === LayoutMeasureMode.Undefined) {
    const effectiveWidth = Math.max(width, dimensions.width);
    return measureText(text, effectiveWidth);
  }

  const textWrap = node.style?.textWrap ?? 'wrap';
  const wrappedText = wrapText(text, width, textWrap);

  return measureText(wrappedText, width);
};

// ink-raw-ansi nodes hold pre-rendered ANSI strings with dimensions
// declared up front (their producer — e.g. the diff viewer — already
// wrapped to the target width). Skip stringWidth, wrap, and tab expansion.
const measureRawAnsiNode = function (node: DOMElement): {
  width: number;
  height: number;
} {
  return {
    width: node.attributes['rawWidth'] as number,
    height: node.attributes['rawHeight'] as number,
  };
};

export const markDirty = (node?: DOMNode): void => {
  let current: DOMNode | undefined = node;
  let markedYoga = false;

  while (current) {
    if (current.nodeName !== '#text') {
      (current as DOMElement).dirty = true;
      // Yoga only caches measurements for nodes that have a measure func.
      // We only need to invalidate the one closest to the change.
      if (
        !markedYoga &&
        (current.nodeName === 'ink-text' ||
          current.nodeName === 'ink-raw-ansi') &&
        current.yogaNode
      ) {
        current.yogaNode.markDirty();
        markedYoga = true;
      }
    }
    current = current.parentNode;
  }
};

/** Walk to the root and trigger its throttled `onRender`. */
export const scheduleRenderFrom = (node?: DOMNode): void => {
  let cur: DOMNode | undefined = node;
  while (cur?.parentNode) cur = cur.parentNode;
  if (cur && cur.nodeName !== '#text') (cur as DOMElement).onRender?.();
};

export const setTextNodeValue = (node: TextNode, text: string): void => {
  if (typeof text !== 'string') {
    text = String(text);
  }

  if (node.nodeValue === text) {
    return;
  }

  node.nodeValue = text;
  markDirty(node);
};

function isDOMElement(node: DOMElement | TextNode): node is DOMElement {
  return node.nodeName !== '#text';
}

/** Recursively null out yogaNode references on a subtree. */
export const clearYogaNodeReferences = (node: DOMElement | TextNode): void => {
  if ('childNodes' in node) {
    for (const child of node.childNodes) {
      clearYogaNodeReferences(child);
    }
  }
  node.yogaNode = undefined;
};

/** Find the React component stack responsible for content at screen row `y`. */
export function findOwnerChainAtRow(root: DOMElement, y: number): string[] {
  let best: string[] = [];
  walk(root, 0);
  return best;

  function walk(node: DOMElement, offsetY: number): void {
    const yoga = node.yogaNode;
    if (!yoga || yoga.getDisplay() === LayoutDisplay.None) return;

    const top = offsetY + yoga.getComputedTop();
    const height = yoga.getComputedHeight();
    if (y < top || y >= top + height) return;

    if (node.debugOwnerChain) best = node.debugOwnerChain;

    for (const child of node.childNodes) {
      if (isDOMElement(child)) walk(child, top);
    }
  }
}
