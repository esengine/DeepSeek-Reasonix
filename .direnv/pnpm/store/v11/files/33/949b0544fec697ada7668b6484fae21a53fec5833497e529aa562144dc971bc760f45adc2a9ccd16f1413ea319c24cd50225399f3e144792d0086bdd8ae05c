import { CacheItem, createCacheKey, getCache, setCache } from "./cache.js";
import { isString } from "./common.js";
import { ANGLE, CS_HUE, CS_RECT, LENGTH, NUM, NUM_POSITIVE, PCT, VAL_COMP } from "./constant.js";
import { resolveColor } from "./resolve.js";
import { isColor, splitValue } from "./util.js";
//#region src/js/css-gradient.ts
/**
* css-gradient
*/
var NAMESPACE = "css-gradient";
var DIM_ANGLE = `${NUM}(?:${ANGLE})`;
var DIM_ANGLE_PCT = `${DIM_ANGLE}|${PCT}`;
var DIM_LEN_PCT = `${`${NUM}(?:${LENGTH})|0`}|${PCT}`;
var DIM_LEN_PCT_POSI = `${NUM_POSITIVE}(?:${LENGTH}|%)|0`;
var DIM_LEN_POSI = `${NUM_POSITIVE}(?:${LENGTH})|0`;
var CTR = "center";
var L_R = "left|right";
var T_B = "top|bottom";
var S_E = "start|end";
var AXIS_X = `${L_R}|x-(?:${S_E})`;
var AXIS_Y = `${T_B}|y-(?:${S_E})`;
var BLOCK = `block-(?:${S_E})`;
var INLINE = `inline-(?:${S_E})`;
var POS_1 = `${CTR}|${AXIS_X}|${AXIS_Y}|${BLOCK}|${INLINE}|${DIM_LEN_PCT}`;
var POS_2 = [
	`(?:${CTR}|${AXIS_X})\\s+(?:${CTR}|${AXIS_Y})`,
	`(?:${CTR}|${AXIS_Y})\\s+(?:${CTR}|${AXIS_X})`,
	`(?:${CTR}|${AXIS_X}|${DIM_LEN_PCT})\\s+(?:${CTR}|${AXIS_Y}|${DIM_LEN_PCT})`,
	`(?:${CTR}|${BLOCK})\\s+(?:${CTR}|${INLINE})`,
	`(?:${CTR}|${INLINE})\\s+(?:${CTR}|${BLOCK})`,
	`(?:${CTR}|${S_E})\\s+(?:${CTR}|${S_E})`
].join("|");
var POS_4 = [
	`(?:${AXIS_X})\\s+(?:${DIM_LEN_PCT})\\s+(?:${AXIS_Y})\\s+(?:${DIM_LEN_PCT})`,
	`(?:${AXIS_Y})\\s+(?:${DIM_LEN_PCT})\\s+(?:${AXIS_X})\\s+(?:${DIM_LEN_PCT})`,
	`(?:${BLOCK})\\s+(?:${DIM_LEN_PCT})\\s+(?:${INLINE})\\s+(?:${DIM_LEN_PCT})`,
	`(?:${INLINE})\\s+(?:${DIM_LEN_PCT})\\s+(?:${BLOCK})\\s+(?:${DIM_LEN_PCT})`,
	`(?:${S_E})\\s+(?:${DIM_LEN_PCT})\\s+(?:${S_E})\\s+(?:${DIM_LEN_PCT})`
].join("|");
var RAD_EXTENT = "(?:clos|farth)est-(?:corner|side)";
var RAD_SIZE = [
	`${RAD_EXTENT}(?:\\s+${RAD_EXTENT})?`,
	`${DIM_LEN_POSI}`,
	`(?:${DIM_LEN_PCT_POSI})\\s+(?:${DIM_LEN_PCT_POSI})`
].join("|");
var RAD_SHAPE = "circle|ellipse";
var FROM_ANGLE = `from\\s+${DIM_ANGLE}`;
var AT_POSITION = `at\\s+(?:${POS_1}|${POS_2}|${POS_4})`;
var TO_SIDE_CORNER = `to\\s+(?:(?:${L_R})(?:\\s(?:${T_B}))?|(?:${T_B})(?:\\s(?:${L_R}))?)`;
var IN_COLOR_SPACE = `in\\s+(?:${CS_RECT}|${CS_HUE})`;
var LINE_SYNTAX_LINEAR = [`(?:${DIM_ANGLE}|${TO_SIDE_CORNER})(?:\\s+${IN_COLOR_SPACE})?`, `${IN_COLOR_SPACE}(?:\\s+(?:${DIM_ANGLE}|${TO_SIDE_CORNER}))?`].join("|");
var LINE_SYNTAX_RADIAL = [
	`(?:${RAD_SHAPE})(?:\\s+(?:${RAD_SIZE}))?(?:\\s+${AT_POSITION})?(?:\\s+${IN_COLOR_SPACE})?`,
	`(?:${RAD_SIZE})(?:\\s+(?:${RAD_SHAPE}))?(?:\\s+${AT_POSITION})?(?:\\s+${IN_COLOR_SPACE})?`,
	`${AT_POSITION}(?:\\s+${IN_COLOR_SPACE})?`,
	`${IN_COLOR_SPACE}(?:\\s+${RAD_SHAPE})(?:\\s+(?:${RAD_SIZE}))?(?:\\s+${AT_POSITION})?`,
	`${IN_COLOR_SPACE}(?:\\s+${RAD_SIZE})(?:\\s+(?:${RAD_SHAPE}))?(?:\\s+${AT_POSITION})?`,
	`${IN_COLOR_SPACE}(?:\\s+${AT_POSITION})?`
].join("|");
var LINE_SYNTAX_CONIC = [
	`${FROM_ANGLE}(?:\\s+${AT_POSITION})?(?:\\s+${IN_COLOR_SPACE})?`,
	`${AT_POSITION}(?:\\s+${IN_COLOR_SPACE})?`,
	`${IN_COLOR_SPACE}(?:\\s+${FROM_ANGLE})?(?:\\s+${AT_POSITION})?`
].join("|");
var DEFAULT_LINEAR = [/to\s+bottom/];
var DEFAULT_RADIAL = [
	/ellipse/,
	/farthest-corner/,
	/at\s+center/
];
var DEFAULT_CONIC = [/at\s+center/];
var IS_CONIC = /^(?:repeating-)?conic-gradient$/;
var IS_LINEAR = /^(?:repeating-)?linear-gradient$/;
var IS_RADIAL = /^(?:repeating-)?radial-gradient$/;
var REG_COLOR_HINT_CONIC = new RegExp(`^(?:${DIM_ANGLE_PCT})$`);
var REG_COLOR_HINT_NON_CONIC = new RegExp(`^(?:${DIM_LEN_PCT})$`);
var REG_DIM_CONIC = new RegExp(`(?:\\s+(?:${DIM_ANGLE_PCT})){1,2}$`);
var REG_DIM_NON_CONIC = new RegExp(`(?:\\s+(?:${DIM_LEN_PCT})){1,2}$`);
var REG_GRAD = /^(?:repeating-)?(?:conic|linear|radial)-gradient\(/;
var REG_GRAD_CAPT = /^((?:repeating-)?(?:conic|linear|radial)-gradient)\(/;
var REG_LINE_CONIC = new RegExp(`^(?:${LINE_SYNTAX_CONIC})$`);
var REG_LINE_LINEAR = new RegExp(`^(?:${LINE_SYNTAX_LINEAR})$`);
var REG_LINE_RADIAL = new RegExp(`^(?:${LINE_SYNTAX_RADIAL})$`);
/**
* get gradient type
* @param value - gradient value
* @returns gradient type
*/
var getGradientType = (value) => {
	if (isString(value)) {
		value = value.trim();
		if (REG_GRAD.test(value)) {
			const [, type] = value.match(REG_GRAD_CAPT);
			return type;
		}
	}
	return "";
};
/**
* validate gradient line
* @param value - gradient line value
* @param type - gradient type
* @returns result
*/
var validateGradientLine = (value, type) => {
	if (isString(value) && isString(type)) {
		value = value.trim();
		type = type.trim();
		let reg = null;
		let defaultValues = [];
		if (IS_LINEAR.test(type)) {
			reg = REG_LINE_LINEAR;
			defaultValues = DEFAULT_LINEAR;
		} else if (IS_RADIAL.test(type)) {
			reg = REG_LINE_RADIAL;
			defaultValues = DEFAULT_RADIAL;
		} else if (IS_CONIC.test(type)) {
			reg = REG_LINE_CONIC;
			defaultValues = DEFAULT_CONIC;
		}
		if (reg) {
			const valid = reg.test(value);
			if (valid) {
				let line = value;
				for (const defaultValue of defaultValues) line = line.replace(defaultValue, "");
				line = line.replace(/\s{2,}/g, " ").trim();
				return {
					line,
					valid
				};
			}
			return {
				valid,
				line: value
			};
		}
	}
	return {
		line: value,
		valid: false
	};
};
/**
* validate color stop list
* @param list
* @param type
* @param [opt]
* @returns result
*/
var validateColorStopList = (list, type, opt = {}) => {
	if (Array.isArray(list) && list.length > 1) {
		const isConic = IS_CONIC.test(type);
		const regColorHint = isConic ? REG_COLOR_HINT_CONIC : REG_COLOR_HINT_NON_CONIC;
		const regDimension = isConic ? REG_DIM_CONIC : REG_DIM_NON_CONIC;
		const valueList = [];
		let prevType = "";
		for (let i = 0; i < list.length; i++) {
			const item = list[i];
			if (isString(item)) if (regColorHint.test(item)) {
				if (i === 0 || prevType === "hint") return {
					colorStops: list,
					valid: false
				};
				prevType = "hint";
				valueList.push(item);
			} else {
				const itemColor = item.replace(regDimension, "");
				if (isColor(itemColor, { format: "specifiedValue" })) {
					const resolvedColor = resolveColor(itemColor, opt);
					prevType = "color";
					valueList.push(item.replace(itemColor, resolvedColor));
				} else return {
					colorStops: list,
					valid: false
				};
			}
			else return {
				colorStops: list,
				valid: false
			};
		}
		if (prevType !== "color") return {
			colorStops: list,
			valid: false
		};
		return {
			valid: true,
			colorStops: valueList
		};
	}
	return {
		colorStops: list,
		valid: false
	};
};
/**
* parse CSS gradient
* @param value - gradient value
* @param [opt] - options
* @returns parsed result
*/
var parseGradient = (value, opt = {}) => {
	if (isString(value)) {
		value = value.trim();
		const cacheKey = createCacheKey({
			namespace: NAMESPACE,
			name: "parseGradient",
			value
		}, opt);
		const cachedResult = getCache(cacheKey);
		if (cachedResult instanceof CacheItem) {
			if (cachedResult.isNull) return null;
			return cachedResult.item;
		}
		const type = getGradientType(value);
		const gradValue = value.replace(REG_GRAD, "").replace(/\)$/, "");
		if (type && gradValue) {
			const [lineOrColorStop = "", ...itemList] = splitValue(gradValue, { delimiter: "," });
			const regDimension = IS_CONIC.test(type) ? REG_DIM_CONIC : REG_DIM_NON_CONIC;
			let colorStop = "";
			if (regDimension.test(lineOrColorStop)) {
				const itemColor = lineOrColorStop.replace(regDimension, "");
				if (isColor(itemColor, { format: "specifiedValue" })) {
					const resolvedColor = resolveColor(itemColor, opt);
					colorStop = lineOrColorStop.replace(itemColor, resolvedColor);
				}
			} else if (isColor(lineOrColorStop, { format: "specifiedValue" })) colorStop = resolveColor(lineOrColorStop, opt);
			if (colorStop) {
				itemList.unshift(colorStop);
				const { colorStops, valid } = validateColorStopList(itemList, type, opt);
				if (valid) {
					const res = {
						value,
						type,
						colorStopList: colorStops
					};
					setCache(cacheKey, res);
					return res;
				}
			} else if (itemList.length > 1) {
				const { line: gradientLine, valid: validLine } = validateGradientLine(lineOrColorStop, type);
				const { colorStops, valid: validColorStops } = validateColorStopList(itemList, type, opt);
				if (validLine && validColorStops) {
					const res = {
						value,
						type,
						gradientLine,
						colorStopList: colorStops
					};
					setCache(cacheKey, res);
					return res;
				}
			}
		}
		setCache(cacheKey, null);
		return null;
	}
	return null;
};
/**
* resolve CSS gradient
* @param value - CSS value
* @param [opt] - options
* @returns result
*/
var resolveGradient = (value, opt = {}) => {
	const { format = VAL_COMP } = opt;
	const gradient = parseGradient(value, opt);
	if (gradient) {
		const { type = "", gradientLine = "", colorStopList = [] } = gradient;
		if (type && Array.isArray(colorStopList) && colorStopList.length > 1) {
			if (gradientLine) return `${type}(${gradientLine}, ${colorStopList.join(", ")})`;
			return `${type}(${colorStopList.join(", ")})`;
		}
	}
	if (format === "specifiedValue") return "";
	return "none";
};
/**
* is CSS gradient
* @param value - CSS value
* @param [opt] - options
* @returns result
*/
var isGradient = (value, opt = {}) => {
	return parseGradient(value, opt) !== null;
};
//#endregion
export { isGradient, resolveGradient };

//# sourceMappingURL=css-gradient.js.map