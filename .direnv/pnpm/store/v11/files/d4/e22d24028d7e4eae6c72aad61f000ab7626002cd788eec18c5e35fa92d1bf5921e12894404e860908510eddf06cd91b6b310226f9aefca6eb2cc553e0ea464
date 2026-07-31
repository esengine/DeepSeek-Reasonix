"use strict";

const CSSRuleImpl = require("./CSSRule-impl.js").implementation;
const CSSRuleList = require("../../../generated/idl/CSSRuleList.js");
const CSSStyleRule = require("../../../generated/idl/CSSStyleRule.js");

class CSSGroupingRuleImpl extends CSSRuleImpl {
  constructor(globalObject, args, privateData) {
    super(globalObject, args, privateData);

    this.cssRules = CSSRuleList.createImpl(globalObject);
  }

  insertRule(rule, index) {
    const result = this.cssRules._insert(rule, index, this._isInNestingContext(), this, this.parentStyleSheet);
    this.parentStyleSheet?.ownerNode?._ownerDocument._clearStyleCache();
    return result;
  }

  deleteRule(index) {
    this.cssRules._remove(index);
    this.parentStyleSheet?.ownerNode?._ownerDocument._clearStyleCache();
  }

  _isInNestingContext() {
    let ancestor = this;
    while (ancestor) {
      if (CSSStyleRule.isImpl(ancestor)) {
        return true;
      }
      ancestor = ancestor.parentRule;
    }
    return false;
  }
}

/**
 * Serialize the body of a grouping rule per CSSOM spec.
 * Produces multi-line output with indented child rules.
 */
function serializeGroupingRuleBody(rules) {
  let body = "{\n";
  for (const rule of rules) {
    const ruleText = rule.cssText;
    // Skip empty CSSNestedDeclarations per CSSOM serialization spec
    if (ruleText) {
      body += `  ${ruleText}\n`;
    }
  }
  body += "}";
  return body;
}

exports.implementation = CSSGroupingRuleImpl;
exports.serializeGroupingRuleBody = serializeGroupingRuleBody;
