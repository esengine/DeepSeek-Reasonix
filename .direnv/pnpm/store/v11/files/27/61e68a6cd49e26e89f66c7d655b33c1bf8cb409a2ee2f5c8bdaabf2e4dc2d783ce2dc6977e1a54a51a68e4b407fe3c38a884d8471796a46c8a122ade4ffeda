"use strict";

class CSSRuleImpl {
  constructor(globalObject, args, privateData) {
    this._globalObject = globalObject;
    this.parentRule = privateData.parentRule;
    this.parentStyleSheet = privateData.parentStyleSheet;
  }

  // Subclasses must override type and cssText
  get type() {
    return 0;
  }

  get cssText() {
    return "";
  }

  set cssText(_value) {
    // Per spec, setting cssText does nothing
  }
}

exports.implementation = CSSRuleImpl;
