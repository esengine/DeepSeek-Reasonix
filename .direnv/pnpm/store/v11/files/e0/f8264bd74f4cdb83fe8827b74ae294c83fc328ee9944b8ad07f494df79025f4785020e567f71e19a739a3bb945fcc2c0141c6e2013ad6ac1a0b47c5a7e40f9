"use strict";

const parsers = require("../helpers/css-values");

const property = "border-left-color";
const lineShorthand = "border-color";
const positionShorthand = "border-left";
const shorthand = "border";

const descriptor = {
  set(v) {
    v = v.trim();
    if (parsers.hasVarFunc(v)) {
      this._borderSetter(property, v, "");
    } else {
      const val = parse(v);
      if (typeof val === "string") {
        const shorthandPriority = this._priorities.get(shorthand);
        const linePriority = this._priorities.get(lineShorthand);
        const positionPriority = this._priorities.get(positionShorthand);
        const priority =
          !(shorthandPriority || linePriority || positionPriority) && this._priorities.has(property) ?
            this._priorities.get(property) :
            "";
        this._borderSetter(property, val, priority);
      }
    }
  },
  get() {
    return this.getPropertyValue(property);
  },
  enumerable: true,
  configurable: true
};

/**
 * Parses the border-left-color property value.
 *
 * @param {string} v - The value to parse.
 * @returns {string|undefined} The parsed value or undefined if invalid.
 */
function parse(v) {
  if (v === "") {
    return v;
  }
  const value = parsers.parsePropertyValue(property, v);
  if (Array.isArray(value) && value.length === 1) {
    return parsers.resolveColorValue(value);
  } else if (typeof value === "string") {
    return value;
  }
  return undefined;
}

module.exports = {
  descriptor,
  parse,
  property
};
