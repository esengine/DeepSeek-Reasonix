"use strict";

const CSSRuleImpl = require("./CSSRule-impl.js").implementation;
const CSSStyleProperties = require("../../../generated/idl/CSSStyleProperties.js");

class CSSNestedDeclarationsImpl extends CSSRuleImpl {
  constructor(globalObject, args, privateData) {
    super(globalObject, args, privateData);
    this.style = CSSStyleProperties.createImpl(globalObject, [], {
      parentRule: this
    });
  }

  get cssText() {
    return this.style.cssText;
  }
}

exports.implementation = CSSNestedDeclarationsImpl;
