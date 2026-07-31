"use strict";

const StyleSheetImpl = require("./StyleSheet-impl.js").implementation;
const CSSRuleList = require("../../../generated/idl/CSSRuleList.js");
const DOMException = require("../../../generated/idl/DOMException.js");
const CSSImportRule = require("../../../generated/idl/CSSImportRule.js");
const cssParser = require("./helpers/css-parser.js");
const { wrapperForImpl } = require("../../../generated/idl/utils.js");

class CSSStyleSheetImpl extends StyleSheetImpl {
  #constructed;
  #disallowModification;

  constructor(globalObject, args, privateData = {}) {
    super(globalObject, args, privateData);

    this.#constructed = "constructed" in privateData ? privateData.constructed : true;
    this.#disallowModification = false;
    this.ownerRule = privateData.ownerRule || null;
    this.cssRules = CSSRuleList.createImpl(globalObject);
  }

  // Legacy alias for cssRules
  get rules() {
    return this.cssRules;
  }

  insertRule(rule, index) {
    // TODO: check origin-clean flag
    this.#checkModificationAllowed();

    const parsed = cssParser.parseRule(rule, this._globalObject);
    if (CSSImportRule.isImpl(parsed) && this.#constructed) {
      throw DOMException.create(this._globalObject, [
        "Cannot insert @import rules into a constructed stylesheet.",
        "SyntaxError"
      ]);
    }

    const result = this.cssRules._insertParsed(parsed, index, null, this);
    this.ownerNode?._ownerDocument._clearStyleCache();
    return result;
  }

  deleteRule(index) {
    // TODO: check origin-clean flag
    this.#checkModificationAllowed();

    this.cssRules._remove(index);
    this.ownerNode?._ownerDocument._clearStyleCache();
  }

  // Legacy method
  addRule(selector, style, index) {
    const rule = `${selector} { ${style} }`;
    if (index === undefined) {
      index = this.cssRules._list.length;
    }
    this.insertRule(rule, index);
    return -1;
  }

  // Legacy method
  removeRule(index) {
    this.deleteRule(index);
  }

  replaceSync(text) {
    if (!this.#constructed) {
      throw DOMException.create(this._globalObject, [
        "Cannot call replaceSync on non-constructed CSSStyleSheet.",
        "NotAllowedError"
      ]);
    }
    if (this.#disallowModification) {
      throw DOMException.create(this._globalObject, [
        "The stylesheet is currently being modified.",
        "NotAllowedError"
      ]);
    }

    // Clear existing rules
    this.cssRules._clear();

    // Parse and populate
    cssParser.parseIntoStyleSheet(text, this._globalObject, this);

    this.ownerNode?._ownerDocument._clearStyleCache();
  }

  replace(text) {
    if (!this.#constructed) {
      return Promise.reject(DOMException.create(this._globalObject, [
        "Cannot call replace on non-constructed CSSStyleSheet.",
        "NotAllowedError"
      ]));
    }
    if (this.#disallowModification) {
      return Promise.reject(DOMException.create(this._globalObject, [
        "The stylesheet is currently being modified.",
        "NotAllowedError"
      ]));
    }

    this.#disallowModification = true;

    // The spec says "in parallel" which allows off-main-thread parsing. We parse synchronously but
    // defer via a microtask so the disallow modification flag is observable between the call and the
    // promise settling.
    return new Promise(resolve => {
      queueMicrotask(() => {
        this.cssRules._clear();
        cssParser.parseIntoStyleSheet(text, this._globalObject, this);
        this.#disallowModification = false;
        this.ownerNode?._ownerDocument._clearStyleCache();

        // webidl2js doesn't auto-wrap return values: https://github.com/jsdom/webidl2js/issues/257
        resolve(wrapperForImpl(this));
      });
    });
  }

  #checkModificationAllowed() {
    if (this.#disallowModification) {
      throw DOMException.create(this._globalObject, [
        "The stylesheet is currently being modified.",
        "NotAllowedError"
      ]);
    }
  }
}

exports.implementation = CSSStyleSheetImpl;
