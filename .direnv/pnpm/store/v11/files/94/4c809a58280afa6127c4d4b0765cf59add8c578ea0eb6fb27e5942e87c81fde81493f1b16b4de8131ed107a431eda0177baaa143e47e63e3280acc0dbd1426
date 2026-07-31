/*!
 * DOM Selector - A CSS selector engine.
 * @license MIT
 * @copyright asamuzaK (Kazz)
 * @see {@link https://github.com/asamuzaK/domSelector/blob/main/LICENSE}
 */

/* import */
import { GenerationalCache } from '@asamuzakjp/generational-cache';
import { Finder } from './js/finder.js';
import { unescapeSelector, parseAstName } from './js/parser.js';
import { filterSelector, getType, initNwsapi } from './js/utility.js';

/* constants */
import {
  DOCUMENT_NODE,
  DOCUMENT_FRAGMENT_NODE,
  ELEMENT_NODE,
  TARGET_ALL,
  TARGET_FIRST,
  TARGET_LINEAL,
  TARGET_SELF,
  COMBINATOR,
  ID_SELECTOR,
  CLASS_SELECTOR,
  TYPE_SELECTOR
} from './js/constant.js';
const CACHE_SIZE = 2048;

/**
 * @typedef {object} CheckResult
 * @property {boolean} match - The match result.
 * @property {string?} pseudoElement - The pseudo-element, if any.
 * @property {object?} ast - The AST object.
 */

/* DOMSelector */
export class DOMSelector {
  /* private fields */
  #window;
  #document;
  #finder;
  #idlUtils;
  #nwsapi;
  #cache;

  /**
   * Creates an instance of DOMSelector.
   * @param {Window} window - The window object.
   * @param {Document} document - The document object.
   * @param {object} [opt] - Options.
   */
  constructor(window, document, opt = {}) {
    const { cacheSize, idlUtils } = opt;
    this.#window = window;
    this.#document = document ?? window.document;
    this.#finder = new Finder(window);
    this.#idlUtils = idlUtils;
    this.#nwsapi = initNwsapi(window, document);
    this.#cache = new GenerationalCache(cacheSize ?? CACHE_SIZE);
  }

  /**
   * Clears the internal cache of finder results.
   * @returns {void}
   */
  clear = () => {
    this.#finder.clearResults(true);
  };

  /**
   * Parses a selector and extracts the rightmost subject keys (Id, Class, Tag).
   * @param {string} selector - The CSS selector to parse.
   * @returns {Array<{id: string|null, className: string|null, tag: string|null}>} The list of extracted keys for each selector group.
   */
  extractSubjects = selector => {
    if (!selector || typeof selector !== 'string') {
      return [{ id: null, className: null, tag: null }];
    }
    const cacheKey = `extract_${selector}`;
    let subjects = this.#cache.get(cacheKey);
    if (subjects !== undefined) {
      return subjects;
    }
    subjects = [];
    try {
      const ast = this.#finder.getAST(selector);
      if (ast?.type === 'SelectorList') {
        for (const selectorNode of ast.children) {
          let idKey = null;
          let classKey = null;
          let tagKey = null;
          let current = selectorNode.children.tail;
          while (current) {
            const node = current.data;
            if (node.type === COMBINATOR) {
              break;
            }
            if (node.type === ID_SELECTOR) {
              idKey = idKey ?? unescapeSelector(node.name);
            } else if (node.type === CLASS_SELECTOR) {
              classKey = classKey ?? unescapeSelector(node.name);
            } else if (node.type === TYPE_SELECTOR) {
              const { localName } = parseAstName(unescapeSelector(node.name));
              if (localName !== '*') {
                tagKey = tagKey ?? localName.toLowerCase();
              }
            }
            current = current.prev;
          }
          subjects.push({ id: idKey, className: classKey, tag: tagKey });
        }
      }
    } catch (e) {
      // fall through
    }
    if (!subjects.length) {
      subjects.push({ id: null, className: null, tag: null });
    }
    this.#cache.set(cacheKey, subjects);
    return subjects;
  };

