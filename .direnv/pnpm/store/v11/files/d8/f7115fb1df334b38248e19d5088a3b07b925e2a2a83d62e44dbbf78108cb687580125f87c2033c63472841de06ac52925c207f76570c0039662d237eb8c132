//#region src/js/constant.ts
/**
* constant
*/
var _DIGIT = "(?:0|[1-9]\\d*)";
var _MATH = `clamp|max|min|exp|hypot|log|pow|sqrt|abs|sign|mod|rem|round|a?(?:cos|sin|tan)|atan2`;
var _CALC = `calc|${_MATH}`;
var _VAR = `var|${_CALC}`;
var ANGLE = "deg|g?rad|turn";
var LENGTH = "[cm]m|[dls]?v(?:[bhiw]|max|min)|in|p[ctx]|q|r?(?:[cl]h|cap|e[mx]|ic)";
var NUM = `[+-]?(?:${_DIGIT}(?:\\.\\d*)?|\\.\\d+)(?:e-?${_DIGIT})?`;
var NUM_POSITIVE = `\\+?(?:${_DIGIT}(?:\\.\\d*)?|\\.\\d+)(?:e-?${_DIGIT})?`;
var NONE = "none";
var PCT = `${NUM}%`;
var SYN_FN_CALC = `^(?:${_CALC})\\(|(?<=[*\\/\\s\\(])(?:${_CALC})\\(`;
var SYN_FN_MATH_START = `^(?:${_MATH})\\($`;
var SYN_FN_VAR = "^var\\(|(?<=[*\\/\\s\\(])var\\(";
var SYN_FN_VAR_START = `^(?:${_VAR})\\(`;
var _ALPHA = `(?:\\s*\\/\\s*(?:${NUM}|${PCT}|${NONE}))?`;
var _ALPHA_LV3 = `(?:\\s*,\\s*(?:${NUM}|${PCT}))?`;
var _COLOR_FUNC = "(?:ok)?l(?:ab|ch)|color|hsla?|hwb|rgba?";
var _COLOR_KEY = "[a-z]+|#[\\da-f]{3}|#[\\da-f]{4}|#[\\da-f]{6}|#[\\da-f]{8}";
var _CS_HUE = "(?:ok)?lch|hsl|hwb";
var _CS_HUE_ARC = "(?:de|in)creasing|longer|shorter";
var _NUM_ANGLE = `${NUM}(?:${ANGLE})?`;
var _NUM_ANGLE_NONE = `(?:${NUM}(?:${ANGLE})?|${NONE})`;
var _NUM_PCT_NONE = `(?:${NUM}|${PCT}|${NONE})`;
var CS_HUE = `(?:${_CS_HUE})(?:\\s(?:${_CS_HUE_ARC})\\shue)?`;
var CS_HUE_CAPT = `(${_CS_HUE})(?:\\s(${_CS_HUE_ARC})\\shue)?`;
var CS_LAB = "(?:ok)?lab";
var CS_LCH = "(?:ok)?lch";
var CS_RGB = `(?:a98|prophoto)-rgb|display-p3|rec2020|srgb(?:-linear)?`;
var CS_XYZ = "xyz(?:-d(?:50|65))?";
var CS_RECT = `${CS_LAB}|${CS_RGB}|${CS_XYZ}`;
var CS_MIX = `${CS_HUE}|${CS_RECT}`;
var FN_COLOR = "color(";
var FN_LIGHT_DARK = "light-dark(";
var FN_MIX = "color-mix(";
var FN_REL = `(?:${_COLOR_FUNC})\\(\\s*from\\s+`;
var FN_REL_CAPT = `(${_COLOR_FUNC})\\(\\s*from\\s+`;
var FN_VAR = "var(";
var SYN_FN_COLOR = `(?:${CS_RGB}|${CS_XYZ})(?:\\s+${_NUM_PCT_NONE}){3}${_ALPHA}`;
var SYN_FN_LIGHT_DARK = "^light-dark\\(";
var SYN_FN_REL = `^${FN_REL}|(?<=[\\s])${FN_REL}`;
var SYN_HSL = `${_NUM_ANGLE_NONE}(?:\\s+${_NUM_PCT_NONE}){2}${_ALPHA}`;
var SYN_HSL_LV3 = `${_NUM_ANGLE}(?:\\s*,\\s*${PCT}){2}${_ALPHA_LV3}`;
var SYN_LCH = `(?:${_NUM_PCT_NONE}\\s+){2}${_NUM_ANGLE_NONE}${_ALPHA}`;
var SYN_MOD = `${_NUM_PCT_NONE}(?:\\s+${_NUM_PCT_NONE}){2}${_ALPHA}`;
var SYN_RGB_LV3 = `(?:${NUM}(?:\\s*,\\s*${NUM}){2}|${PCT}(?:\\s*,\\s*${PCT}){2})${_ALPHA_LV3}`;
var SYN_COLOR_TYPE = `${_COLOR_KEY}|hsla?\\(\\s*${SYN_HSL_LV3}\\s*\\)|rgba?\\(\\s*${SYN_RGB_LV3}\\s*\\)|(?:hsla?|hwb)\\(\\s*${SYN_HSL}\\s*\\)|(?:(?:ok)?lab|rgba?)\\(\\s*${SYN_MOD}\\s*\\)|(?:ok)?lch\\(\\s*${SYN_LCH}\\s*\\)|color\\(\\s*${SYN_FN_COLOR}\\s*\\)`;
var SYN_MIX_PART = `(?:${SYN_COLOR_TYPE})(?:\\s+${PCT})?`;
var SYN_MIX = `color-mix\\(\\s*in\\s+(?:${CS_MIX})\\s*,\\s*${SYN_MIX_PART}\\s*,\\s*${SYN_MIX_PART}\\s*\\)`;
var SYN_MIX_CAPT = `color-mix\\(\\s*in\\s+(${CS_MIX})\\s*,\\s*(${SYN_MIX_PART})\\s*,\\s*(${SYN_MIX_PART})\\s*\\)`;
var VAL_COMP = "computedValue";
var VAL_MIX = "mixValue";
var VAL_SPEC = "specifiedValue";
//#endregion
export { ANGLE, CS_HUE, CS_HUE_CAPT, CS_LAB, CS_LCH, CS_MIX, CS_RECT, CS_RGB, CS_XYZ, FN_COLOR, FN_LIGHT_DARK, FN_MIX, FN_REL, FN_REL_CAPT, FN_VAR, LENGTH, NONE, NUM, NUM_POSITIVE, PCT, SYN_COLOR_TYPE, SYN_FN_CALC, SYN_FN_COLOR, SYN_FN_LIGHT_DARK, SYN_FN_MATH_START, SYN_FN_REL, SYN_FN_VAR, SYN_FN_VAR_START, SYN_HSL, SYN_HSL_LV3, SYN_LCH, SYN_MIX, SYN_MIX_CAPT, SYN_MIX_PART, SYN_MOD, SYN_RGB_LV3, VAL_COMP, VAL_MIX, VAL_SPEC };

//# sourceMappingURL=constant.js.map