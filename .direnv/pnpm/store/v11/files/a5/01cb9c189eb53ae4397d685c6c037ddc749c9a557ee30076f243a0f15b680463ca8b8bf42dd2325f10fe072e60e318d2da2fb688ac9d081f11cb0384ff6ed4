/**
 * parser.js
 */

/* import */
import * as cssTree from 'css-tree';
import { getType } from './utility.js';

/* constants */
import {
  ATTR_SELECTOR,
  BIT_01,
  BIT_02,
  BIT_04,
  BIT_08,
  BIT_16,
  BIT_32,
  BIT_FFFF,
  CLASS_SELECTOR,
  DUO,
  HEX,
  ID_SELECTOR,
  KEYS_LOGICAL,
  NTH,
  PS_CLASS_SELECTOR,
  PS_ELEMENT_SELECTOR,
  SELECTOR,
  SYNTAX_ERR,
  TYPE_SELECTOR
} from './constant.js';
const AST_SORT_ORDER = new Map([
  [PS_ELEMENT_SELECTOR, BIT_01],
  [ID_SELECTOR, BIT_02],
  [CLASS_SELECTOR, BIT_04],
  [TYPE_SELECTOR, BIT_08],
  [ATTR_SELECTOR, BIT_16],
  [PS_CLASS_SELECTOR, BIT_32]
]);
const KEYS_PS_CLASS_STATE = new Set([
  'checked',
  'closed',
  'disabled',
  'empty',
  'enabled',
  'in-range',
  'indeterminate',
  'invalid',
  'open',
  'out-of-range',
  'placeholder-shown',
  'read-only',
  'read-write',
  'valid'
]);
const KEYS_SHADOW_HOST = new Set(['host', 'host-context']);
const REG_EMPTY_PS_FUNC =
  /(?<=:(?:dir|has|host(?:-context)?|is|lang|not|nth-(?:last-)?(?:child|of-type)|where))\(\s+\)/g;
const REG_SHADOW_PS_ELEMENT = /^part|slotted$/;
const U_FFFD = '\uFFFD';

/**
 * Unescapes a CSS selector string.
 * @param {string} selector - The CSS selector to unescape.
 * @returns {string} The unescaped selector string.
 */
export const unescapeSelector = (selector = '') => {
  if (typeof selector === 'string' && selector.indexOf('\\', 0) >= 0) {
    const arr = selector.split('\\');
    const selectorItems = [arr[0]];
    const l = arr.length;
    for (let i = 1; i < l; i++) {
      const item = arr[i];
      if (item === '' && i === l - 1) {
        selectorItems.push(U_FFFD);
      } else if (item === '') {
        // Empty segment at non-last position means \\ (escaped backslash)
        selectorItems.push('\\');
        i++; // skip the next segment which is the remainder after \\
        if (i < l) {
          selectorItems.push(arr[i]);
        }
      } else {
        const hexExists = /^([\da-f]{1,6}\s?)/i.exec(item);
        if (hexExists) {
          const [, hex] = hexExists;
          let str;
          try {
            const low = parseInt('D800', HEX);
            const high = parseInt('DFFF', HEX);
            const deci = parseInt(hex, HEX);
            if (deci === 0 || (deci >= low && deci <= high)) {
              str = U_FFFD;
            } else {
              str = String.fromCodePoint(deci);
            }
          } catch (e) {
            str = U_FFFD;
          }
          let postStr = '';
          if (item.length > hex.length) {
            postStr = item.substring(hex.length);
          }
          selectorItems.push(`${str}${postStr}`);
          // whitespace
        } else if (/^[\n\r\f]/.test(item)) {
          selectorItems.push(`\\${item}`);
        } else {
          selectorItems.push(item);
        }
      }
    }
    return selectorItems.join('');
  }
  return selector;
};

/**
 * Preprocesses a selector string according to the specification.
 * @see https://drafts.csswg.org/css-syntax-3/#input-preprocessing
 * @param {string} value - The value to preprocess.
 * @returns {string} The preprocessed selector string.
 */
