"use strict";

const CSSGroupingRuleImpl = require("./CSSGroupingRule-impl.js").implementation;
const CSSStyleProperties = require("../../../generated/idl/CSSStyleProperties.js");
const csstree = require("./helpers/patched-csstree.js");

class CSSStyleRuleImpl extends CSSGroupingRuleImpl {
  #selectorText;

  constructor(globalObject, args, privateData) {
    super(globalObject, args, privateData);
    this.#selectorText = privateData.selectorText;
    this.style = CSSStyleProperties.createImpl(globalObject, [], {
      parentRule: this
    });
  }

  get selectorText() {
    return this.#selectorText;
  }

  set selectorText(value) {
    try {
      const ast = csstree.parse(value, { context: "selectorList" });
      if (ast.children.isEmpty) {
        return;
      }
    } catch {
      return;
    }
    this.#selectorText = value;
  }

  get type() {
    return 1; // STYLE_RULE
  }

  get cssText() {
    const styleText = this.style.cssText;
    const rules = this.cssRules._list;

    if (rules.length === 0 && !styleText) {
      return `${this.selectorText} { }`;
    }

    if (rules.length === 0) {
      return `${this.selectorText} { ${styleText} }`;
    }

    // Has nested rules: use multi-line format
    let body = "";
    if (styleText) {
      body += `\n  ${styleText}`;
    }
    for (const rule of rules) {
      const ruleText = rule.cssText;
      // Skip empty CSSNestedDeclarations per CSSOM serialization spec
      if (ruleText) {
        body += `\n  ${ruleText}`;
      }
    }
    return `${this.selectorText} {${body}\n}`;
  }
}

exports.implementation = CSSStyleRuleImpl;
