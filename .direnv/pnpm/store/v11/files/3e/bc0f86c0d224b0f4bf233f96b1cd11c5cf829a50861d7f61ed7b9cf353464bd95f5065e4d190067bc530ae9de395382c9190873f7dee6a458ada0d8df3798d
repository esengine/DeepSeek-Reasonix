"use strict";

const CSSConditionRuleImpl = require("./CSSConditionRule-impl.js").implementation;
const { serializeGroupingRuleBody } = require("./CSSGroupingRule-impl.js");
const { serializeCustomIdent } = require("./helpers/css-values.js");

// https://drafts.csswg.org/css-conditional-5/#typedef-container-name
const containerNameExclusions = new Set(["none", "and", "or", "not"]);

class CSSContainerRuleImpl extends CSSConditionRuleImpl {
  constructor(globalObject, args, privateData) {
    super(globalObject, args, privateData);
    this.containerName = privateData.containerName;
    this.containerQuery = privateData.containerQuery;
  }

  get conditionText() {
    if (this.containerName) {
      return serializeCustomIdent(this.containerName, containerNameExclusions) + " " + this.containerQuery;
    }
    return this.containerQuery;
  }

  get type() {
    return 0; // No standard type constant for container rules
  }

  get cssText() {
    return `@container ${this.conditionText} ${serializeGroupingRuleBody(this.cssRules._list)}`;
  }
}

exports.implementation = CSSContainerRuleImpl;
