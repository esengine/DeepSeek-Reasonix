/**
 * matcher.js
 */

/* import */
import { generateCSS, parseAstName, unescapeSelector } from './parser.js';
import {
  generateException,
  getDirectionality,
  getLanguageAttribute,
  getType,
  isContentEditable,
  isCustomElement,
  isNamespaceDeclared
} from './utility.js';

/* constants */
import {
  ALPHA_NUM,
  FORM_PARTS,
  IDENT,
  INPUT_EDIT,
  LANG_PART,
  NOT_SUPPORTED_ERR,
  PS_ELEMENT_SELECTOR,
  STRING,
  SYNTAX_ERR
} from './constant.js';
const KEYS_FORM_PS_DISABLED = new Set([
  ...FORM_PARTS,
  'fieldset',
  'optgroup',
  'option'
]);
const KEYS_INPUT_EDIT = new Set(INPUT_EDIT);
const REG_LANG_VALID = new RegExp(`^(?:\\*-)?${ALPHA_NUM}${LANG_PART}$`, 'i');

/**
 * Validates a pseudo-element selector.
 * @param {string} astName - The name of the pseudo-element from the AST.
 * @param {string} astType - The type of the selector from the AST.
 * @param {object} [opt] - Optional parameters.
 * @param {boolean} [opt.forgive] - If true, ignores unknown pseudo-elements.
 * @param {boolean} [opt.warn] - If true, throws an error for unsupported ones.
 * @throws {DOMException} If the selector is invalid or unsupported.
 * @returns {void}
 */
export const matchPseudoElementSelector = (astName, astType, opt = {}) => {
  const { forgive, globalObject, warn } = opt;
  if (astType !== PS_ELEMENT_SELECTOR) {
    // Ensure the AST node is a pseudo-element selector.
    throw new TypeError(`Unexpected ast type ${getType(astType)}`);
  }
  switch (astName) {
    case 'after':
    case 'backdrop':
    case 'before':
    case 'cue':
    case 'cue-region':
    case 'first-letter':
    case 'first-line':
    case 'file-selector-button':
    case 'marker':
    case 'placeholder':
    case 'selection':
    case 'target-text': {
      // Warn if the pseudo-element is known but unsupported.
      if (warn) {
        throw generateException(
          `Unsupported pseudo-element ::${astName}`,
          NOT_SUPPORTED_ERR,
          globalObject
        );
      }
      break;
    }
    case 'part':
    case 'slotted': {
      // Warn if the functional pseudo-element is known but unsupported.
      if (warn) {
        throw generateException(
          `Unsupported pseudo-element ::${astName}()`,
          NOT_SUPPORTED_ERR,
          globalObject
        );
      }
      break;
    }
    default: {
      // Handle vendor-prefixed or unknown pseudo-elements.
      if (astName.startsWith('-webkit-')) {
        if (warn) {
          throw generateException(
            `Unsupported pseudo-element ::${astName}`,
            NOT_SUPPORTED_ERR,
            globalObject
          );
        }
        // Throw an error for unknown pseudo-elements if not forgiven.
      } else if (!forgive) {
        throw generateException(
          `Unknown pseudo-element ::${astName}`,
          SYNTAX_ERR,
          globalObject
        );
      }
    }
  }
};

/**
 * Matches the :dir() pseudo-class against an element's directionality.
 * @param {object} ast - The AST object for the pseudo-class.
 * @param {object} node - The element node to match against.
 * @throws {TypeError} If the AST does not contain a valid direction value.
 * @returns {boolean} - True if the directionality matches, otherwise false.
 */
export const matchDirectionPseudoClass = (ast, node) => {
  const { name } = ast;
  // The :dir() pseudo-class requires a direction argument (e.g., "ltr").
  if (!name) {
    const type = name === '' ? '(empty String)' : getType(name);
    throw new TypeError(`Unexpected ast type ${type}`);
  }
  // Get the computed directionality of the element.
  const dir = getDirectionality(node);
  // Compare the expected direction with the element's actual direction.
  return name === dir;
};

/**
 * Matches the :lang() pseudo-class against an element's language.
 * @see https://datatracker.ietf.org/doc/html/rfc4647#section-3.3.1
 * @param {object} ast - The AST object for the pseudo-class.
 * @param {object} node - The element node to match against.
 * @returns {boolean} - True if the language matches, otherwise false.
 */
