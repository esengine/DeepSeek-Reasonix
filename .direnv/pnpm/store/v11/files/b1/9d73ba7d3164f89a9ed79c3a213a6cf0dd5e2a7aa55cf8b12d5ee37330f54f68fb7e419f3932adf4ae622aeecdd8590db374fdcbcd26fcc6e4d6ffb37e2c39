"use strict";

const CSSRuleImpl = require("./CSSRule-impl.js").implementation;
const CSSRuleList = require("../../../generated/idl/CSSRuleList.js");
const { normalizeKeyText } = require("./CSSKeyframeRule-impl.js");
const cssParser = require("./helpers/css-parser.js");
const idlUtils = require("../../../generated/idl/utils.js");
const { serializeCustomIdent } = require("./helpers/css-values.js");

// https://drafts.csswg.org/css-animations/#typedef-keyframes-name
const keyframesExclusions = new Set(["none"]);

class CSSKeyframesRuleImpl extends CSSRuleImpl {
  constructor(globalObject, args, privateData) {
    super(globalObject, args, privateData);

    this.name = privateData.name;
    this.cssRules = CSSRuleList.createImpl(globalObject);
  }

  get type() {
    return 7; // KEYFRAMES_RULE
  }

  get length() {
    return this.cssRules._list.length;
  }

  get [idlUtils.supportedPropertyIndices]() {
    return this.cssRules._list.keys();
  }

  [idlUtils.supportsPropertyIndex](index) {
    return index >= 0 && index < this.cssRules._list.length;
  }

  [idlUtils.indexedGet](index) {
    return this.cssRules._list[index];
  }

  appendRule(rule) {
    const newRule = cssParser.parseKeyframeRule(rule, this._globalObject);
    if (newRule === null) {
      return;
    }
    newRule.parentRule = this;
    newRule.parentStyleSheet = this.parentStyleSheet;
    this.cssRules._list.push(newRule);
  }

  deleteRule(select) {
    const normalized = normalizeKeyText(select);
    const index = this.cssRules._list.findIndex(
      r => r.keyText === normalized
    );
    if (index !== -1) {
      const removed = this.cssRules._list[index];
      removed.parentRule = null;
      removed.parentStyleSheet = null;
      this.cssRules._list.splice(index, 1);
    }
  }

  findRule(select) {
    const normalized = normalizeKeyText(select);
    // Per spec, return the last matching rule
    for (let i = this.cssRules._list.length - 1; i >= 0; i--) {
      if (this.cssRules._list[i].keyText === normalized) {
        return this.cssRules._list[i];
      }
    }
    return null;
  }

  get cssText() {
    const rules = this.cssRules._list.map(r => r.cssText).join(" ");
    const name = serializeCustomIdent(this.name, keyframesExclusions);
    return `@keyframes ${name} { ${rules} }`;
  }
}

exports.implementation = CSSKeyframesRuleImpl;
