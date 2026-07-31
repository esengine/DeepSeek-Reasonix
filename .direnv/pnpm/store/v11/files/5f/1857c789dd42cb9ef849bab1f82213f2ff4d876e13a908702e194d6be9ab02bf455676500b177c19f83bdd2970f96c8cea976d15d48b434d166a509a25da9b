/**
 * utility.js
 */

/* import */
import nwsapi from '@asamuzakjp/nwsapi';
import bidiFactory from 'bidi-js';
import * as cssTree from 'css-tree';
import isCustomElementName from 'is-potential-custom-element-name';

/* constants */
import {
  ATRULE,
  COMBO,
  COMPOUND_I,
  DESCEND,
  DOCUMENT_FRAGMENT_NODE,
  DOCUMENT_NODE,
  DOCUMENT_POSITION_CONTAINS,
  DOCUMENT_POSITION_PRECEDING,
  ELEMENT_NODE,
  HAS_COMPOUND,
  INPUT_BUTTON,
  INPUT_EDIT,
  INPUT_LTR,
  INPUT_TEXT,
  KEYS_LOGICAL,
  LOGIC_COMPLEX,
  LOGIC_COMPOUND,
  N_TH,
  PSEUDO_CLASS,
  RULE,
  SCOPE,
  SELECTOR_LIST,
  TAG_TYPE,
  TARGET_ALL,
  TARGET_FIRST,
  TEXT_NODE,
  TYPE_FROM,
  TYPE_TO
} from './constant.js';
const KEYS_DIR_AUTO = new Set([...INPUT_BUTTON, ...INPUT_TEXT, 'hidden']);
const KEYS_DIR_LTR = new Set(INPUT_LTR);
const KEYS_INPUT_EDIT = new Set(INPUT_EDIT);
const KEYS_NODE_DIR_EXCLUDE = new Set(['bdi', 'script', 'style', 'textarea']);
const KEYS_NODE_FOCUSABLE = new Set(['button', 'select', 'textarea']);
const KEYS_NODE_FOCUSABLE_SVG = new Set([
  'clipPath',
  'defs',
  'desc',
  'linearGradient',
  'marker',
  'mask',
  'metadata',
  'pattern',
  'radialGradient',
  'script',
  'style',
  'symbol',
  'title'
]);
const REG_ATTR_SIMPLE = /^\[[A-Z\d-]{1,255}(?:="?[A-Z\d\s-]{1,255}"?)?\]$/i;
const REG_TAG_SIMPLE = new RegExp(`^(?:${TAG_TYPE})$`);
const REG_EXCLUDE_BASIC =
  /[|\\]|::|[^\u0021-\u007F\s]|\[\s*[\w$*=^|~-]+(?:(?:"[\w$*=^|~\s'-]+"|'[\w$*=^|~\s"-]+')?(?:\s+[\w$*=^|~-]+)+|"[^"\]]{1,255}|'[^'\]]{1,255})\s*\]|:(?:is|where)\(\s*\)/;
const REG_COMPLEX = new RegExp(`${COMPOUND_I}${COMBO}${COMPOUND_I}`, 'i');
const REG_DESCEND = new RegExp(`${COMPOUND_I}${DESCEND}${COMPOUND_I}`, 'i');
const REG_LOGIC_COMPLEX = new RegExp(
  `:(?!${PSEUDO_CLASS}|${N_TH}|${LOGIC_COMPLEX})`
);
const REG_LOGIC_COMPOUND = new RegExp(
  `:(?!${PSEUDO_CLASS}|${N_TH}|${LOGIC_COMPOUND})`
);
const REG_LOGIC_HAS_COMPOUND = new RegExp(
  `:(?!${PSEUDO_CLASS}|${N_TH}|${LOGIC_COMPOUND}|${HAS_COMPOUND})`
);
const REG_END_WITH_HAS = new RegExp(`:${HAS_COMPOUND}$`);
const REG_WO_LOGICAL = new RegExp(`:(?!${PSEUDO_CLASS}|${N_TH})`);
const REG_IS_HTML = /^(?:application\/xhtml\+x|text\/ht)ml$/;
const REG_IS_XML =
  /^(?:application\/(?:[\w\-.]+\+)?|image\/[\w\-.]+\+|text\/)xml$/;