export const matchLanguagePseudoClass = (ast, node) => {
  // Get the effective language attribute for the current node.
  const elementLang = getLanguageAttribute(node);
  // If the element has no language, it cannot match a specific pattern.
  if (elementLang === null) {
    return false;
  }
  // Use cached regex.
  if (ast._langRegex !== undefined) {
    if (ast._langPattern === '*') {
      return elementLang !== '';
    }
    if (ast._langRegex === null) {
      return false;
    }
    return ast._langRegex.test(elementLang);
  }
  const { name, type, value } = ast;
  let langPattern;
  // Determine the language pattern from the AST.
  if (type === STRING && value) {
    langPattern = value;
  } else if (type === IDENT && name) {
    langPattern = unescapeSelector(name);
  }
  // Cache lang pattern.
  ast._langPattern = langPattern;
  // If no valid language pattern is provided, it cannot match.
  if (typeof langPattern !== 'string') {
    ast._langRegex = null;
    return false;
  }
  // Handle the universal selector '*' for :lang.
  if (langPattern === '*') {
    ast._langRegex = null;
    return elementLang !== '';
  }
  // Validate the provided language pattern structure.
  if (!REG_LANG_VALID.test(langPattern)) {
    ast._langRegex = null;
    return false;
  }
  // Build a regex for extended language range matching.
  let matcherRegex;
  if (langPattern.indexOf('-') > -1) {
    const [langMain, langSub, ...langRest] = langPattern.split('-');
    const extendedMain =
      langMain === '*' ? `${ALPHA_NUM}${LANG_PART}` : `${langMain}${LANG_PART}`;
    const extendedSub = `-${langSub}${LANG_PART}`;
    let extendedRest = '';
    for (let i = 0; i < langRest.length; i++) {
      extendedRest += `-${langRest[i]}${LANG_PART}`;
    }
    matcherRegex = new RegExp(
      `^${extendedMain}${extendedSub}${extendedRest}$`,
      'i'
    );
  } else {
    matcherRegex = new RegExp(`^${langPattern}${LANG_PART}$`, 'i');
  }
  ast._langRegex = matcherRegex;
  // Test the element's language against the constructed regex.
  return matcherRegex.test(elementLang);
};

/**
 * Matches the :disabled and :enabled pseudo-classes.
 * @param {string} astName - pseudo-class name
 * @param {object} node - Element node
 * @returns {boolean} - True if matched
 */
export const matchDisabledPseudoClass = (astName, node) => {
  const { localName, parentNode } = node;
  if (
    !KEYS_FORM_PS_DISABLED.has(localName) &&
    !isCustomElement(node, { formAssociated: true })
  ) {
    return false;
  }
  let isDisabled = false;
  if (node.disabled || node.hasAttribute('disabled')) {
    isDisabled = true;
  } else if (localName === 'option') {
    if (
      parentNode &&
      parentNode.localName === 'optgroup' &&
      (parentNode.disabled || parentNode.hasAttribute('disabled'))
    ) {
      isDisabled = true;
    }
  } else if (localName !== 'optgroup') {
    let current = parentNode;
    while (current) {
      if (
        current.localName === 'fieldset' &&
        (current.disabled || current.hasAttribute('disabled'))
      ) {
        // The first <legend> in a disabled <fieldset> is not disabled.
        let legend;
        let element = current.firstElementChild;
        while (element) {
          if (element.localName === 'legend') {
            legend = element;
            break;
          }
          element = element.nextElementSibling;
        }
        if (!legend || !legend.contains(node)) {
          isDisabled = true;
        }
        // Found the containing fieldset, stop searching up.
        break;
      }
      current = current.parentNode;
    }
  }
  if (astName === 'disabled') {
    return isDisabled;
  }
  return !isDisabled;
};

/**
 * Match the :read-only and :read-write pseudo-classes
 * @param {string} astName - pseudo-class name
 * @param {object} node - Element node
 * @returns {boolean} - True if matched
 */
export const matchReadOnlyPseudoClass = (astName, node) => {
  const { localName } = node;
  let isReadOnly = false;
  switch (localName) {
    case 'textarea':
    case 'input': {
      const isEditableInput = !node.type || KEYS_INPUT_EDIT.has(node.type);
      if (localName === 'textarea' || isEditableInput) {
        isReadOnly =
          node.readOnly ||
          node.hasAttribute('readonly') ||
          node.disabled ||
          node.hasAttribute('disabled');
      } else {
        // Non-editable input types are always read-only
        isReadOnly = true;
      }
      break;
    }
    default: {
      isReadOnly = !isContentEditable(node);
    }
  }
  if (astName === 'read-only') {
    return isReadOnly;
  }
  return !isReadOnly;
};

/**
 * Matches an attribute selector against an element.
 * This function handles various attribute matchers like '=', '~=', '^=', etc.,
 * and considers namespaces and case sensitivity based on document type.
 * @param {object} ast - The AST for the attribute selector.
 * @param {object} node - The element node to match against.
 * @param {object} [opt] - Optional parameters.
 * @param {boolean} [opt.check] - True if running in an internal check.
 * @param {boolean} [opt.forgive] - True to forgive certain syntax errors.
 * @returns {boolean} - True if the attribute selector matches, otherwise false.
 */
