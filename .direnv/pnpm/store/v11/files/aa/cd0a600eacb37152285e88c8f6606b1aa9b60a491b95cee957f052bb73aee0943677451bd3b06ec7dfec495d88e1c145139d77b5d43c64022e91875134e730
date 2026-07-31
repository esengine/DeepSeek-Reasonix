"use strict";

const CSSRuleImpl = require("./CSSRule-impl.js").implementation;

class CSSLayerStatementRuleImpl extends CSSRuleImpl {
  constructor(globalObject, args, privateData) {
    super(globalObject, args, privateData);
    this.nameList = Object.freeze(privateData.nameList);
  }

  get cssText() {
    return `@layer ${this.nameList.join(", ")};`;
  }
}

exports.implementation = CSSLayerStatementRuleImpl;