/**
 * Manages state for extracting nested selectors from a CSS AST.
 */
class SelectorExtractor {
  constructor() {
    this.selectors = [];
    this.isScoped = false;
  }

  /**
   * Walker enter function.
   * @param {object} node - The AST node.
   */
  enter(node) {
    switch (node.type) {
      case ATRULE: {
        if (node.name === 'scope') {
          this.isScoped = true;
        }
        break;
      }
      case SCOPE: {
        const { children, type } = node.root;
        const arr = [];
        if (type === SELECTOR_LIST) {
          for (const child of children) {
            const selector = cssTree.generate(child);
            arr.push(selector);
          }
          this.selectors.push(arr);
        }
        break;
      }
      case RULE: {
        const { children, type } = node.prelude;
        const arr = [];
        if (type === SELECTOR_LIST) {
          let hasAmp = false;
          for (const child of children) {
            const selector = cssTree.generate(child);
            if (this.isScoped && !hasAmp) {
              hasAmp = /\x26/.test(selector);
            }
            arr.push(selector);
          }
          if (this.isScoped) {
            if (hasAmp) {
              this.selectors.push(arr);
              /* FIXME:
              } else {
                this.selectors = arr;
                this.isScoped = false;
              */
            }
          } else {
            this.selectors.push(arr);
          }
        }
      }
    }
  }

  /**
   * Walker leave function.
   * @param {object} node - The AST node.
   */
  leave(node) {
    if (node.type === ATRULE) {
      if (node.name === 'scope') {
        this.isScoped = false;
      }
    }
  }
}

/**
 * Get type of an object.
 * @param {object} o - Object to check.
 * @returns {string} - Type of the object.
 */
export const getType = o =>
  Object.prototype.toString.call(o).slice(TYPE_FROM, TYPE_TO);

/**
 * Verify array contents.
 * @param {Array} arr - The array.
 * @param {string} type - Expected type, e.g. 'String'.
 * @throws {TypeError} - Throws if array or its items are of unexpected type.
 * @returns {Array} - The verified array.
 */
export const verifyArray = (arr, type) => {
  if (!Array.isArray(arr)) {
    throw new TypeError(`Unexpected type ${getType(arr)}`);
  }
  if (typeof type !== 'string') {
    throw new TypeError(`Unexpected type ${getType(type)}`);
  }
  for (const item of arr) {
    if (getType(item) !== type) {
      throw new TypeError(`Unexpected type ${getType(item)}`);
    }
  }
  return arr;
};

/**
 * Generate a DOMException.
 * @param {string} msg - The error message.
 * @param {string} name - The error name.
 * @param {object} globalObject - The global object (e.g., window).
 * @returns {DOMException} The generated DOMException object.
 */
export const generateException = (msg, name, globalObject = globalThis) => {
  return new globalObject.DOMException(msg, name);
};

/**
 * Find a nested :has() pseudo-class.
 * @param {object} leaf - The AST leaf to check.
 * @returns {?object} The leaf if it's :has, otherwise null.
 */
export const findNestedHas = leaf => {
  return leaf.name === 'has';
};

/**
 * Find a logical pseudo-class that contains a nested :has().
 * @param {object} leaf - The AST leaf to check.
 * @returns {?object} The leaf if it matches, otherwise null.
 */
export const findLogicalWithNestedHas = leaf => {
  if (KEYS_LOGICAL.has(leaf.name) && cssTree.find(leaf, findNestedHas)) {
    return leaf;
  }
  return null;
};

/**
 * Filter a list of nodes based on An+B logic
 * @param {Array.<object>} nodes - array of nodes to filter
 * @param {object} anb - An+B options
 * @param {number} anb.a - a
 * @param {number} anb.b - b
 * @param {boolean} [anb.reverse] - reverse order
 * @returns {Array.<object>} - array of matched nodes
 */