export const matchAttributeSelector = (ast, node, opt = {}) => {
  const {
    flags: astFlags,
    matcher: astMatcher,
    name: astName,
    value: astValue
  } = ast;
  const { check, forgive, globalObject } = opt;
  // Validate selector flags ('i' or 's').
  if (typeof astFlags === 'string' && !/^[is]$/i.test(astFlags) && !forgive) {
    const css = generateCSS(ast);
    throw generateException(
      `Invalid selector ${css}`,
      SYNTAX_ERR,
      globalObject
    );
  }
  const { attributes } = node;
  // An element with no attributes cannot match.
  if (!attributes || !attributes.length) {
    return false;
  }
  // Determine case sensitivity based on document type and flags.
  let caseInsensitive;
  if (node.ownerDocument.contentType === 'text/html') {
    if (typeof astFlags === 'string' && /^s$/i.test(astFlags)) {
      caseInsensitive = false;
    } else {
      caseInsensitive = true;
    }
  } else if (typeof astFlags === 'string' && /^i$/i.test(astFlags)) {
    caseInsensitive = true;
  } else {
    caseInsensitive = false;
  }
  // Prepare the attribute name from the selector for matching.
  let astAttrName = unescapeSelector(astName.name);
  if (caseInsensitive) {
    astAttrName = astAttrName.toLowerCase();
  }
  // A set to store the values of attributes whose names match.
  const attrValues = new Set();
  // Handle namespaced attribute names (e.g., [*|attr], [ns|attr]).
  if (astAttrName.indexOf('|') > -1) {
    const { prefix: astPrefix, localName: astLocalName } =
      parseAstName(astAttrName);
    for (const item of attributes) {
      let { name: itemName, value: itemValue } = item;
      if (caseInsensitive) {
        itemName = itemName.toLowerCase();
        itemValue = itemValue.toLowerCase();
      }
      const colonIdx = itemName.indexOf(':');
      switch (astPrefix) {
        case '': {
          if (astLocalName === itemName) {
            attrValues.add(itemValue);
          }
          break;
        }
        case '*': {
          if (colonIdx > -1) {
            const itemLocalName = itemName
              .substring(colonIdx + 1)
              .replace(/^:/, '');
            if (itemLocalName === astLocalName) {
              attrValues.add(itemValue);
            }
          } else if (astLocalName === itemName) {
            attrValues.add(itemValue);
          }
          break;
        }
        default: {
          if (!check) {
            if (forgive) {
              return false;
            }
            const css = generateCSS(ast);
            throw generateException(
              `Invalid selector ${css}`,
              SYNTAX_ERR,
              globalObject
            );
          }
          if (colonIdx > -1) {
            const itemPrefix = itemName.substring(0, colonIdx);
            const itemLocalName = itemName
              .substring(colonIdx + 1)
              .replace(/^:/, '');
            // Ignore the 'xml:lang' attribute.
            if (itemPrefix === 'xml' && itemLocalName === 'lang') {
              continue;
            } else if (
              astPrefix === itemPrefix &&
              astLocalName === itemLocalName
            ) {
              const namespaceDeclared = isNamespaceDeclared(astPrefix, node);
              if (namespaceDeclared) {
                attrValues.add(itemValue);
              }
            }
          }
        }
      }
    }
    // Handle non-namespaced attribute names.
  } else {
    for (let { name: itemName, value: itemValue } of attributes) {
      if (caseInsensitive) {
        itemName = itemName.toLowerCase();
        itemValue = itemValue.toLowerCase();
      }
      const colonIdx = itemName.indexOf(':');
      if (colonIdx > -1) {
        const itemPrefix = itemName.substring(0, colonIdx);
        const itemLocalName = itemName
          .substring(colonIdx + 1)
          .replace(/^:/, '');
        // The attribute is starting with ':'.
        if (!itemPrefix && astAttrName === `:${itemLocalName}`) {
          attrValues.add(itemValue);
          // Ignore the 'xml:lang' attribute.
        } else if (itemPrefix === 'xml' && itemLocalName === 'lang') {
          continue;
        } else if (astAttrName === itemLocalName) {
          attrValues.add(itemValue);
        }
      } else if (astAttrName === itemName) {
        attrValues.add(itemValue);
      }
    }
  }
  if (!attrValues.size) {
    return false;
  }
  // Prepare the value from the selector's RHS for comparison.
  const { name: astIdentValue, value: astStringValue } = astValue ?? {};
  let attrValue;
  if (astIdentValue) {
    // Ident values are not unescaped by css-tree, so we must unescape them
    // (e.g. `\5c` hex escapes, `\n` character escapes).
    if (caseInsensitive) {
      attrValue = unescapeSelector(astIdentValue).toLowerCase();
    } else {
      attrValue = unescapeSelector(astIdentValue);
    }
  } else if (astStringValue) {
    // String values (quoted) are already unescaped by css-tree, so use as-is.
    if (caseInsensitive) {
      attrValue = astStringValue.toLowerCase();
    } else {
      attrValue = astStringValue;
    }
  } else if (astStringValue === '') {
    attrValue = astStringValue;
  }
  // Perform the final match based on the specified matcher.
  switch (astMatcher) {
    case '=': {
      return typeof attrValue === 'string' && attrValues.has(attrValue);
    }
    case '~=': {
      if (attrValue && typeof attrValue === 'string') {
        if (/\s/.test(attrValue)) {
          return false;
        }
        if (ast._tildeTarget === undefined) {
          ast._tildeTarget = ` ${attrValue} `;
        }
        const target = ast._tildeTarget;
        for (const value of attrValues) {
          if (` ${value.replace(/[\t\r\n\f]/g, ' ')} `.includes(target)) {
            return true;
          }
        }
      }
      return false;
    }
    case '|=': {
      if (attrValue && typeof attrValue === 'string') {
        for (const value of attrValues) {
          if (value === attrValue || value.startsWith(`${attrValue}-`)) {
            return true;
          }
        }
      }
      return false;
    }
    case '^=': {
      if (attrValue && typeof attrValue === 'string') {
        for (const value of attrValues) {
          if (value.startsWith(`${attrValue}`)) {
            return true;
          }
        }
      }
      return false;
    }
    case '$=': {
      if (attrValue && typeof attrValue === 'string') {
        for (const value of attrValues) {
          if (value.endsWith(`${attrValue}`)) {
            return true;
          }
        }
      }
      return false;
    }
    case '*=': {
      if (attrValue && typeof attrValue === 'string') {
        for (const value of attrValues) {
          if (value.includes(`${attrValue}`)) {
            return true;
          }
        }
      }
      return false;
    }
    case null:
    default: {
      // This case handles attribute existence checks (e.g., '[disabled]').
      return true;
    }
  }
};