export const preprocess = value => {
  // Non-string values will be converted to string.
  if (typeof value !== 'string') {
    if (value === undefined || value === null) {
      return getType(value).toLowerCase();
    } else if (Array.isArray(value)) {
      return value.join(',');
    } else if (Object.hasOwn(value, 'toString')) {
      return value.toString();
    } else {
      throw new DOMException(`Invalid selector ${value}`, SYNTAX_ERR);
    }
  }
  let selector = value;
  let index = 0;
  while (index >= 0) {
    // @see https://drafts.csswg.org/selectors/#id-selectors
    index = selector.indexOf('#', index);
    if (index < 0) {
      break;
    }
    const preHash = selector.substring(0, index + 1);
    let postHash = selector.substring(index + 1);
    const codePoint = postHash.codePointAt(0);
    if (codePoint > BIT_FFFF) {
      const str = `\\${codePoint.toString(HEX)} `;
      if (postHash.length === DUO) {
        postHash = str;
      } else {
        postHash = `${str}${postHash.substring(DUO)}`;
      }
    }
    selector = `${preHash}${postHash}`;
    index++;
  }
  selector = selector
    .replace(/\f|\r\n?/g, '\n')
    .replace(/[\0\uD800-\uDFFF]|\\$/g, U_FFFD);
  if (selector === '&') {
    return '';
  }
  return selector.replace(/\x26/g, ':scope');
};

/**
 * Creates an Abstract Syntax Tree (AST) from a CSS selector string.
 * @param {string} sel - The CSS selector string.
 * @returns {object} The parsed AST object.
 */
