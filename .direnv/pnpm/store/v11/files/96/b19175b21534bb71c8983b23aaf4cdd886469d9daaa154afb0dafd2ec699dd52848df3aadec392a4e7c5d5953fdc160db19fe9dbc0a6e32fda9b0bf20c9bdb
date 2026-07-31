"use strict";

const CSSRuleImpl = require("./CSSRule-impl.js").implementation;
const DOMException = require("../../../generated/idl/DOMException.js");
const CSSStyleProperties = require("../../../generated/idl/CSSStyleProperties.js");
const csstree = require("./helpers/patched-csstree.js");

function validateKeyframeSelector(selector) {
  const ast = csstree.parse(selector, { context: "value" });
  return !csstree.lexer.matchType("keyframe-selector", ast).error;
}

// Normalize a single keyframe selector: "from" → "0%", "To" → "100%", ".5%" → "0.5%", etc.
function normalizeSelector(selector) {
  const lower = selector.toLowerCase();
  if (lower === "from") {
    return "0%";
  }
  if (lower === "to") {
    return "100%";
  }
  // Normalize the number: ".5%" → "0.5%", "100.0%" → "100%", "1e1%" → "10%"
  return String(parseFloat(selector)) + "%";
}

function normalizeKeyText(text) {
  return text.split(",").map(s => normalizeSelector(s.trim())).join(", ");
}

class CSSKeyframeRuleImpl extends CSSRuleImpl {
  #keyText;

  constructor(globalObject, args, privateData) {
    super(globalObject, args, privateData);
    this.#keyText = normalizeKeyText(privateData.keyText);

    this.style = CSSStyleProperties.createImpl(globalObject, [], {
      parentRule: this
    });
  }

  get type() {
    return 8; // KEYFRAME_RULE
  }

  get keyText() {
    return this.#keyText;
  }

  set keyText(value) {
    const selectors = value.split(",").map(s => s.trim());
    const normalized = [];
    for (const sel of selectors) {
      if (!validateKeyframeSelector(sel)) {
        throw DOMException.create(this._globalObject, [
          "Invalid keyframe selector.",
          "SyntaxError"
        ]);
      }
      normalized.push(normalizeSelector(sel));
    }
    this.#keyText = normalized.join(", ");
  }

  get cssText() {
    return `${this.#keyText} { ${this.style.cssText} }`;
  }
}

exports.implementation = CSSKeyframeRuleImpl;
exports.normalizeKeyText = normalizeKeyText;
