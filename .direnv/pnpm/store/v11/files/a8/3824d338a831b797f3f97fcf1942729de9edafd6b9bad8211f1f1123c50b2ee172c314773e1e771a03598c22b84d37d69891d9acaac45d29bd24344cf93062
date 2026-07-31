"use strict";

const CSSGroupingRuleImpl = require("./CSSGroupingRule-impl.js").implementation;
const { serializeGroupingRuleBody } = require("./CSSGroupingRule-impl.js");

class CSSLayerBlockRuleImpl extends CSSGroupingRuleImpl {
  constructor(globalObject, args, privateData) {
    super(globalObject, args, privateData);
    this.name = privateData.name;
  }

  get cssText() {
    const name = this.name ? ` ${this.name}` : "";
    return `@layer${name} ${serializeGroupingRuleBody(this.cssRules._list)}`;
  }
}

exports.implementation = CSSLayerBlockRuleImpl;
