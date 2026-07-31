"use strict";

const parsers = require("../helpers/css-values");
const paddingTop = require("./paddingTop");
const paddingRight = require("./paddingRight");
const paddingBottom = require("./paddingBottom");
const paddingLeft = require("./paddingLeft");

const property = "padding";

const position = "edges";

const shorthandFor = new Map([
  [paddingTop.property, paddingTop],
  [paddingRight.property, paddingRight],
  [paddingBottom.property, paddingBottom],
  [paddingLeft.property, paddingLeft]
]);

const descriptor = {
  set(v) {
    v = v.trim();
    if (parsers.hasVarFunc(v)) {
      for (const [longhand] of shorthandFor) {
        this._setProperty(longhand, "");
      }
      this._setProperty(property, v);
    } else {
      const val = parse(v);
      if (Array.isArray(val) || typeof val === "string") {
        const priority = this._priorities.get(property) ?? "";
        this._positionShorthandSetter(property, val, priority);
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
 * Parses the padding property value.
 *
 * @param {string} v - The value to parse.
 * @returns {Array<string>|string|undefined} The parsed value or undefined if invalid.
 */
function parse(v) {
  if (v === "") {
    return v;
  }
  const values = parsers.parsePropertyValue(property, v);
  const parsedValues = [];
  if (Array.isArray(values) && values.length) {
    if (values.length > 4) {
      return undefined;
    }
    for (const value of values) {
      const parsedValue = parsers.resolveNumericValue([value], {
        length: values.length,
        min: 0,
        type: "length"
      });
      if (!parsedValue) {
        return undefined;
      }
      parsedValues.push(parsedValue);
    }
  } else if (typeof values === "string") {
    parsedValues.push(values);
  }
  if (parsedValues.length) {
    return parsedValues;
  }
  return undefined;
}

module.exports = {
  descriptor,
  parse,
  position,
  property,
  shorthandFor
};
