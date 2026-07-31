"use strict";

const parsers = require("../helpers/css-values");

const property = "border-spacing";

const descriptor = {
  set(v) {
    v = v.trim();
    if (parsers.hasVarFunc(v)) {
      this._setProperty(property, v);
    } else {
      const val = parse(v);
      if (typeof val === "string") {
        const priority = this._priorities.get(property) ?? "";
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
 * Parses the border-spacing property value.
 *
 * @param {string} v - The value to parse.
 * @returns {string|undefined} The parsed value or undefined if invalid.
 */
function parse(v) {
  if (v === "") {
    return v;
  }
  const value = parsers.parsePropertyValue(property, v);
  if (Array.isArray(value) && value.length) {
    switch (value.length) {
      case 1: {
        return parsers.resolveNumericValue(value, {
          type: "length"
        });
      }
      case 2: {
        const [part1, part2] = value;
        const val1 = parsers.resolveNumericValue([part1], {
          type: "length"
        });
        const val2 = parsers.resolveNumericValue([part2], {
          type: "length"
        });
        if (val1 && val2) {
          return `${val1} ${val2}`;
        }
        break;
      }
      default:
    }
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
