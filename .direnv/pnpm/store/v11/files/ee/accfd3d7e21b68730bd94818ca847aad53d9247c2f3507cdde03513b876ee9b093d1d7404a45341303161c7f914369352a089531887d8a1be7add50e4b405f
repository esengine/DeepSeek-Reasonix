"use strict";

const parsers = require("../helpers/css-values");
const borderTopColor = require("./borderTopColor");
const borderRightColor = require("./borderRightColor");
const borderBottomColor = require("./borderBottomColor");
const borderLeftColor = require("./borderLeftColor");

const property = "border-color";
const shorthand = "border";

const shorthandFor = new Map([
  [borderTopColor.property, borderTopColor],
  [borderRightColor.property, borderRightColor],
  [borderBottomColor.property, borderBottomColor],
  [borderLeftColor.property, borderLeftColor]
]);

const descriptor = {
  set(v) {
    v = v.trim();
    if (parsers.hasVarFunc(v)) {
      this._borderSetter(property, v, "");
    } else {
      const val = parse(v);
      if (Array.isArray(val) || typeof val === "string") {
        const priority =
          !this._priorities.get(shorthand) && this._priorities.has(property) ? this._priorities.get(property) : "";
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
 * Parses the border-color property value.
 *
 * @param {string} v - The value to parse.
 * @returns {Array<string>|undefined} The parsed value or undefined if invalid.
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
      const parsedValue = parsers.resolveColorValue([value], {
        length: values.length
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
    switch (parsedValues.length) {
      case 1: {
        return parsedValues;
      }
      case 2: {
        const [val1, val2] = parsedValues;
        if (val1 === val2) {
          return [val1];
        }
        return parsedValues;
      }
      case 3: {
        const [val1, val2, val3] = parsedValues;
        if (val1 === val3) {
          if (val1 === val2) {
            return [val1];
          }
          return [val1, val2];
        }
        return parsedValues;
      }
      case 4: {
        const [val1, val2, val3, val4] = parsedValues;
        if (val2 === val4) {
          if (val1 === val3) {
            if (val1 === val2) {
              return [val1];
            }
            return [val1, val2];
          }
          return [val1, val2, val3];
        }
        return parsedValues;
      }
      default:
    }
  }
  return undefined;
}

module.exports = {
  descriptor,
  parse,
  property,
  shorthandFor
};