export const filterNodesByAnB = (nodes, anb) => {
  const { a, b, reverse } = anb;
  const processedNodes = reverse ? [...nodes].reverse() : nodes;
  const l = nodes.length;
  const matched = [];
  if (a === 0) {
    if (b > 0 && b <= l) {
      matched.push(processedNodes[b - 1]);
    }
    return matched;
  }
  let startIndex = b - 1;
  if (a > 0) {
    while (startIndex < 0) {
      startIndex += a;
    }
    for (let i = startIndex; i < l; i += a) {
      matched.push(processedNodes[i]);
    }
  } else if (startIndex >= 0) {
    for (let i = startIndex; i >= 0; i += a) {
      matched.push(processedNodes[i]);
    }
    return matched.reverse();
  }
  return matched;
};

/**
 * Resolve content document, root node, and check if it's in a shadow DOM.
 * @param {object} node - Document, DocumentFragment, or Element node.
 * @returns {Array.<object|boolean>} - [document, root, isInShadow].
 */
export const resolveContent = node => {
  if (!node?.nodeType) {
    throw new TypeError(`Unexpected type ${getType(node)}`);
  }
  let document;
  let root;
  let shadow;
  switch (node.nodeType) {
    case DOCUMENT_NODE: {
      document = node;
      root = node;
      break;
    }
    case DOCUMENT_FRAGMENT_NODE: {
      const { host, mode, ownerDocument } = node;
      document = ownerDocument;
      root = node;
      shadow = host && (mode === 'close' || mode === 'open');
      break;
    }
    case ELEMENT_NODE: {
      document = node.ownerDocument;
      let refNode = node;
      while (refNode) {
        const { host, mode, nodeType, parentNode } = refNode;
        if (nodeType === DOCUMENT_FRAGMENT_NODE) {
          shadow = host && (mode === 'close' || mode === 'open');
          break;
        } else if (parentNode) {
          refNode = parentNode;
        } else {
          break;
        }
      }
      root = refNode;
      break;
    }
    default: {
      throw new TypeError(`Unexpected node ${node.nodeName}`);
    }
  }
  return [document, root, !!shadow];
};

/**
 * Traverse node tree with a TreeWalker.
 * @param {object} node - The target node.
 * @param {object} walker - The TreeWalker instance.
 * @param {boolean} [force] - Traverse only to the next node.
 * @returns {?object} - The current node if found, otherwise null.
 */
export const traverseNode = (node, walker, force = false) => {
  if (!node?.nodeType) {
    throw new TypeError(`Unexpected type ${getType(node)}`);
  }
  if (!walker) {
    return null;
  }
  let refNode = walker.currentNode;
  if (refNode === node) {
    return refNode;
  } else if (force || refNode.contains(node)) {
    refNode = walker.nextNode();
    while (refNode) {
      if (refNode === node) {
        break;
      }
      refNode = walker.nextNode();
    }
    return refNode;
  } else {
    if (refNode !== walker.root) {
      let bool;
      while (refNode) {
        if (refNode === node) {
          bool = true;
          break;
        } else if (refNode === walker.root || refNode.contains(node)) {
          break;
        }
        refNode = walker.parentNode();
      }
      if (bool) {
        return refNode;
      }
    }
    if (node.nodeType === ELEMENT_NODE) {
      let bool;
      while (refNode) {
        if (refNode === node) {
          bool = true;
          break;
        }
        refNode = walker.nextNode();
      }
      if (bool) {
        return refNode;
      }
    }
  }
  return null;
};

/**
 * Check if a node is a custom element.
 * @param {object} node - The Element node.
 * @param {object} [opt] - Options.
 * @returns {boolean} - True if it's a custom element.
 */
export const isCustomElement = (node, opt = {}) => {
  if (!node?.nodeType) {
    throw new TypeError(`Unexpected type ${getType(node)}`);
  }
  if (node.nodeType !== ELEMENT_NODE) {
    return false;
  }
  const { localName, ownerDocument } = node;
  const { formAssociated } = opt;
  const window = ownerDocument.defaultView;
  let elmConstructor;
  const attr = node.getAttribute('is');
  if (attr) {
    elmConstructor =
      isCustomElementName(attr) && window.customElements.get(attr);
  } else {
    elmConstructor =
      isCustomElementName(localName) && window.customElements.get(localName);
  }
  if (elmConstructor) {
    if (formAssociated) {
      return !!elmConstructor.formAssociated;
    }
    return true;
  }
  return false;
};

