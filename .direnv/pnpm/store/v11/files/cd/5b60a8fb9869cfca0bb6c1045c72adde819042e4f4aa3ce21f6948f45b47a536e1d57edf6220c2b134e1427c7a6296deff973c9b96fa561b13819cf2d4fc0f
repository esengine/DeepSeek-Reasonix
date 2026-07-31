"use strict";

const CSSConditionRuleImpl = require("./CSSConditionRule-impl.js").implementation;
const { serializeGroupingRuleBody } = require("./CSSGroupingRule-impl.js");
const MediaList = require("../../../generated/idl/MediaList.js");
const { evaluateMediaList } = require("./MediaList-impl.js");

class CSSMediaRuleImpl extends CSSConditionRuleImpl {
  constructor(globalObject, args, privateData) {
    super(globalObject, args, privateData);

    this.media = MediaList.createImpl(globalObject, [], {
      mediaText: this._conditionText
    });
  }

  get type() {
    return 4; // MEDIA_RULE
  }

  get matches() {
    return evaluateMediaList(this.media._list);
  }

  get conditionText() {
    return this.media.mediaText;
  }

  get cssText() {
    return `@media ${this.media.mediaText} ${serializeGroupingRuleBody(this.cssRules._list)}`;
  }
}

exports.implementation = CSSMediaRuleImpl;
