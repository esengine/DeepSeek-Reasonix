"use strict";

const parsers = require("../helpers/css-values");
const backgroundImage = require("./backgroundImage");
const backgroundPosition = require("./backgroundPosition");
const backgroundSize = require("./backgroundSize");
const backgroundRepeat = require("./backgroundRepeat");
const backgroundOrigin = require("./backgroundOrigin");
const backgroundClip = require("./backgroundClip");
const backgroundAttachment = require("./backgroundAttachment");
const backgroundColor = require("./backgroundColor");

const property = "background";

const initialValues = new Map([
  [backgroundImage.property, "none"],
  [backgroundPosition.property, "0% 0%"],
  [backgroundSize.property, "auto"],
  [backgroundRepeat.property, "repeat"],
  [backgroundOrigin.property, "padding-box"],
  [backgroundClip.property, "border-box"],
  [backgroundAttachment.property, "scroll"],
  [backgroundColor.property, "transparent"]
]);

const shorthandFor = new Map([
  [backgroundImage.property, backgroundImage],
  [backgroundPosition.property, backgroundPosition],
  [backgroundSize.property, backgroundSize],
  [backgroundRepeat.property, backgroundRepeat],
  [backgroundOrigin.property, backgroundOrigin],
  [backgroundClip.property, backgroundClip],
  [backgroundAttachment.property, backgroundAttachment],
  [backgroundColor.property, backgroundColor]
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
      const bgValues = parse(v);
      if (typeof bgValues === "string") {
        for (const [key] of shorthandFor) {
          this._setProperty(key, bgValues);
        }
        this._setProperty(property, bgValues);
        return;
      }
      if (!Array.isArray(bgValues)) {
        return;
      }
      const bgMap = new Map([
        [backgroundImage.property, []],
        [backgroundPosition.property, []],
        [backgroundSize.property, []],
        [backgroundRepeat.property, []],
        [backgroundOrigin.property, []],
        [backgroundClip.property, []],
        [backgroundAttachment.property, []],
        [backgroundColor.property, []]
      ]);
      const backgrounds = [];
      for (const bgValue of bgValues) {
        const bg = [];
        let hasPosition = false;
        const originValue = bgValue[backgroundOrigin.property];
        const clipValue = bgValue[backgroundClip.property];
        const isDefaultBox =
          originValue === initialValues.get(backgroundOrigin.property) &&
          clipValue === initialValues.get(backgroundClip.property);
        for (const [longhand] of shorthandFor) {
          const value = bgValue[longhand];
          if (value) {
            const arr = bgMap.get(longhand);
            arr.push(value);
            if (longhand === backgroundOrigin.property) {
              if (!isDefaultBox) {
                bg.push(originValue);
              }
            } else if (longhand === backgroundClip.property) {
              if (!isDefaultBox && originValue !== clipValue) {
                bg.push(clipValue);
              }
            } else if (value !== initialValues.get(longhand)) {
              if (longhand === backgroundPosition.property) {
                hasPosition = true;
                bg.push(value);
              } else if (longhand === backgroundSize.property) {
                if (hasPosition) {
                  bg.push(`/ ${value}`);
                } else {
                  bg.push(initialValues.get(backgroundPosition.property), `/ ${value}`);
                }
              } else {
                bg.push(value);
              }
            } else if (longhand === backgroundImage.property && v === "none") {
              bg.push(value);
            } else if (longhand === backgroundColor.property && v === "transparent") {
              bg.push(value);
            }
          }
        }
        backgrounds.push(bg.join(" "));
      }
      const priority = this._priorities.get(property) ?? "";
      for (const [longhand, value] of bgMap) {
        this._setProperty(longhand, value.join(", "), priority);
      }
      this._setProperty(property, backgrounds.join(", "), priority);
    }
  },
  get() {
    const v = this.getPropertyValue(property);
    if (parsers.hasVarFunc(v)) {
      return v;
    }
    if (parsers.isGlobalKeyword(v)) {
      for (const [longhand] of shorthandFor) {
        if (this.getPropertyValue(longhand) !== v) {
          return "";
        }
      }
      return v;
    }
    const bgMap = new Map();
    let l = 0;
    for (const [longhand] of shorthandFor) {
      const val = this.getPropertyValue(longhand);
      if (!val || parsers.hasVarFunc(val)) {
        return "";
      }
      if (longhand === backgroundImage.property) {
        if (val === "none" && v === "none" && this.getPropertyValue(backgroundColor.property) === "transparent") {
          return val;
        }
        if (val !== initialValues.get(longhand)) {
          const imgValues = parsers.splitValue(val, {
            delimiter: ","
          });
          l = Math.max(l, imgValues.length);
          bgMap.set(longhand, imgValues);
        }
      } else if (longhand === backgroundColor.property) {
        if (val !== initialValues.get(longhand) || v.includes(val)) {
          bgMap.set(longhand, [val]);
        }
      } else if (val !== initialValues.get(longhand)) {
        const values = parsers.splitValue(val, {
          delimiter: ","
        });
        l = Math.max(l, values.length);
        bgMap.set(longhand, values);
      }
    }
    if (l === 0) {
      const bgColArr = bgMap.get(backgroundColor.property);
      const background = bgColArr ? bgColArr[0] : null;
      if (background) {
        return background;
      }
      return "";
    }
    const bgValues = [];
    for (let i = 0; i < l; i++) {
      const bg = [];
      let hasPosition = false;
      let originValue, clipValue;
      const originValues = bgMap.get(backgroundOrigin.property);
      const clipValues = bgMap.get(backgroundClip.property);
      if (originValues) {
        if (originValues[i] !== undefined) {
          originValue = originValues[i];
        } else {
          originValue = initialValues.get(backgroundOrigin.property);
        }
      } else {
        originValue = initialValues.get(backgroundOrigin.property);
      }
      if (clipValues) {
        if (clipValues[i] !== undefined) {
          clipValue = clipValues[i];
        } else {
          clipValue = initialValues.get(backgroundClip.property);
        }
      } else {
        clipValue = initialValues.get(backgroundClip.property);
      }
      const isDefaultBox =
        originValue === initialValues.get(backgroundOrigin.property) &&
        clipValue === initialValues.get(backgroundClip.property);
      let boxSet = false;
      for (const [longhand] of shorthandFor) {
        let value;
        if (bgMap.has(longhand)) {
          const values = bgMap.get(longhand);
          value = values[i] !== undefined ? values[i] : initialValues.get(longhand);
        } else {
          value = initialValues.get(longhand);
        }
        if (parsers.hasVarFunc(value)) {
          return "";
        }
        if (longhand === backgroundOrigin.property || longhand === backgroundClip.property) {
          if (isDefaultBox || boxSet) {
            continue;
          } else {
            if (originValue === clipValue) {
              bg.push(originValue);
            } else {
              bg.push(originValue, clipValue);
            }
            boxSet = true;
          }
        } else if (longhand === backgroundColor.property) {
          if (i === l - 1 && (value !== initialValues.get(longhand) || bgMap.has(longhand))) {
            bg.push(value);
          }
        } else if (value !== initialValues.get(longhand)) {
          if (longhand === backgroundPosition.property) {
            hasPosition = true;
            bg.push(value);
          } else if (longhand === backgroundSize.property) {
            if (hasPosition) {
              bg.push(`/ ${value}`);
            } else {
              bg.push(initialValues.get(backgroundPosition.property), `/ ${value}`);
            }
          } else {
            bg.push(value);
          }
        }
      }
      bgValues.push(bg.join(" "));
    }
    return bgValues.join(", ");
  },
  enumerable: true,
  configurable: true
};