/**
 * Get slotted text content.
 * @param {object} node - The Element node (likely a <slot>).
 * @returns {?string} - The text content.
 */
export const getSlottedTextContent = node => {
  if (!node?.nodeType) {
    throw new TypeError(`Unexpected type ${getType(node)}`);
  }
  if (typeof node.assignedNodes !== 'function') {
    return null;
  }
  const nodes = node.assignedNodes();
  if (nodes.length) {
    let text = '';
    const l = nodes.length;
    for (let i = 0; i < l; i++) {
      const item = nodes[i];
      text = item.textContent.trim();
      if (text) {
        break;
      }
    }
    return text;
  }
  return node.textContent.trim();
};

/**
 * Get directionality of a node.
 * @see https://html.spec.whatwg.org/multipage/dom.html#the-dir-attribute
 * @param {object} node - The Element node.
 * @returns {?string} - 'ltr' or 'rtl'.
 */
export const getDirectionality = node => {
  if (!node?.nodeType) {
    throw new TypeError(`Unexpected type ${getType(node)}`);
  }
  if (node.nodeType !== ELEMENT_NODE) {
    return null;
  }
  const { dir: dirAttr, localName, parentNode } = node;
  const { getEmbeddingLevels } = bidiFactory();
  if (dirAttr === 'ltr' || dirAttr === 'rtl') {
    return dirAttr;
  } else if (dirAttr === 'auto') {
    let text = '';
    switch (localName) {
      case 'input': {
        if (!node.type || KEYS_DIR_AUTO.has(node.type)) {
          text = node.value;
        } else if (KEYS_DIR_LTR.has(node.type)) {
          return 'ltr';
        }
        break;
      }
      case 'slot': {
        text = getSlottedTextContent(node);
        break;
      }
      case 'textarea': {
        text = node.value;
        break;
      }
      default: {
        const items = [].slice.call(node.childNodes);
        for (const item of items) {
          const {
            dir: itemDir,
            localName: itemLocalName,
            nodeType: itemNodeType,
            textContent: itemTextContent
          } = item;
          if (itemNodeType === TEXT_NODE) {
            text = itemTextContent.trim();
          } else if (
            itemNodeType === ELEMENT_NODE &&
            !KEYS_NODE_DIR_EXCLUDE.has(itemLocalName) &&
            (!itemDir || (itemDir !== 'ltr' && itemDir !== 'rtl'))
          ) {
            if (itemLocalName === 'slot') {
              text = getSlottedTextContent(item);
            } else {
              text = itemTextContent.trim();
            }
          }
          if (text) {
            break;
          }
        }
      }
    }
    if (text) {
      const {
        paragraphs: [{ level }]
      } = getEmbeddingLevels(text);
      if (level % 2 === 1) {
        return 'rtl';
      }
    } else if (parentNode) {
      const { nodeType: parentNodeType } = parentNode;
      if (parentNodeType === ELEMENT_NODE) {
        return getDirectionality(parentNode);
      }
    }
  } else if (localName === 'input' && node.type === 'tel') {
    return 'ltr';
  } else if (localName === 'bdi') {
    const text = node.textContent.trim();
    if (text) {
      const {
        paragraphs: [{ level }]
      } = getEmbeddingLevels(text);
      if (level % 2 === 1) {
        return 'rtl';
      }
    }
  } else if (parentNode) {
    if (localName === 'slot') {
      const text = getSlottedTextContent(node);
      if (text) {
        const {
          paragraphs: [{ level }]
        } = getEmbeddingLevels(text);
        if (level % 2 === 1) {
          return 'rtl';
        }
        return 'ltr';
      }
    }
    const { nodeType: parentNodeType } = parentNode;
    if (parentNodeType === ELEMENT_NODE) {
      return getDirectionality(parentNode);
    }
  }
  return 'ltr';
};

