"use strict";

const CSSRuleImpl = require("./CSSRule-impl.js").implementation;
const { serializeCustomIdent } = require("./helpers/css-values.js");

// https://drafts.csswg.org/css-counter-styles-3/#typedef-counter-style-name
const counterStyleExclusions = new Set(
  ["none", "decimal", "disc", "square", "circle", "disclosure-open", "disclosure-closed"]
);

// TODO: implement these as proper getters and setters that follow the spec at
// https://drafts.csswg.org/css-counter-styles-3/#csscounterstylerule, including serialization,
// parsing, special handling for certain values, etc.
const descriptorNames = [
  "system", "symbols", "additiveSymbols", "negative",
  "prefix", "suffix", "range", "pad", "speakAs", "fallback"
];

class CSSCounterStyleRuleImpl extends CSSRuleImpl {
  constructor(globalObject, args, privateData) {
    super(globalObject, args, privateData);
    this.name = privateData.name;
    for (const desc of descriptorNames) {
      this[desc] = "";
    }
  }

  get type() {
    return 11; // COUNTER_STYLE_RULE
  }

  get cssText() {
    const parts = [];
    for (const desc of descriptorNames) {
      if (this[desc]) {
        // Convert camelCase to kebab-case for CSS property names
        const cssName = desc.replace(/[A-Z]/g, m => `-${m.toLowerCase()}`);
        parts.push(`${cssName}: ${this[desc]};`);
      }
    }
    return `@counter-style ${serializeCustomIdent(this.name, counterStyleExclusions)} { ${parts.join(" ")} }`;
  }
}

exports.implementation = CSSCounterStyleRuleImpl;
