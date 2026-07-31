"use strict";

const idlUtils = require("../../../generated/idl/utils.js");
const DOMException = require("../../../generated/idl/DOMException.js");
const CSSImportRule = require("../../../generated/idl/CSSImportRule.js");
const CSSNamespaceRule = require("../../../generated/idl/CSSNamespaceRule.js");
const CSSStyleRule = require("../../../generated/idl/CSSStyleRule.js");
const CSSFontFaceRule = require("../../../generated/idl/CSSFontFaceRule.js");
const CSSLayerStatementRule = require("../../../generated/idl/CSSLayerStatementRule.js");
const cssParser = require("./helpers/css-parser.js");

class CSSRuleListImpl {
  constructor(globalObject) {
    this._globalObject = globalObject;
    this._list = [];
  }

  get length() {
    return this._list.length;
  }

  item(index) {
    return this._list[index] || null;
  }

  get [idlUtils.supportedPropertyIndices]() {
    return this._list.keys();
  }

  // https://drafts.csswg.org/cssom/#insert-a-css-rule
  // `_insert()` parses the rule text and then validates and inserts. `CSSStyleSheet`'s
  // `insertRule()` needs to inspect the parsed rule before inserting, so it parses itself and then
  // calls `_insertParsed()` directly to avoid double-parsing.
  _insert(rule, index, nested, parentRule, parentStyleSheet) {
    this.#checkInsertIndex(index);

    let newRule;
    try {
      newRule = cssParser.parseRule(rule, this._globalObject);
    } catch (syntaxError) {
      if (nested) {
        newRule = cssParser.parseDeclarationsAsNestedRule(rule, this._globalObject);
        if (!newRule) {
          throw syntaxError;
        }
      } else {
        throw syntaxError;
      }
    }

    return this.#validateAndInsert(newRule, index, parentRule, parentStyleSheet);
  }

  _insertParsed(newRule, index, parentRule, parentStyleSheet) {
    this.#checkInsertIndex(index);
    return this.#validateAndInsert(newRule, index, parentRule, parentStyleSheet);
  }

  #checkInsertIndex(index) {
    if (index > this._list.length) {
      throw DOMException.create(this._globalObject, [
        "Index is out of range.",
        "IndexSizeError"
      ]);
    }
  }

  #validateAndInsert(newRule, index, parentRule, parentStyleSheet) {
    if (parentRule) {
      // Inside a grouping rule: @import and @namespace are never allowed
      if (CSSImportRule.isImpl(newRule)) {
        throw DOMException.create(this._globalObject, [
          "@import rules are not allowed inside grouping rules.",
          "HierarchyRequestError"
        ]);
      }
      if (CSSNamespaceRule.isImpl(newRule)) {
        throw DOMException.create(this._globalObject, [
          "@namespace rules are not allowed inside grouping rules.",
          "HierarchyRequestError"
        ]);
      }

      // @font-face is not valid inside a style rule ancestor
      if (CSSFontFaceRule.isImpl(newRule)) {
        let ancestor = parentRule;
        while (ancestor) {
          if (CSSStyleRule.isImpl(ancestor)) {
            throw DOMException.create(this._globalObject, [
              "@font-face rules are not allowed inside style rules.",
              "HierarchyRequestError"
            ]);
          }
          ancestor = ancestor.parentRule;
        }
      }
    } else {
      // Sheet-level constraints

      // @import rules can only be preceded by other @import and @layer statement rules
      if (CSSImportRule.isImpl(newRule)) {
        for (let i = 0; i < index; i++) {
          const r = this._list[i];
          if (!CSSImportRule.isImpl(r) && !CSSLayerStatementRule.isImpl(r)) {
            throw DOMException.create(this._globalObject, [
              "@import rules must precede all other rules.",
              "HierarchyRequestError"
            ]);
          }
        }
      }

      // @namespace rules cannot be inserted if any non-import/namespace/layer-statement rules exist
      if (CSSNamespaceRule.isImpl(newRule)) {
        const hasOtherRules = this._list.some(
          r => !CSSImportRule.isImpl(r) && !CSSNamespaceRule.isImpl(r) && !CSSLayerStatementRule.isImpl(r)
        );
        if (hasOtherRules) {
          throw DOMException.create(this._globalObject, [
            "Cannot insert @namespace rule when other rules exist.",
            "InvalidStateError"
          ]);
        }
      }

      // Non-import/namespace/layer-statement rules cannot be inserted before @import or @namespace rules
      if (!CSSImportRule.isImpl(newRule) && !CSSNamespaceRule.isImpl(newRule) &&
          !CSSLayerStatementRule.isImpl(newRule)) {
        for (let i = index; i < this._list.length; i++) {
          const r = this._list[i];
          if (CSSImportRule.isImpl(r) || CSSNamespaceRule.isImpl(r)) {
            throw DOMException.create(this._globalObject, [
              "Cannot insert a rule before @import or @namespace rules.",
              "HierarchyRequestError"
            ]);
          }
        }
      }

      // Validate namespace prefixes in style rule selectors
      if (CSSStyleRule.isImpl(newRule)) {
        const declaredPrefixes = new Set();
        for (const r of this._list) {
          if (CSSNamespaceRule.isImpl(r)) {
            declaredPrefixes.add(r.prefix);
          }
        }
        const nsPrefixPattern = /(?:^|[\s,>+~(])([a-zA-Z_][\w-]*)\|/g;
        let m;
        while ((m = nsPrefixPattern.exec(newRule.selectorText)) !== null) {
          if (!declaredPrefixes.has(m[1])) {
            throw DOMException.create(this._globalObject, [
              "Undeclared namespace prefix in selector.",
              "SyntaxError"
            ]);
          }
        }
      }
    }

    newRule.parentRule = parentRule;
    newRule.parentStyleSheet = parentStyleSheet;
    if (CSSImportRule.isImpl(newRule) && newRule.styleSheet) {
      newRule.styleSheet.parentStyleSheet = parentStyleSheet;
    }
    this._list.splice(index, 0, newRule);

    return index;
  }

  // https://drafts.csswg.org/cssom/#remove-a-css-rule
  _remove(index) {
    if (index >= this._list.length) {
      throw DOMException.create(this._globalObject, [
        "Index is out of range.",
        "IndexSizeError"
      ]);
    }

    const oldRule = this._list[index];

    if (CSSNamespaceRule.isImpl(oldRule)) {
      const hasOtherRules = this._list.some(
        r => !CSSImportRule.isImpl(r) && !CSSNamespaceRule.isImpl(r) && !CSSLayerStatementRule.isImpl(r)
      );
      if (hasOtherRules) {
        throw DOMException.create(this._globalObject, [
          "Cannot delete @namespace rule when other rules exist.",
          "InvalidStateError"
        ]);
      }
    }

    // Not yet in spec; see https://github.com/w3c/csswg-drafts/issues/13612
    if (CSSImportRule.isImpl(oldRule) && oldRule.styleSheet) {
      oldRule.styleSheet.parentStyleSheet = null;
    }

    oldRule.parentRule = null;
    oldRule.parentStyleSheet = null;
    this._list.splice(index, 1);
  }

  _clear() {
    for (const rule of this._list) {
      if (CSSImportRule.isImpl(rule) && rule.styleSheet) {
        rule.styleSheet.parentStyleSheet = null;
      }
      if (rule.cssRules) {
        rule.cssRules._clear();
      }
      rule.parentRule = null;
      rule.parentStyleSheet = null;
    }
    this._list.length = 0;
  }
}

exports.implementation = CSSRuleListImpl;