/**
 * Traverses up the DOM tree to find the language attribute for a node.
 * It checks for 'lang' in HTML and 'xml:lang' in XML contexts.
 * @param {object} node - The starting element node.
 * @returns {string|null} The language attribute value, or null if not found.
 */
export const getLanguageAttribute = node => {
  if (!node?.nodeType) {
    throw new TypeError(`Unexpected type ${getType(node)}`);
  }
  if (node.nodeType !== ELEMENT_NODE) {
    return null;
  }
  const { contentType } = node.ownerDocument;
  const isHtml = REG_IS_HTML.test(contentType);
  const isXml = REG_IS_XML.test(contentType);
  let isShadow = false;
  // Traverse up from the current node to the root.
  let current = node;
  while (current) {
    // Check if the current node is an element.
    switch (current.nodeType) {
      case ELEMENT_NODE: {
        // Check for and return the language attribute if present.
        if (isHtml && current.hasAttribute('lang')) {
          return current.getAttribute('lang');
        } else if (isXml && current.hasAttribute('xml:lang')) {
          return current.getAttribute('xml:lang');
        }
        break;
      }
      case DOCUMENT_FRAGMENT_NODE: {
        // Continue traversal if the current node is a shadow root.
        if (current.host) {
          isShadow = true;
        }
        break;
      }
      case DOCUMENT_NODE:
      default: {
        // Stop if we reach the root document node.
        return null;
      }
    }
    if (isShadow) {
      current = current.host;
      isShadow = false;
    } else if (current.parentNode) {
      current = current.parentNode;
    } else {
      break;
    }
  }
  // No language attribute was found in the hierarchy.
  return null;
};

/**
 * Check if content is editable.
 * NOTE: Not implemented in jsdom https://github.com/jsdom/jsdom/issues/1670
 * @param {object} node - The Element node.
 * @returns {boolean} - True if content is editable.
 */
export const isContentEditable = node => {
  if (!node?.nodeType) {
    throw new TypeError(`Unexpected type ${getType(node)}`);
  }
  if (node.nodeType !== ELEMENT_NODE) {
    return false;
  }
  if (typeof node.isContentEditable === 'boolean') {
    return node.isContentEditable;
  } else if (node.ownerDocument.designMode === 'on') {
    return true;
  } else {
    let attr;
    if (node.hasAttribute('contenteditable')) {
      attr = node.getAttribute('contenteditable');
    } else {
      attr = 'inherit';
    }
    switch (attr) {
      case '':
      case 'true': {
        return true;
      }
      case 'plaintext-only': {
        // FIXME:
        // @see https://github.com/w3c/editing/issues/470
        // @see https://github.com/whatwg/html/issues/10651
        return true;
      }
      case 'false': {
        return false;
      }
      default: {
        if (node?.parentNode?.nodeType === ELEMENT_NODE) {
          return isContentEditable(node.parentNode);
        }
        return false;
      }
    }
  }
};

/**
 * Check if a node is visible.
 * @param {object} node - The Element node.
 * @returns {boolean} - True if the node is visible.
 */
export const isVisible = node => {
  if (node?.nodeType !== ELEMENT_NODE) {
    return false;
  }
  // TODO: switch to node.checkVisibility()
  const window = node.ownerDocument.defaultView;
  const { display, visibility } = window.getComputedStyle(node);
  return display !== 'none' && visibility === 'visible';
};

/**
 * Check if focus is visible on the node.
 * @param {object} node - The Element node.
 * @returns {boolean} - True if focus is visible.
 */
export const isFocusVisible = node => {
  if (node?.nodeType !== ELEMENT_NODE) {
    return false;
  }
  const { localName, type } = node;
  switch (localName) {
    case 'input': {
      if (!type || KEYS_INPUT_EDIT.has(type)) {
        return true;
      }
      return false;
    }
    case 'textarea': {
      return true;
    }
    default: {
      return isContentEditable(node);
    }
  }
};

/**
 * Check if an area is focusable.
 * @param {object} node - The Element node.
 * @returns {boolean} - True if the area is focusable.
 */