  /**
   * Checks if an element matches a CSS selector.
   * @param {string} selector - The CSS selector to check against.
   * @param {Element} node - The element node to check.
   * @param {object} [opt] - Optional parameters.
   * @returns {CheckResult} An object containing the check result.
   */
  check = (selector, node, opt = {}) => {
    if (!node?.nodeType) {
      const e = new this.#window.TypeError(`Unexpected type ${getType(node)}`);
      return this.#finder.onError(e, opt);
    } else if (node.nodeType !== ELEMENT_NODE) {
      const e = new this.#window.TypeError(`Unexpected node ${node.nodeName}`);
      return this.#finder.onError(e, opt);
    }
    const document = node.ownerDocument;
    if (
      document === this.#document &&
      document.contentType === 'text/html' &&
      document.documentElement &&
      node.parentNode
    ) {
      const cacheKey = `check_${selector}`;
      let filterMatches = this.#cache.get(cacheKey);
      if (filterMatches === undefined) {
        filterMatches = filterSelector(selector, TARGET_SELF);
        this.#cache.set(cacheKey, filterMatches);
      }
      if (filterMatches) {
        try {
          const n = this.#idlUtils ? this.#idlUtils.wrapperForImpl(node) : node;
          const match = this.#nwsapi.match(selector, n);
          let ast = null;
          if (match) {
            const astCacheKey = `check_ast_${selector}`;
            ast = this.#cache.get(astCacheKey);
            if (ast === undefined) {
              ast = this.#finder.getAST(selector);
              this.#cache.set(astCacheKey, ast);
            }
          }
          return {
            match,
            ast,
            pseudoElement: null
          };
        } catch (e) {
          // fall through
        }
      }
    }
    if (this.#idlUtils) {
      node = this.#idlUtils.wrapperForImpl(node);
    }
    opt.check = true;
    opt.noexcept = true;
    opt.warn = false;
    return this.#finder.setup(selector, node, opt).find(TARGET_SELF);
  };

  /**
   * Returns true if the element matches the selector.
   * @param {string} selector - The CSS selector to match against.
   * @param {Element} node - The element node to test.
   * @param {object} [opt] - Optional parameters.
   * @returns {boolean} `true` if the element matches, or `false` otherwise.
   */
  matches = (selector, node, opt = {}) => {
    if (!node?.nodeType) {
      const e = new this.#window.TypeError(`Unexpected type ${getType(node)}`);
      return this.#finder.onError(e, opt);
    } else if (node.nodeType !== ELEMENT_NODE) {
      const e = new this.#window.TypeError(`Unexpected node ${node.nodeName}`);
      return this.#finder.onError(e, opt);
    }
    const document = node.ownerDocument;
    if (
      document === this.#document &&
      document.contentType === 'text/html' &&
      document.documentElement &&
      node.parentNode
    ) {
      const cacheKey = `matches_${selector}`;
      let filterMatches = this.#cache.get(cacheKey);
      if (filterMatches === undefined) {
        filterMatches = filterSelector(selector, TARGET_SELF);
        this.#cache.set(cacheKey, filterMatches);
      }
      if (filterMatches) {
        try {
          const n = this.#idlUtils ? this.#idlUtils.wrapperForImpl(node) : node;
          return this.#nwsapi.match(selector, n);
        } catch (e) {
          // fall through
        }
      }
    }
    let res;
    try {
      if (this.#idlUtils) {
        node = this.#idlUtils.wrapperForImpl(node);
      }
      const nodes = this.#finder.setup(selector, node, opt).find(TARGET_SELF);
      res = nodes.size;
    } catch (e) {
      this.#finder.onError(e, opt);
    }
    return !!res;
  };

  /**
   * Traverses up the DOM tree to find the first node that matches the selector.
   * @param {string} selector - The CSS selector to match against.
   * @param {Element} node - The element from which to start traversing.
   * @param {object} [opt] - Optional parameters.
   * @returns {?Element} The first matching ancestor element, or `null`.
   */
  closest = (selector, node, opt = {}) => {
    if (!node?.nodeType) {
      const e = new this.#window.TypeError(`Unexpected type ${getType(node)}`);
      return this.#finder.onError(e, opt);
    } else if (node.nodeType !== ELEMENT_NODE) {
      const e = new this.#window.TypeError(`Unexpected node ${node.nodeName}`);
      return this.#finder.onError(e, opt);
    }
    const document = node.ownerDocument;
    if (
      document === this.#document &&
      document.contentType === 'text/html' &&
      document.documentElement &&
      node.parentNode
    ) {
      const cacheKey = `closest_${selector}`;
      let filterMatches = this.#cache.get(cacheKey);
      if (filterMatches === undefined) {
        filterMatches = filterSelector(selector, TARGET_LINEAL);
        this.#cache.set(cacheKey, filterMatches);
      }
      if (filterMatches) {
        try {
          const n = this.#idlUtils ? this.#idlUtils.wrapperForImpl(node) : node;
          return this.#nwsapi.closest(selector, n);
        } catch (e) {
          // fall through
        }
      }
    }
    let res;
    try {
      if (this.#idlUtils) {
        node = this.#idlUtils.wrapperForImpl(node);
      }
      const nodes = this.#finder.setup(selector, node, opt).find(TARGET_LINEAL);
      if (nodes.size) {
        let refNode = node;
        while (refNode) {
          if (nodes.has(refNode)) {
            res = refNode;
            break;
          }
          refNode = refNode.parentNode;
        }
      }
    } catch (e) {
      this.#finder.onError(e, opt);
    }
    return res ?? null;
  };