export const parseSelector = sel => {
  const selector = preprocess(sel);
  // invalid selectors
  if (/^$|^\s*>|,\s*$/.test(selector)) {
    throw new DOMException(`Invalid selector ${selector}`, SYNTAX_ERR);
  }
  try {
    return cssTree.parse(selector, {
      context: 'selectorList'
    });
  } catch (e) {
    const { message } = e;
    if (
      /^(?:"\]"|Attribute selector [()\s,=~^$*|]+) is expected$/.test(
        message
      ) &&
      !selector.endsWith(']')
    ) {
      const index = selector.lastIndexOf('[');
      const selPart = selector.substring(index);
      if (selPart.includes('"')) {
        const quotes = selPart.match(/"/g).length;
        if (quotes % 2) {
          return parseSelector(`${selector}"]`);
        }
        return parseSelector(`${selector}]`);
      }
      return parseSelector(`${selector}]`);
    } else if (message === '")" is expected') {
      // workaround for https://github.com/csstree/csstree/issues/283
      if (REG_EMPTY_PS_FUNC.test(selector)) {
        return parseSelector(`${selector.replaceAll(REG_EMPTY_PS_FUNC, '()')}`);
      } else if (!selector.endsWith(')')) {
        return parseSelector(`${selector})`);
      } else {
        throw new DOMException(`Invalid selector ${selector}`, SYNTAX_ERR);
      }
    } else {
      throw new DOMException(`Invalid selector ${selector}`, SYNTAX_ERR);
    }
  }
};

/**
 * Walks the provided AST to collect selector branches and gather information
 * about its contents.
 * @param {object} ast - The AST to traverse.
 * @param {boolean} toObject - True if converts ast to object, false otherwise.
 * @returns {{branches: Array<object>, info: object}} An object containing the selector branches and info.
 */
export const walkAST = (ast = {}, toObject = false) => {
  const branches = new Set();
  const info = {
    hasForgivenPseudoFunc: false,
    hasHasPseudoFunc: false,
    hasLogicalPseudoFunc: false,
    hasNotPseudoFunc: false,
    hasNthChildOfSelector: false,
    hasNestedSelector: false,
    hasStatePseudoClass: false
  };
  const opt = {
    enter(node) {
      switch (node.type) {
        case CLASS_SELECTOR: {
          if (/^-?\d/.test(node.name)) {
            throw new DOMException(
              `Invalid selector .${node.name}`,
              SYNTAX_ERR
            );
          }
          break;
        }
        case ID_SELECTOR: {
          if (/^-?\d/.test(node.name)) {
            throw new DOMException(
              `Invalid selector #${node.name}`,
              SYNTAX_ERR
            );
          }
          break;
        }
        case PS_CLASS_SELECTOR: {
          if (KEYS_LOGICAL.has(node.name)) {
            info.hasNestedSelector = true;
            info.hasLogicalPseudoFunc = true;
            if (node.name === 'has') {
              info.hasHasPseudoFunc = true;
            } else if (node.name === 'not') {
              info.hasNotPseudoFunc = true;
            } else {
              info.hasForgivenPseudoFunc = true;
            }
          } else if (KEYS_PS_CLASS_STATE.has(node.name)) {
            info.hasStatePseudoClass = true;
          } else if (
            KEYS_SHADOW_HOST.has(node.name) &&
            Array.isArray(node.children) &&
            node.children.length
          ) {
            info.hasNestedSelector = true;
          }
          break;
        }
        case PS_ELEMENT_SELECTOR: {
          if (REG_SHADOW_PS_ELEMENT.test(node.name)) {
            info.hasNestedSelector = true;
          }
          break;
        }
        case NTH: {
          if (node.selector) {
            info.hasNestedSelector = true;
            info.hasNthChildOfSelector = true;
          }
          break;
        }
        case SELECTOR: {
          branches.add(node.children);
          break;
        }
        default:
      }
    }
  };
  const clonedAst = cssTree.clone(ast);
  cssTree.walk(toObject ? cssTree.toPlainObject(clonedAst) : clonedAst, opt);
  if (info.hasNestedSelector === true) {
    cssTree.findAll(clonedAst, (node, item, list) => {
      if (list) {
        if (node.type === PS_CLASS_SELECTOR && KEYS_LOGICAL.has(node.name)) {
          const itemList = list.filter(i => {
            const { name, type } = i;
            return type === PS_CLASS_SELECTOR && KEYS_LOGICAL.has(name);
          });
          for (const { children } of itemList) {
            // SelectorList
            for (const { children: grandChildren } of children) {
              // Selector
              for (const { children: greatGrandChildren } of grandChildren) {
                if (branches.has(greatGrandChildren)) {
                  branches.delete(greatGrandChildren);
                }
              }
            }
          }
        } else if (
          node.type === PS_CLASS_SELECTOR &&
          KEYS_SHADOW_HOST.has(node.name) &&
          Array.isArray(node.children) &&
          node.children.length
        ) {
          const itemList = list.filter(i => {
            const { children, name, type } = i;
            const res =
              type === PS_CLASS_SELECTOR &&
              KEYS_SHADOW_HOST.has(name) &&
              Array.isArray(children) &&
              children.length;
            return res;
          });
          for (const { children } of itemList) {
            // Selector
            for (const { children: grandChildren } of children) {
              if (branches.has(grandChildren)) {
                branches.delete(grandChildren);
              }
            }
          }
        } else if (
          node.type === PS_ELEMENT_SELECTOR &&
          REG_SHADOW_PS_ELEMENT.test(node.name)
        ) {
          const itemList = list.filter(i => {
            const { name, type } = i;
            const res =
              type === PS_ELEMENT_SELECTOR && REG_SHADOW_PS_ELEMENT.test(name);
            return res;
          });
          for (const { children } of itemList) {
            // Selector
            for (const { children: grandChildren } of children) {
              if (branches.has(grandChildren)) {
                branches.delete(grandChildren);
              }
            }
          }
        } else if (node.type === NTH && node.selector) {
          const itemList = list.filter(i => {
            const { selector, type } = i;
            const res = type === NTH && selector;
            return res;
          });
          for (const { selector } of itemList) {
            const { children } = selector;
            // Selector
            for (const { children: grandChildren } of children) {
              if (branches.has(grandChildren)) {
                branches.delete(grandChildren);
              }
            }
          }
        }
      }
    });
  }
  return {
    info,
    branches: [...branches]
  };
};

/**
 * Comparison function for sorting AST nodes based on specificity.
 * @param {object} a - The first AST node.
 * @param {object} b - The second AST node.
 * @returns {number} -1, 0 or 1, depending on the sort order.
 */
export const compareASTNodes = (a, b) => {
  const bitA = AST_SORT_ORDER.get(a.type);
  const bitB = AST_SORT_ORDER.get(b.type);
  if (bitA === bitB) {
    return 0;
  } else if (bitA > bitB) {
    return 1;
  } else {
    return -1;
  }
};

/**
 * Sorts a collection of AST nodes based on CSS specificity rules.
 * @param {Array<object>} asts - A collection of AST nodes to sort.
 * @returns {Array<object>} A new array containing the sorted AST nodes.
 */
export const sortAST = asts => {
  const arr = [...asts];
  if (arr.length > 1) {
    arr.sort(compareASTNodes);
  }
  return arr;
};

/**
 * Parses a type selector's name, which may include a namespace prefix.
 * @param {string} selector - The type selector name (e.g., 'ns|E' or 'E').
 * @returns {{prefix: string, localName: string}} An object with `prefix` and
 * `localName` properties.
 */
export const parseAstName = selector => {
  let prefix;
  let localName;
  if (selector && typeof selector === 'string') {
    if (selector.indexOf('|') > -1) {
      [prefix, localName] = selector.split('|');
    } else {
      prefix = '*';
      localName = selector;
    }
  } else {
    throw new DOMException(`Invalid selector ${selector}`, SYNTAX_ERR);
  }
  return {
    prefix,
    localName
  };
};

/* Re-exported from css-tree. */
export { find as findAST, generate as generateCSS } from 'css-tree';