export const isFocusableArea = node => {
  if (node?.nodeType !== ELEMENT_NODE) {
    return false;
  }
  if (!node.isConnected) {
    return false;
  }
  const window = node.ownerDocument.defaultView;
  if (node instanceof window.HTMLElement) {
    if (Number.isInteger(parseInt(node.getAttribute('tabindex')))) {
      return true;
    }
    if (isContentEditable(node)) {
      return true;
    }
    const { localName, parentNode } = node;
    switch (localName) {
      case 'a': {
        if (node.href || node.hasAttribute('href')) {
          return true;
        }
        return false;
      }
      case 'iframe': {
        return true;
      }
      case 'input': {
        if (
          node.disabled ||
          node.hasAttribute('disabled') ||
          node.hidden ||
          node.hasAttribute('hidden')
        ) {
          return false;
        }
        return true;
      }
      case 'summary': {
        if (parentNode.localName === 'details') {
          let child = parentNode.firstElementChild;
          let bool = false;
          while (child) {
            if (child.localName === 'summary') {
              bool = child === node;
              break;
            }
            child = child.nextElementSibling;
          }
          return bool;
        }
        return false;
      }
      default: {
        if (
          KEYS_NODE_FOCUSABLE.has(localName) &&
          !(node.disabled || node.hasAttribute('disabled'))
        ) {
          return true;
        }
      }
    }
  } else if (node instanceof window.SVGElement) {
    if (Number.isInteger(parseInt(node.getAttributeNS(null, 'tabindex')))) {
      const ns = 'http://www.w3.org/2000/svg';
      let bool;
      let refNode = node;
      while (refNode.namespaceURI === ns) {
        bool = KEYS_NODE_FOCUSABLE_SVG.has(refNode.localName);
        if (bool) {
          break;
        }
        if (refNode?.parentNode?.namespaceURI === ns) {
          refNode = refNode.parentNode;
        } else {
          break;
        }
      }
      if (bool) {
        return false;
      }
      return true;
    }
    if (
      node.localName === 'a' &&
      (node.href || node.hasAttributeNS(null, 'href'))
    ) {
      return true;
    }
  }
  return false;
};

/**
 * Check if a node is focusable.
 * NOTE: Not applied, needs fix in jsdom itself.
 * @see https://github.com/whatwg/html/pull/8392
 * @see https://phabricator.services.mozilla.com/D156219
 * @see https://github.com/jsdom/jsdom/issues/3029
 * @see https://github.com/jsdom/jsdom/issues/3464
 * @param {object} node - The Element node.
 * @returns {boolean} - True if the node is focusable.
 */
export const isFocusable = node => {
  if (node?.nodeType !== ELEMENT_NODE) {
    return false;
  }
  const window = node.ownerDocument.defaultView;
  let refNode = node;
  let res = true;
  while (refNode) {
    if (refNode.disabled || refNode.hasAttribute('disabled')) {
      res = false;
      break;
    }
    if (refNode.hidden || refNode.hasAttribute('hidden')) {
      res = false;
    }
    const { contentVisibility, display, visibility } =
      window.getComputedStyle(refNode);
    if (
      display === 'none' ||
      visibility !== 'visible' ||
      (contentVisibility === 'hidden' && refNode !== node)
    ) {
      res = false;
    } else {
      res = true;
    }
    if (res && refNode?.parentNode?.nodeType === ELEMENT_NODE) {
      refNode = refNode.parentNode;
    } else {
      break;
    }
  }
  return res;
};

/**
 * Get namespace URI.
 * @param {string} ns - The namespace prefix.
 * @param {object} node - The Element node.
 * @returns {?string} - The namespace URI.
 */
export const getNamespaceURI = (ns, node) => {
  if (typeof ns !== 'string') {
    throw new TypeError(`Unexpected type ${getType(ns)}`);
  } else if (!node?.nodeType) {
    throw new TypeError(`Unexpected type ${getType(node)}`);
  }
  if (!ns || node.nodeType !== ELEMENT_NODE) {
    return null;
  }
  const { attributes } = node;
  let res;
  for (const attr of attributes) {
    const { name, namespaceURI, prefix, value } = attr;
    if (name === `xmlns:${ns}`) {
      res = value;
    } else if (prefix === ns) {
      res = namespaceURI;
    }
    if (res) {
      break;
    }
  }
  return res ?? null;
};

