"use strict";

const parsers = require("../helpers/css-values");
const borderRightWidth = require("./borderRightWidth");
const borderRightStyle = require("./borderRightStyle");
const borderRightColor = require("./borderRightColor");

const property = "border-right";
const shorthand = "border";

const subProps = {
  width: borderRightWidth.property,
  style: borderRightStyle.property,
  color: borderRightColor.property
};

const initialValues = new Map([
  [borderRightWidth.property, "medium"],
  [borderRightStyle.property, "none"],
  [borderRightColor.property, "currentcolor"]
]);

const shorthandFor = new Map([
  [borderRightWidth.property, borderRightWidth],
  [borderRightStyle.property, borderRightStyle],
  [borderRightColor.property, borderRightColor]
]);

const descriptor = {
  set(v) {
    v = v.trim();
    if (parsers.hasVarFunc(v)) {
      this._borderSetter(property, v, "");
    } else {
      const val = parse(v);
      if (val || typeof val === "string") {
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
 * Parses the border-right property value.
 *
 * @param {string} v - The value to parse.
 * @returns {object|string|undefined} The parsed value or undefined if invalid.
 */
function parse(v) {
  if (v === "") {
    return v;
  }
  const values = parsers.splitValue(v);
  const parsedValues = new Map();
  for (const val of values) {
    const value = parsers.parsePropertyValue(property, val);
    if (Array.isArray(value) && value.length === 1) {
      const parsedValue = parsers.resolveBorderShorthandValue(value, subProps, parsedValues);
      if (typeof parsedValue === "string") {
        return parsedValue;
      } else if (Array.isArray(parsedValue)) {
        const [key, resolvedVal] = parsedValue;
        parsedValues.set(key, resolvedVal);
      } else {
        return undefined;
      }
    } else {
      return undefined;
    }
  }
  if (parsedValues.size) {
    const keys = shorthandFor.keys();
    const obj = {
      [borderRightWidth.property]: "medium"
    };
    for (const key of keys) {
      if (parsedValues.has(key)) {
        const parsedValue = parsedValues.get(key);
        if (parsedValue !== initialValues.get(key)) {
          obj[key] = parsedValues.get(key);
          if (obj[borderRightWidth.property] && obj[borderRightWidth.property] === "medium") {
            delete obj[borderRightWidth.property];
          }
        }
      }
    }
    return obj;
  }
  return undefined;
}

module.exports = {
  descriptor,
  initialValues,
  parse,
  property,
  shorthandFor
};
