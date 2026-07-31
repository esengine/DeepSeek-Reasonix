"use strict";

const parsers = require("../helpers/css-values");
const fontStyle = require("./fontStyle");
const fontVariant = require("./fontVariant");
const fontWeight = require("./fontWeight");
const fontSize = require("./fontSize");
const lineHeight = require("./lineHeight");
const fontFamily = require("./fontFamily");

// Constants
const { AST_TYPES } = parsers;

const property = "font";

const shorthandFor = new Map([
  [fontStyle.property, fontStyle],
  [fontVariant.property, fontVariant],
  [fontWeight.property, fontWeight],
  [fontSize.property, fontSize],
  [lineHeight.property, lineHeight],
  [fontFamily.property, fontFamily]
]);

const descriptor = {
  set(v) {
    v = v.trim();
    if (v === "" || parsers.hasVarFunc(v)) {
      for (const [key] of shorthandFor) {
        this._setProperty(key, "");
      }
      this._setProperty(property, v);
    } else {
      const obj = parse(v);
      if (!obj) {
        return;
      }
      const priority = this._priorities.get(property) ?? "";
      const str = new Set();
      for (const [key] of shorthandFor) {
        const val = obj[key];
        if (typeof val === "string") {
          this._setProperty(key, val, priority);
          if (val && val !== "normal" && !str.has(val)) {
            if (key === lineHeight.property) {
              str.add(`/ ${val}`);
            } else {
              str.add(val);
            }
          }
        }
      }
      this._setProperty(property, [...str].join(" "), priority);
    }
  },
  get() {
    const val = this.getPropertyValue(property);
    if (parsers.hasVarFunc(val)) {
      return val;
    }
    const str = new Set();
    for (const [key] of shorthandFor) {
      const v = this.getPropertyValue(key);
      if (parsers.hasVarFunc(v)) {
        return "";
      }
      if (v && v !== "normal" && !str.has(v)) {
        if (key === lineHeight.property) {
          str.add(`/ ${v}`);
        } else {
          str.add(`${v}`);
        }
      }
    }
    return [...str].join(" ");
  },
  enumerable: true,
  configurable: true
};

/**
 * Parses the font property value.
 *
 * @param {string} v - The value to parse.
 * @returns {object|undefined} The parsed value object or undefined if invalid.
 */
function parse(v) {
  if (v === "") {
    return v;
  } else if (parsers.hasCalcFunc(v)) {
    v = parsers.resolveCalc(v);
  }
  if (!parsers.isValidPropertyValue(property, v)) {
    return undefined;
  }
  const [fontBlock, ...families] = parsers.splitValue(v, {
    delimiter: ","
  });
  const [fontBlockA, fontBlockB] = parsers.splitValue(fontBlock, {
    delimiter: "/"
  });
  const font = {
    [fontStyle.property]: "normal",
    [fontVariant.property]: "normal",
    [fontWeight.property]: "normal"
  };
  const fontFamilies = new Set();
  if (fontBlockB) {
    const [lineB, ...familiesB] = fontBlockB.trim().split(" ");
    if (!lineB || !familiesB.length) {
      return undefined;
    }
    const lineHeightB = lineHeight.parse(lineB);
    if (typeof lineHeightB !== "string") {
      return undefined;
    }
    const familyB = fontFamily.parse(familiesB.join(" "));
    if (typeof familyB === "string") {
      fontFamilies.add(familyB);
    } else {
      return undefined;
    }
    const parts = parsers.splitValue(fontBlockA.trim());
    const properties = [fontStyle.property, fontVariant.property, fontWeight.property, fontSize.property];
    for (const part of parts) {
      if (part === "normal") {
        continue;
      } else {
        for (const longhand of properties) {
          switch (longhand) {
            case fontSize.property: {
              const parsedValue = fontSize.parse(part);
              if (typeof parsedValue === "string") {
                font[longhand] = parsedValue;
              }
              break;
            }
            case fontStyle.property:
            case fontWeight.property: {
              if (font[longhand] === "normal") {
                const longhandItem = shorthandFor.get(longhand);
                const parsedValue = longhandItem.parse(part);
                if (typeof parsedValue === "string") {
                  font[longhand] = parsedValue;
                }
              }
              break;
            }
            case fontVariant.property: {
              if (font[longhand] === "normal") {
                const parsedValue = fontVariant.parse(part);
                if (typeof parsedValue === "string") {
                  if (parsedValue === "small-cap") {
                    font[longhand] = parsedValue;
                  } else if (parsedValue !== "normal") {
                    return undefined;
                  }
                }
              }
              break;
            }
            default:
          }
        }
      }
    }
    if (Object.hasOwn(font, fontSize.property)) {
      font[lineHeight.property] = lineHeightB;
    } else {
      return undefined;
    }
  } else {
    const revParts = parsers.splitValue(fontBlockA.trim()).toReversed();
    if (revParts.length === 1) {
      const [part] = revParts;
      const value = parsers.parsePropertyValue(property, part);
      if (Array.isArray(value) && value.length === 1) {
        const [{ name, type }] = value;
        if (type === AST_TYPES.GLOBAL_KEYWORD) {
          return {
            [fontStyle.property]: name,
            [fontVariant.property]: name,
            [fontWeight.property]: name,
            [fontSize.property]: name,
            [lineHeight.property]: name,
            [fontFamily.property]: name
          };
        }
      }
      return undefined;
    }
    const properties = [fontStyle.property, fontVariant.property, fontWeight.property, lineHeight.property];
    for (const longhand of properties) {
      font[longhand] = "normal";
    }
    const revFontFamily = [];
    let fontSizeA;
    for (const part of revParts) {
      if (fontSizeA) {
        if (/^normal$/i.test(part)) {
          continue;
        } else {
          for (const longhand of properties) {
            switch (longhand) {
              case fontStyle.property:
              case fontWeight.property:
              case lineHeight.property: {
                if (font[longhand] === "normal") {
                  const longhandItem = shorthandFor.get(longhand);
                  const parsedValue = longhandItem.parse(part);
                  if (typeof parsedValue === "string") {
                    font[longhand] = parsedValue;
                  }
                }
                break;
              }
              case fontVariant.property: {
                if (font[longhand] === "normal") {
                  const parsedValue = fontVariant.parse(part);
                  if (typeof parsedValue === "string") {
                    if (parsedValue === "small-cap") {
                      font[longhand] = parsedValue;
                    } else if (parsedValue !== "normal") {
                      return undefined;
                    }
                  }
                }
                break;
              }
              default:
            }
          }
        }
      } else {
        const parsedFontSize = fontSize.parse(part);
        if (typeof parsedFontSize === "string") {
          fontSizeA = parsedFontSize;
        } else {
          const parsedFontFamily = fontFamily.parse(part);
          if (typeof parsedFontFamily === "string") {
            revFontFamily.push(parsedFontFamily);
          } else {
            return undefined;
          }
        }
      }
    }
    const family = fontFamily.parse(revFontFamily.toReversed().join(" "));
    if (fontSizeA && family) {
      font[fontSize.property] = fontSizeA;
      fontFamilies.add(fontFamily.parse(family));
    } else {
      return undefined;
    }
  }
  for (const family of families) {
    const parsedFontFamily = fontFamily.parse(family);
    if (parsedFontFamily) {
      fontFamilies.add(parsedFontFamily);
    } else {
      return undefined;
    }
  }
  font[fontFamily.property] = [...fontFamilies].join(", ");
  return font;
}

module.exports = {
  descriptor,
  parse,
  property,
  shorthandFor
};
