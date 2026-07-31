"use strict";

const CSSGroupingRuleImpl = require("./CSSGroupingRule-impl.js").implementation;

class CSSConditionRuleImpl extends CSSGroupingRuleImpl {
  constructor(globalObject, args, privateData) {
    super(globalObject, args, privateData);
    this._conditionText = privateData.conditionText;
  }

  // Getter required: CSSMediaRule overrides this, so an own data property would shadow it.
  get conditionText() {
    return this._conditionText;
  }
}

exports.implementation = CSSConditionRuleImpl;