/**
 * match type selector
 * @param {object} ast - AST
 * @param {object} node - Element node
 * @param {object} [opt] - options
 * @param {boolean} [opt.check] - running in internal check()
 * @param {boolean} [opt.forgive] - forgive undeclared namespace
 * @returns {boolean} - result
 */
export const matchTypeSelector = (ast, node, opt = {}) => {
  const astName = unescapeSelector(ast.name);
  const { localName, namespaceURI, prefix } = node;
  const { check, forgive, globalObject } = opt;
  let { prefix: astPrefix, localName: astLocalName } = parseAstName(
    astName,
    node
  );
  const isHTML =
    node.ownerDocument.contentType === 'text/html' &&
    (!namespaceURI || namespaceURI === 'http://www.w3.org/1999/xhtml');
  if (isHTML && localName === astLocalName && !astName.includes('|')) {
    return true;
  }
  const firstChar = localName.charCodeAt(0);
  const isAlphabet =
    (firstChar >= 65 && firstChar <= 90) ||
    (firstChar >= 97 && firstChar <= 122);
  if (isHTML && isAlphabet) {
    astPrefix = astPrefix.toLowerCase();
    astLocalName = astLocalName.toLowerCase();
  }
  let nodePrefix;
  let nodeLocalName;
  const colonIdx = localName.indexOf(':');
  if (colonIdx > -1) {
    nodePrefix = localName.substring(0, colonIdx);
    nodeLocalName = localName.substring(colonIdx + 1);
  } else {
    nodePrefix = prefix || '';
    nodeLocalName = localName;
  }
  const isUniversal = astLocalName === '*';
  switch (astPrefix) {
    case '': {
      return (
        !nodePrefix &&
        !namespaceURI &&
        (isUniversal || astLocalName === nodeLocalName)
      );
    }
    case '*': {
      return isUniversal || astLocalName === nodeLocalName;
    }
    default: {
      if (!check) {
        if (forgive) {
          return false;
        }
        const css = generateCSS(ast);
        throw generateException(
          `Invalid selector ${css}`,
          SYNTAX_ERR,
          globalObject
        );
      }
      const astNS = node.lookupNamespaceURI(astPrefix);
      const nodeNS = node.lookupNamespaceURI(nodePrefix);
      if (astNS === nodeNS && astPrefix === nodePrefix) {
        return isUniversal || astLocalName === nodeLocalName;
      } else if (!forgive && !astNS) {
        throw generateException(
          `Undeclared namespace ${astPrefix}`,
          SYNTAX_ERR,
          globalObject
        );
      }
      return false;
    }
  }
};
