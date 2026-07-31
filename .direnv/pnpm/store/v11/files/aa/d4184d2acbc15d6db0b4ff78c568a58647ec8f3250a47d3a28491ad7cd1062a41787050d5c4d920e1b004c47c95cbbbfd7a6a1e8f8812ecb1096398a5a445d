"use strict";

const parsers = require("../helpers/css-values");

const property = "font-weight";
const shorthand = "font";

const descriptor = {
  set(v) {
    v = v.trim();
    if (parsers.hasVarFunc(v)) {
      this._setProperty(shorthand, "");
      this._setProperty(property, v);
    } else {
      const val = parse(v);
      if (typeof val === "string") {
        const priority =
          !this._priorities.get(shorthand) && this._priorities.has(property) ? this._priorities.get(property) : "";
        this._setProperty(property, val, priority);
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
 * Parses the font-weight property value.
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
    const parsedValue = parsers.resolveNumericValue(value, {
      min: 1,
      max: 1000
    });
    if (!parsedValue) {
      return undefined;
    }
    return parsedValue;
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
