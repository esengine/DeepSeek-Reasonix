"use strict";

const CSSGroupingRuleImpl = require("./CSSGroupingRule-impl.js").implementation;
const { serializeGroupingRuleBody } = require("./CSSGroupingRule-impl.js");

class CSSScopeRuleImpl extends CSSGroupingRuleImpl {
  constructor(globalObject, args, privateData) {
    super(globalObject, args, privateData);
    this.start = privateData.start;
    this.end = privateData.end;
  }

  get cssText() {
    let prelude = "";
    if (this.start) {
      prelude += ` (${this.start})`;
    }
    if (this.end) {
      prelude += ` to (${this.end})`;
    }
    return `@scope${prelude} ${serializeGroupingRuleBody(this.cssRules._list)}`;
  }
}

exports.implementation = CSSScopeRuleImpl;
