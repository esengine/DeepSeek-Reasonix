"use strict";

const parsers = require("../helpers/css-values");
const borderWidth = require("./borderWidth");
const borderStyle = require("./borderStyle");
const borderColor = require("./borderColor");
const borderTop = require("./borderTop");
const borderRight = require("./borderRight");
const borderBottom = require("./borderBottom");
const borderLeft = require("./borderLeft");

const property = "border";

const subProps = {
  width: borderWidth.property,
  style: borderStyle.property,
  color: borderColor.property
};

const initialValues = new Map([
  [borderWidth.property, "medium"],
  [borderStyle.property, "none"],
  [borderColor.property, "currentcolor"]
]);

const shorthandFor = new Map([
  [borderWidth.property, borderWidth],
  [borderStyle.property, borderStyle],
  [borderColor.property, borderColor]
]);

const positionShorthandFor = new Map([
  [borderTop.property, borderTop],
  [borderRight.property, borderRight],
  [borderBottom.property, borderBottom],
  [borderLeft.property, borderLeft]
]);

const descriptor = {
  set(v) {
    v = v.trim();
    if (parsers.hasVarFunc(v)) {
      this._borderSetter(property, v, "");
    } else {
      const val = parse(v);
      if (val || typeof val === "string") {
        const priority = this._priorities.get(property) ?? "";
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
 * Parses the border property value.
 *
 * @param {string} v - The value to parse.
 * @returns {object|string|undefined} The parsed value or undefined if invalid.
 */
function parse(v) {
  if (v === "" || parsers.hasVarFunc(v)) {
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
      [borderWidth.property]: "medium"
    };
    for (const key of keys) {
      if (parsedValues.has(key)) {
        const parsedValue = parsedValues.get(key);
        if (parsedValue !== initialValues.get(key)) {
          obj[key] = parsedValues.get(key);
          if (obj[borderWidth.property] && obj[borderWidth.property] === "medium") {
            delete obj[borderWidth.property];
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
  positionShorthandFor,
  property,
  shorthandFor
};