  /**
   * Returns the first element within the subtree that matches the selector.
   * @param {string} selector - The CSS selector to match.
   * @param {Document|DocumentFragment|Element} node - The node to find within.
   * @param {object} [opt] - Optional parameters.
   * @returns {?Element} The first matching element, or `null`.
   */
  querySelector = (selector, node, opt = {}) => {
    if (!node?.nodeType) {
      const e = new this.#window.TypeError(`Unexpected type ${getType(node)}`);
      return this.#finder.onError(e, opt);
    }
    const document =
      node.nodeType === DOCUMENT_NODE ? node : node.ownerDocument;
    if (
      document === this.#document &&
      document.contentType === 'text/html' &&
      document.documentElement &&
      (node.nodeType !== DOCUMENT_FRAGMENT_NODE || !node.host)
    ) {
      const cacheKey = `querySelector_${selector}`;
      let filterMatches = this.#cache.get(cacheKey);
      if (filterMatches === undefined) {
        filterMatches = filterSelector(selector, TARGET_FIRST);
        this.#cache.set(cacheKey, filterMatches);
      }
      if (filterMatches) {
        try {
          const n = this.#idlUtils ? this.#idlUtils.wrapperForImpl(node) : node;
          return this.#nwsapi.first(selector, n);
        } catch (e) {
          // fall through
        }
      }
    }
    let res;
    try {
      if (this.#idlUtils) {
        node = this.#idlUtils.wrapperForImpl(node);
      }
      const nodes = this.#finder.setup(selector, node, opt).find(TARGET_FIRST);
      if (nodes.size) {
        [res] = [...nodes];
      }
    } catch (e) {
      this.#finder.onError(e, opt);
    }
    return res ?? null;
  };

  /**
   * Returns an array of elements within the subtree that match the selector.
   * Note: This method returns an Array, not a NodeList.
   * @param {string} selector - The CSS selector to match.
   * @param {Document|DocumentFragment|Element} node - The node to find within.
   * @param {object} [opt] - Optional parameters.
   * @returns {Array<Element>} An array of elements, or an empty array.
   */
  querySelectorAll = (selector, node, opt = {}) => {
    if (!node?.nodeType) {
      const e = new this.#window.TypeError(`Unexpected type ${getType(node)}`);
      return this.#finder.onError(e, opt);
    }
    const document =
      node.nodeType === DOCUMENT_NODE ? node : node.ownerDocument;
    if (
      document === this.#document &&
      document.contentType === 'text/html' &&
      document.documentElement &&
      (node.nodeType !== DOCUMENT_FRAGMENT_NODE || !node.host)
    ) {
      const cacheKey = `querySelectorAll_${selector}`;
      let filterMatches = this.#cache.get(cacheKey);
      if (filterMatches === undefined) {
        filterMatches = filterSelector(selector, TARGET_ALL);
        this.#cache.set(cacheKey, filterMatches);
      }
      if (filterMatches) {
        try {
          const n = this.#idlUtils ? this.#idlUtils.wrapperForImpl(node) : node;
          return this.#nwsapi.select(selector, n);
        } catch (e) {
          // fall through
        }
      }
    }
    let res;
    try {
      if (this.#idlUtils) {
        node = this.#idlUtils.wrapperForImpl(node);
      }
      const nodes = this.#finder.setup(selector, node, opt).find(TARGET_ALL);
      if (nodes.size) {
        res = [...nodes];
      }
    } catch (e) {
      this.#finder.onError(e, opt);
    }
    return res ?? [];
  };
}