/**
 * Check if a namespace is declared.
 * @param {string} ns - The namespace.
 * @param {object} node - The Element node.
 * @returns {boolean} - True if the namespace is declared.
 */
export const isNamespaceDeclared = (ns = '', node = {}) => {
  if (!ns || typeof ns !== 'string' || node?.nodeType !== ELEMENT_NODE) {
    return false;
  }
  if (node.lookupNamespaceURI(ns)) {
    return true;
  }
  const root = node.ownerDocument.documentElement;
  let parent = node;
  let res;
  while (parent) {
    res = getNamespaceURI(ns, parent);
    if (res || parent === root) {
      break;
    }
    parent = parent.parentNode;
  }
  return !!res;
};

/**
 * Check if nodeA precedes and/or contains nodeB.
 * @param {object} nodeA - The first Element node.
 * @param {object} nodeB - The second Element node.
 * @returns {boolean} - True if nodeA precedes nodeB.
 */
export const isPreceding = (nodeA, nodeB) => {
  if (!nodeA?.nodeType) {
    throw new TypeError(`Unexpected type ${getType(nodeA)}`);
  } else if (!nodeB?.nodeType) {
    throw new TypeError(`Unexpected type ${getType(nodeB)}`);
  }
  if (nodeA.nodeType !== ELEMENT_NODE || nodeB.nodeType !== ELEMENT_NODE) {
    return false;
  }
  const posBit = nodeB.compareDocumentPosition(nodeA);
  const res =
    posBit & DOCUMENT_POSITION_PRECEDING || posBit & DOCUMENT_POSITION_CONTAINS;
  return !!res;
};

/**
 * Comparison function for sorting nodes based on document position.
 * @param {object} a - The first node.
 * @param {object} b - The second node.
 * @returns {number} - Sort order.
 */
export const compareNodes = (a, b) => {
  if (isPreceding(b, a)) {
    return 1;
  }
  return -1;
};

/**
 * Sort a collection of nodes.
 * @param {Array.<object>|Set.<object>} nodes - Collection of nodes.
 * @returns {Array.<object>} - Collection of sorted nodes.
 */
export const sortNodes = (nodes = []) => {
  const arr = [...nodes];
  if (arr.length > 1) {
    arr.sort(compareNodes);
  }
  return arr;
};

/**
 * Concat an array of nested selectors into an equivalent single selector.
 * @param {Array.<Array.<string>>} selectors - [parents, children, ...].
 * @returns {string} - The concatenated selector.
 */
export const concatNestedSelectors = selectors => {
  if (!Array.isArray(selectors)) {
    throw new TypeError(`Unexpected type ${getType(selectors)}`);
  }
  let selector = '';
  if (selectors.length) {
    const revSelectors = selectors.toReversed();
    let child = verifyArray(revSelectors.shift(), 'String');
    if (child.length === 1) {
      [child] = child;
    }
    while (revSelectors.length) {
      const parentArr = verifyArray(revSelectors.shift(), 'String');
      if (!parentArr.length) {
        continue;
      }
      let parent;
      if (parentArr.length === 1) {
        [parent] = parentArr;
        if (!/^[>~+]/.test(parent) && /[\s>~+]/.test(parent)) {
          parent = `:is(${parent})`;
        }
      } else {
        parent = `:is(${parentArr.join(', ')})`;
      }
      if (selector.includes('\x26')) {
        selector = selector.replace(/\x26/g, parent);
      }
      if (Array.isArray(child)) {
        const items = [];
        for (let item of child) {
          if (item.includes('\x26')) {
            if (/^[>~+]/.test(item)) {
              item = `${parent} ${item.replace(/\x26/g, parent)} ${selector}`;
            } else {
              item = `${item.replace(/\x26/g, parent)} ${selector}`;
            }
          } else {
            item = `${parent} ${item} ${selector}`;
          }
          items.push(item.trim());
        }
        selector = items.join(', ');
      } else if (revSelectors.length) {
        selector = `${child} ${selector}`;
      } else {
        if (child.includes('\x26')) {
          if (/^[>~+]/.test(child)) {
            selector = `${parent} ${child.replace(/\x26/g, parent)} ${selector}`;
          } else {
            selector = `${child.replace(/\x26/g, parent)} ${selector}`;
          }
        } else {
          selector = `${parent} ${child} ${selector}`;
        }
      }
      selector = selector.trim();
      if (revSelectors.length) {
        child = parentArr.length > 1 ? parentArr : parent;
      } else {
        break;
      }
    }
    selector = selector.replace(/\x26/g, ':scope').trim();
  }
  return selector;
};