/**
 * Parses the background property value.
 *
 * @param {string} v - The value to parse.
 * @returns {Array<object>|undefined} The parsed background values or undefined if invalid.
 */
function parse(v) {
  if (v === "" || parsers.isGlobalKeyword(v)) {
    return v;
  } else if (parsers.hasCalcFunc(v)) {
    v = parsers.resolveCalc(v);
  }
  if (!parsers.isValidPropertyValue(property, v)) {
    return undefined;
  }
  const values = parsers.splitValue(v, {
    delimiter: ","
  });
  const bgValues = [];
  const l = values.length;
  for (let i = 0; i < l; i++) {
    let bg = {
      [backgroundImage.property]: initialValues.get(backgroundImage.property),
      [backgroundPosition.property]: initialValues.get(backgroundPosition.property),
      [backgroundSize.property]: initialValues.get(backgroundSize.property),
      [backgroundRepeat.property]: initialValues.get(backgroundRepeat.property),
      [backgroundOrigin.property]: initialValues.get(backgroundOrigin.property),
      [backgroundClip.property]: initialValues.get(backgroundClip.property),
      [backgroundAttachment.property]: initialValues.get(backgroundAttachment.property),
      [backgroundColor.property]: initialValues.get(backgroundColor.property)
    };
    if (l > 1 && i !== l - 1) {
      bg = {
        [backgroundImage.property]: initialValues.get(backgroundImage.property),
        [backgroundPosition.property]: initialValues.get(backgroundPosition.property),
        [backgroundSize.property]: initialValues.get(backgroundSize.property),
        [backgroundRepeat.property]: initialValues.get(backgroundRepeat.property),
        [backgroundOrigin.property]: initialValues.get(backgroundOrigin.property),
        [backgroundClip.property]: initialValues.get(backgroundClip.property),
        [backgroundAttachment.property]: initialValues.get(backgroundAttachment.property)
      };
    }
    const bgPosition = [];
    const bgSize = [];
    const bgRepeat = [];
    const bgParts = parsers.splitValue(values[i], {
      delimiter: "/"
    });
    if (!bgParts.length || bgParts.length > 2) {
      return undefined;
    }
    const [bgPart1, bgPart2 = ""] = bgParts;
    const parts1 = parsers.splitValue(bgPart1);
    let originFilled = false;
    for (const part of parts1) {
      let partValid = false;
      for (const [longhand, value] of shorthandFor) {
        if (parsers.isValidPropertyValue(longhand, part)) {
          partValid = true;
          switch (longhand) {
            case backgroundClip.property: {
              const parsedValue = value.parse(part);
              if (parsedValue) {
                bg[longhand] = parsedValue;
              }
              break;
            }
            case backgroundOrigin.property: {
              const parsedValue = value.parse(part);
              if (parsedValue && !originFilled) {
                bg[longhand] = parsedValue;
                originFilled = true;
              }
              break;
            }
            case backgroundColor.property: {
              if (i !== values.length - 1) {
                return undefined;
              }
              const parsedValue = value.parse(part);
              if (parsedValue) {
                bg[longhand] = parsedValue;
              }
              break;
            }
            case backgroundPosition.property: {
              const parsedValue = value.parse(part);
              if (parsedValue) {
                bgPosition.push(parsedValue);
              }
              break;
            }
            case backgroundRepeat.property: {
              const parsedValue = value.parse(part);
              if (parsedValue) {
                bgRepeat.push(parsedValue);
              }
              break;
            }
            case backgroundSize.property: {
              break;
            }
            default: {
              const parsedValue = value.parse(part);
              if (parsedValue) {
                bg[longhand] = parsedValue;
              }
            }
          }
        }
      }
      if (!partValid) {
        return undefined;
      }
    }
    if (bgPart2) {
      const parts2 = parsers.splitValue(bgPart2);
      for (const part of parts2) {
        let partValid = false;
        for (const [longhand, value] of shorthandFor) {
          if (parsers.isValidPropertyValue(longhand, part)) {
            partValid = true;
            switch (longhand) {
              case backgroundClip.property: {
                const parsedValue = value.parse(part);
                if (parsedValue) {
                  bg[longhand] = parsedValue;
                }
                break;
              }
              case backgroundOrigin.property: {
                const parsedValue = value.parse(part);
                if (parsedValue && !originFilled) {
                  bg[longhand] = parsedValue;
                  originFilled = true;
                }
                break;
              }
              case backgroundColor.property: {
                if (i !== l - 1) {
                  return undefined;
                }
                const parsedValue = value.parse(part);
                if (parsedValue) {
                  bg[longhand] = parsedValue;
                }
                break;
              }
              case backgroundPosition.property: {
                break;
              }
              case backgroundRepeat.property: {
                const parsedValue = value.parse(part);
                if (parsedValue) {
                  bgRepeat.push(parsedValue);
                }
                break;
              }
              case backgroundSize.property: {
                const parsedValue = value.parse(part);
                if (parsedValue) {
                  bgSize.push(parsedValue);
                }
                break;
              }
              default: {
                const parsedValue = value.parse(part);
                if (parsedValue) {
                  bg[longhand] = parsedValue;
                }
              }
            }
          }
        }
        if (!partValid) {
          return undefined;
        }
      }
    }
    if (bgPosition.length) {
      const { parse: parser } = shorthandFor.get(backgroundPosition.property);
      const value = parser(bgPosition.join(" "));
      if (value) {
        bg[backgroundPosition.property] = value;
      }
    }
    if (bgSize.length) {
      const { parse: parser } = shorthandFor.get(backgroundSize.property);
      const value = parser(bgSize.join(" "));
      if (value) {
        bg[backgroundSize.property] = value;
      }
    }
    if (bgRepeat.length) {
      const { parse: parser } = shorthandFor.get(backgroundRepeat.property);
      const value = parser(bgRepeat.join(" "));
      if (value) {
        bg[backgroundRepeat.property] = value;
      }
    }
    bgValues.push(bg);
  }
  return bgValues;
}

module.exports = {
  descriptor,
  initialValues,
  parse,
  property,
  shorthandFor
};