/**
 * Extract nested selectors from CSSRule.cssText.
 * @param {string} css - CSSRule.cssText.
 * @returns {Array.<Array.<string>>} - Array of nested selectors.
 */
export const extractNestedSelectors = css => {
  const ast = cssTree.parse(css, {
    context: 'rule'
  });
  const extractor = new SelectorExtractor();
  cssTree.walk(ast, {
    enter: extractor.enter.bind(extractor),
    leave: extractor.leave.bind(extractor)
  });
  return extractor.selectors;
};

/**
 * Initialize nwsapi.
 * @param {object} window - The Window object.
 * @param {object} document - The Document object.
 * @returns {object} - The nwsapi instance.
 */
export const initNwsapi = (window, document) => {
  if (!window?.DOMException) {
    throw new TypeError(`Unexpected global object ${getType(window)}`);
  }
  if (document?.nodeType !== DOCUMENT_NODE) {
    document = window.document;
  }
  const nw = nwsapi({
    document,
    DOMException: window.DOMException
  });
  nw.configure({
    LOGERRORS: false
  });
  return nw;
};

/**
 * Filter a selector for use with nwsapi.
 * @param {string} selector - The selector string.
 * @param {string} target - The target type.
 * @returns {boolean} - True if the selector is valid for nwsapi.
 */
export const filterSelector = (selector, target) => {
  const isQuerySelectorAll = target === TARGET_ALL;
  // Basic validation and fast-fail for null/undefined/non-string values
  if (
    !selector ||
    typeof selector !== 'string' ||
    /null|undefined/.test(selector)
  ) {
    return false;
  }
  // Exclude various complex or unsupported selectors early
  // i.e. non-ASCII, escaped selectors, namespaced selectors and pseudo-elements
  if (selector.includes('/') || REG_EXCLUDE_BASIC.test(selector)) {
    return false;
  }
  // Validate attribute selector integrity
  if (selector.includes('[')) {
    const index = selector.lastIndexOf('[');
    if (selector.indexOf(']', index) === -1) {
      return false;
    }
  }
  // Target-specific early exits
  if (target === TARGET_FIRST) {
    return REG_ATTR_SIMPLE.test(selector);
  }
  if (target === TARGET_ALL && REG_TAG_SIMPLE.test(selector)) {
    return false;
  }
  // Logic for pseudo-classes
  if (selector.includes(':')) {
    // Exclude descendant combinators in logical selectors for querySelectorAll
    if (isQuerySelectorAll && REG_DESCEND.test(selector)) {
      return false;
    }
    // Determine if the selector has complex logical structures
    const isComplex = isQuerySelectorAll ? false : REG_COMPLEX.test(selector);
    // Handle :has() specifically
    if (selector.includes(':has(')) {
      if (isQuerySelectorAll) {
        return false;
      }
      if (!isComplex || REG_LOGIC_HAS_COMPOUND.test(selector)) {
        return false;
      }
      return REG_END_WITH_HAS.test(selector);
    }
    // Handle :is() and :not()
    if (/(?:is|not)\(/.test(selector)) {
      if (isComplex) {
        return !REG_LOGIC_COMPLEX.test(selector);
      } else {
        return !REG_LOGIC_COMPOUND.test(selector);
      }
    }
    // Default check for other pseudo-classes against known list
    if (REG_WO_LOGICAL.test(selector)) {
      return false;
    }
  }
  return true;
};
