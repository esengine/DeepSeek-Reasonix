import { CacheItem, NullObject, createCacheKey, getCache, setCache } from "./cache.js";
import { isString } from "./common.js";
import { ANGLE, CS_HUE_CAPT, CS_MIX, CS_RGB, CS_XYZ, FN_MIX, NONE, NUM, PCT, SYN_COLOR_TYPE, SYN_FN_COLOR, SYN_HSL, SYN_HSL_LV3, SYN_LCH, SYN_MIX, SYN_MIX_CAPT, SYN_MIX_PART, SYN_MOD, SYN_RGB_LV3, VAL_COMP, VAL_MIX, VAL_SPEC } from "./constant.js";
import { resolveColor } from "./resolve.js";
import { interpolateHue, roundToPrecision, splitValue } from "./util.js";
//#region src/js/color.ts
/**
* color
*
* Ref: CSS Color Module Level 4
*      Sample code for Color Conversions
*      https://w3c.github.io/csswg-drafts/css-color-4/#color-conversion-code
*/
var NAMESPACE = "color";
var PPTH = .001;
var HALF = .5;
var DUO = 2;
var TRIA = 3;
var QUAD = 4;
var OCT = 8;
var DEC = 10;
var DOZ = 12;
var HEX = 16;
var SEXA = 60;
var DEG_HALF = 180;
var DEG = 360;
var MAX_PCT = 100;
var MAX_RGB = 255;
var POW_SQR = 2;
var POW_CUBE = 3;
var POW_LINEAR = 2.4;
var LINEAR_COEF = 12.92;
var LINEAR_OFFSET = .055;
var LAB_L = 116;
var LAB_A = 500;
var LAB_B = 200;
var LAB_EPSILON = 216 / 24389;
var LAB_KAPPA = 24389 / 27;
var D50 = [
	.3457 / .3585,
	1,
	.2958 / .3585
];
var MATRIX_D50_TO_D65 = [
	[
		.955473421488075,
		-.02309845494876471,
		.06325924320057072
	],
	[
		-.0283697093338637,
		1.0099953980813041,
		.021041441191917323
	],
	[
		.012314014864481998,
		-.020507649298898964,
		1.330365926242124
	]
];
var MATRIX_D65_TO_D50 = [
	[
		1.0479297925449969,
		.022946870601609652,
		-.05019226628920524
	],
	[
		.02962780877005599,
		.9904344267538799,
		-.017073799063418826
	],
	[
		-.009243040646204504,
		.015055191490298152,
		.7518742814281371
	]
];
var MATRIX_L_RGB_TO_XYZ = [
	[
		506752 / 1228815,
		87881 / 245763,
		12673 / 70218
	],
	[
		87098 / 409605,
		175762 / 245763,
		12673 / 175545
	],
	[
		7918 / 409605,
		87881 / 737289,
		1001167 / 1053270
	]
];
var MATRIX_XYZ_TO_L_RGB = [
	[
		12831 / 3959,
		-329 / 214,
		-1974 / 3959
	],
	[
		-851781 / 878810,
		1648619 / 878810,
		36519 / 878810
	],
	[
		705 / 12673,
		-2585 / 12673,
		705 / 667
	]
];
var MATRIX_XYZ_TO_LMS = [
	[
		.819022437996703,
		.3619062600528904,
		-.1288737815209879
	],
	[
		.0329836539323885,
		.9292868615863434,
		.0361446663506424
	],
	[
		.0481771893596242,
		.2642395317527308,
		.6335478284694309
	]
];
var MATRIX_LMS_TO_XYZ = [
	[
		1.2268798758459243,
		-.5578149944602171,
		.2813910456659647
	],
	[
		-.0405757452148008,
		1.112286803280317,
		-.0717110580655164
	],
	[
		-.0763729366746601,
		-.4214933324022432,
		1.5869240198367816
	]
];
var MATRIX_OKLAB_TO_LMS = [
	[
		1,
		.3963377773761749,
		.2158037573099136
	],
	[
		1,
		-.1055613458156586,
		-.0638541728258133
	],
	[
		1,
		-.0894841775298119,
		-1.2914855480194092
	]
];
var MATRIX_LMS_TO_OKLAB = [
	[
		.210454268309314,
		.7936177747023054,
		-.0040720430116193
	],
	[
		1.9779985324311684,
		-2.42859224204858,
		.450593709617411
	],
	[
		.0259040424655478,
		.7827717124575296,
		-.8086757549230774
	]
];
var MATRIX_P3_TO_XYZ = [
	[
		608311 / 1250200,
		189793 / 714400,
		198249 / 1000160
	],
	[
		35783 / 156275,
		247089 / 357200,
		198249 / 2500400
	],
	[
		0 / 1,
		32229 / 714400,
		5220557 / 5000800
	]
];
var MATRIX_REC2020_TO_XYZ = [
	[
		63426534 / 99577255,
		20160776 / 139408157,
		47086771 / 278816314
	],
	[
		26158966 / 99577255,
		472592308 / 697040785,
		8267143 / 139408157
	],
	[
		0 / 1,
		19567812 / 697040785,
		295819943 / 278816314
	]
];
var MATRIX_A98_TO_XYZ = [
	[
		573536 / 994567,
		263643 / 1420810,
		187206 / 994567
	],
	[
		591459 / 1989134,
		6239551 / 9945670,
		374412 / 4972835
	],
	[
		53769 / 1989134,
		351524 / 4972835,
		4929758 / 4972835
	]
];
var MATRIX_PROPHOTO_TO_XYZ_D50 = [
	[
		.7977666449006423,
		.13518129740053308,
		.0313477341283922
	],
	[
		.2880748288194013,
		.711835234241873,
		8993693872564e-17
	],
	[
		0,
		0,
		.8251046025104602
	]
];
var REG_COLOR = new RegExp(`^(?:${SYN_COLOR_TYPE})$`);
var REG_CS_HUE = new RegExp(`^${CS_HUE_CAPT}$`);
var REG_CS_XYZ = /^xyz(?:-d(?:50|65))?$/;
var REG_CURRENT = /^currentColor$/i;
var REG_FN_COLOR = new RegExp(`^color\\(\\s*(${SYN_FN_COLOR})\\s*\\)$`);
var REG_HSL = new RegExp(`^hsla?\\(\\s*(${SYN_HSL}|${SYN_HSL_LV3})\\s*\\)$`);
var REG_HWB = new RegExp(`^hwb\\(\\s*(${SYN_HSL})\\s*\\)$`);
var REG_LAB = new RegExp(`^lab\\(\\s*(${SYN_MOD})\\s*\\)$`);
var REG_LCH = new RegExp(`^lch\\(\\s*(${SYN_LCH})\\s*\\)$`);
var REG_MIX = new RegExp(`^${SYN_MIX}$`);
var REG_MIX_CAPT = new RegExp(`^${SYN_MIX_CAPT}$`);
var REG_MIX_NEST = new RegExp(`${SYN_MIX}`, "g");
var REG_OKLAB = new RegExp(`^oklab\\(\\s*(${SYN_MOD})\\s*\\)$`);
var REG_OKLCH = new RegExp(`^oklch\\(\\s*(${SYN_LCH})\\s*\\)$`);
var REG_SPEC = /^(?:specifi|comput)edValue$/;
var REG_ANGLE_TO_DEG = new RegExp(`^(${NUM})(${ANGLE})?$`);
var REG_PARSE_RGB = new RegExp(`^rgba?\\(\\s*(${SYN_MOD}|${SYN_RGB_LV3})\\s*\\)$`);
var REG_MIX_CS_RGB_XYZ = new RegExp(`^(?:${CS_RGB}|${CS_XYZ})$`);
var REG_MIX_IN_CS = new RegExp(`in\\s+(${CS_MIX})`);
var REG_MIX_START = new RegExp(`^color-mix\\(\\s*in\\s+(${CS_MIX})\\s*,`);
var REG_MIX_COLOR_PART = new RegExp(`^(${SYN_COLOR_TYPE})(?:\\s+(${PCT}))?$`);
/**
* named colors
*/
var NAMED_COLORS = {
	aliceblue: [
		240,
		248,
		255
	],
	antiquewhite: [
		250,
		235,
		215
	],
	aqua: [
		0,
		255,
		255
	],
	aquamarine: [
		127,
		255,
		212
	],
	azure: [
		240,
		255,
		255
	],
	beige: [
		245,
		245,
		220
	],
	bisque: [
		255,
		228,
		196
	],
	black: [
		0,
		0,
		0
	],
	blanchedalmond: [
		255,
		235,
		205
	],
	blue: [
		0,
		0,
		255
	],
	blueviolet: [
		138,
		43,
		226
	],
	brown: [
		165,
		42,
		42
	],
	burlywood: [
		222,
		184,
		135
	],
	cadetblue: [
		95,
		158,
		160
	],
	chartreuse: [
		127,
		255,
		0
	],
	chocolate: [
		210,
		105,
		30
	],
	coral: [
		255,
		127,
		80
	],
	cornflowerblue: [
		100,
		149,
		237
	],
	cornsilk: [
		255,
		248,
		220
	],
	crimson: [
		220,
		20,
		60
	],
	cyan: [
		0,
		255,
		255
	],
	darkblue: [
		0,
		0,
		139
	],
	darkcyan: [
		0,
		139,
		139
	],
	darkgoldenrod: [
		184,
		134,
		11
	],
	darkgray: [
		169,
		169,
		169
	],
	darkgreen: [
		0,
		100,
		0
	],
	darkgrey: [
		169,
		169,
		169
	],
	darkkhaki: [
		189,
		183,
		107
	],
	darkmagenta: [
		139,
		0,
		139
	],
	darkolivegreen: [
		85,
		107,
		47
	],
	darkorange: [
		255,
		140,
		0
	],
	darkorchid: [
		153,
		50,
		204
	],
	darkred: [
		139,
		0,
		0
	],
	darksalmon: [
		233,
		150,
		122
	],
	darkseagreen: [
		143,
		188,
		143
	],
	darkslateblue: [
		72,
		61,
		139
	],
	darkslategray: [
		47,
		79,
		79
	],
	darkslategrey: [
		47,
		79,
		79
	],
	darkturquoise: [
		0,
		206,
		209
	],
	darkviolet: [
		148,
		0,
		211
	],
	deeppink: [
		255,
		20,
		147
	],
	deepskyblue: [
		0,
		191,
		255
	],
	dimgray: [
		105,
		105,
		105
	],
	dimgrey: [
		105,
		105,
		105
	],
	dodgerblue: [
		30,
		144,
		255
	],
	firebrick: [
		178,
		34,
		34
	],
	floralwhite: [
		255,
		250,
		240
	],
	forestgreen: [
		34,
		139,
		34
	],
	fuchsia: [
		255,
		0,
		255
	],
	gainsboro: [
		220,
		220,
		220
	],
	ghostwhite: [
		248,
		248,
		255
	],
	gold: [
		255,
		215,
		0
	],
	goldenrod: [
		218,
		165,
		32
	],
	gray: [
		128,
		128,
		128
	],
	green: [
		0,
		128,
		0
	],
	greenyellow: [
		173,
		255,
		47
	],
	grey: [
		128,
		128,
		128
	],
	honeydew: [
		240,
		255,
		240
	],
	hotpink: [
		255,
		105,
		180
	],
	indianred: [
		205,
		92,
		92
	],
	indigo: [
		75,
		0,
		130
	],
	ivory: [
		255,
		255,
		240
	],
	khaki: [
		240,
		230,
		140
	],
	lavender: [
		230,
		230,
		250
	],
	lavenderblush: [
		255,
		240,
		245
	],
	lawngreen: [
		124,
		252,
		0
	],
	lemonchiffon: [
		255,
		250,
		205
	],
	lightblue: [
		173,
		216,
		230
	],
	lightcoral: [
		240,
		128,
		128
	],
	lightcyan: [
		224,
		255,
		255
	],
	lightgoldenrodyellow: [
		250,
		250,
		210
	],
	lightgray: [
		211,
		211,
		211
	],
	lightgreen: [
		144,
		238,
		144
	],
	lightgrey: [
		211,
		211,
		211
	],
	lightpink: [
		255,
		182,
		193
	],
	lightsalmon: [
		255,
		160,
		122
	],
	lightseagreen: [
		32,
		178,
		170
	],
	lightskyblue: [
		135,
		206,
		250
	],
	lightslategray: [
		119,
		136,
		153
	],
	lightslategrey: [
		119,
		136,
		153
	],
	lightsteelblue: [
		176,
		196,
		222
	],
	lightyellow: [
		255,
		255,
		224
	],
	lime: [
		0,
		255,
		0
	],
	limegreen: [
		50,
		205,
		50
	],
	linen: [
		250,
		240,
		230
	],
	magenta: [
		255,
		0,
		255
	],
	maroon: [
		128,
		0,
		0
	],
	mediumaquamarine: [
		102,
		205,
		170
	],
	mediumblue: [
		0,
		0,
		205
	],
	mediumorchid: [
		186,
		85,
		211
	],
	mediumpurple: [
		147,
		112,
		219
	],
	mediumseagreen: [
		60,
		179,
		113
	],
	mediumslateblue: [
		123,
		104,
		238
	],
	mediumspringgreen: [
		0,
		250,
		154
	],
	mediumturquoise: [
		72,
		209,
		204
	],
	mediumvioletred: [
		199,
		21,
		133
	],
	midnightblue: [
		25,
		25,
		112
	],
	mintcream: [
		245,
		255,
		250
	],
	mistyrose: [
		255,
		228,
		225
	],
	moccasin: [
		255,
		228,
		181
	],
	navajowhite: [
		255,
		222,
		173
	],
	navy: [
		0,
		0,
		128
	],
	oldlace: [
		253,
		245,
		230
	],
	olive: [
		128,
		128,
		0
	],
	olivedrab: [
		107,
		142,
		35
	],
	orange: [
		255,
		165,
		0
	],
	orangered: [
		255,
		69,
		0
	],
	orchid: [
		218,
		112,
		214
	],
	palegoldenrod: [
		238,
		232,
		170
	],
	palegreen: [
		152,
		251,
		152
	],
	paleturquoise: [
		175,
		238,
		238
	],
	palevioletred: [
		219,
		112,
		147
	],
	papayawhip: [
		255,
		239,
		213
	],
	peachpuff: [
		255,
		218,
		185
	],
	peru: [
		205,
		133,
		63
	],
	pink: [
		255,
		192,
		203
	],
	plum: [
		221,
		160,
		221
	],
	powderblue: [
		176,
		224,
		230
	],
	purple: [
		128,
		0,
		128
	],
	rebeccapurple: [
		102,
		51,
		153
	],
	red: [
		255,
		0,
		0
	],
	rosybrown: [
		188,
		143,
		143
	],
	royalblue: [
		65,
		105,
		225
	],
	saddlebrown: [
		139,
		69,
		19
	],
	salmon: [
		250,
		128,
		114
	],
	sandybrown: [
		244,
		164,
		96
	],
	seagreen: [
		46,
		139,
		87
	],
	seashell: [
		255,
		245,
		238
	],
	sienna: [
		160,
		82,
		45
	],
	silver: [
		192,
		192,
		192
	],
	skyblue: [
		135,
		206,
		235
	],
	slateblue: [
		106,
		90,
		205
	],
	slategray: [
		112,
		128,
		144
	],
	slategrey: [
		112,
		128,
		144
	],
	snow: [
		255,
		250,
		250
	],
	springgreen: [
		0,
		255,
		127
	],
	steelblue: [
		70,
		130,
		180
	],
	tan: [
		210,
		180,
		140
	],
	teal: [
		0,
		128,
		128
	],
	thistle: [
		216,
		191,
		216
	],
	tomato: [
		255,
		99,
		71
	],
	turquoise: [
		64,
		224,
		208
	],
	violet: [
		238,
		130,
		238
	],
	wheat: [
		245,
		222,
		179
	],
	white: [
		255,
		255,
		255
	],
	whitesmoke: [
		245,
		245,
		245
	],
	yellow: [
		255,
		255,
		0
	],
	yellowgreen: [
		154,
		205,
		50
	]
};
/**
* cache invalid color value
* @param key - cache key
* @param nullable - is nullable
* @returns cached value
*/
var cacheInvalidColorValue = (cacheKey, format, nullable = false) => {
	if (format === "specifiedValue") {
		const res = "";
		setCache(cacheKey, res);
		return res;
	}
	if (nullable) {
		setCache(cacheKey, null);
		return new NullObject();
	}
	const res = [
		"rgb",
		0,
		0,
		0,
		0
	];
	setCache(cacheKey, res);
	return res;
};
/**
* resolve invalid color value
* @param format - output format
* @param nullable - is nullable
* @returns resolved value
*/
var resolveInvalidColorValue = (format, nullable = false) => {
	switch (format) {
		case "hsl":
		case "hwb":
		case VAL_MIX: return new NullObject();
		case VAL_SPEC: return "";
		default:
			if (nullable) return new NullObject();
			return [
				"rgb",
				0,
				0,
				0,
				0
			];
	}
};
/**
* validate color components
* @param arr - color components
* @param [opt] - options
* @param [opt.alpha] - alpha channel
* @param [opt.minLength] - min length
* @param [opt.maxLength] - max length
* @param [opt.minRange] - min range
* @param [opt.maxRange] - max range
* @param [opt.validateRange] - validate range
* @returns result - validated color components
*/
var validateColorComponents = (arr, opt = {}) => {
	if (!Array.isArray(arr)) throw new TypeError(`${arr} is not an array.`);
	const { alpha = false, minLength = TRIA, maxLength = QUAD, minRange = 0, maxRange = 1, validateRange = true } = opt;
	if (!Number.isFinite(minLength)) throw new TypeError(`${minLength} is not a number.`);
	if (!Number.isFinite(maxLength)) throw new TypeError(`${maxLength} is not a number.`);
	if (!Number.isFinite(minRange)) throw new TypeError(`${minRange} is not a number.`);
	if (!Number.isFinite(maxRange)) throw new TypeError(`${maxRange} is not a number.`);
	const l = arr.length;
	if (l < minLength || l > maxLength) throw new Error(`Unexpected array length ${l}.`);
	let i = 0;
	while (i < l) {
		const v = arr[i];
		if (!Number.isFinite(v)) throw new TypeError(`${v} is not a number.`);
		else if (i < TRIA && validateRange && (v < minRange || v > maxRange)) throw new RangeError(`${v} is not between ${minRange} and ${maxRange}.`);
		else if (i === TRIA && (v < 0 || v > 1)) throw new RangeError(`${v} is not between 0 and 1.`);
		i++;
	}
	if (alpha && l === TRIA) arr.push(1);
	return arr;
};
/**
* transform matrix
* @param mtx - 3 * 3 matrix
* @param vct - vector
* @param [skip] - skip validate
* @returns TriColorChannels - [p1, p2, p3]
*/
var transformMatrix = (mtx, vct, skip = false) => {
	if (!Array.isArray(mtx)) throw new TypeError(`${mtx} is not an array.`);
	else if (mtx.length !== TRIA) throw new Error(`Unexpected array length ${mtx.length}.`);
	else if (!skip) for (let i of mtx) i = validateColorComponents(i, {
		maxLength: TRIA,
		validateRange: false
	});
	const [[r1c1, r1c2, r1c3], [r2c1, r2c2, r2c3], [r3c1, r3c2, r3c3]] = mtx;
	let v1, v2, v3;
	if (skip) [v1, v2, v3] = vct;
	else [v1, v2, v3] = validateColorComponents(vct, {
		maxLength: TRIA,
		validateRange: false
	});
	return [
		r1c1 * v1 + r1c2 * v2 + r1c3 * v3,
		r2c1 * v1 + r2c2 * v2 + r2c3 * v3,
		r3c1 * v1 + r3c2 * v2 + r3c3 * v3
	];
};
/**
* normalize color components
* @param colorA - color components [v1, v2, v3, v4]
* @param colorB - color components [v1, v2, v3, v4]
* @param [skip] - skip validate
* @returns result - [colorA, colorB]
*/
var normalizeColorComponents = (colorA, colorB, skip = false) => {
	if (!Array.isArray(colorA)) throw new TypeError(`${colorA} is not an array.`);
	else if (colorA.length !== QUAD) throw new Error(`Unexpected array length ${colorA.length}.`);
	if (!Array.isArray(colorB)) throw new TypeError(`${colorB} is not an array.`);
	else if (colorB.length !== QUAD) throw new Error(`Unexpected array length ${colorB.length}.`);
	let i = 0;
	while (i < QUAD) {
		if (colorA[i] === "none" && colorB[i] === "none") {
			colorA[i] = 0;
			colorB[i] = 0;
		} else if (colorA[i] === "none") colorA[i] = colorB[i];
		else if (colorB[i] === "none") colorB[i] = colorA[i];
		i++;
	}
	if (skip) return [colorA, colorB];
	return [validateColorComponents(colorA, {
		minLength: QUAD,
		validateRange: false
	}), validateColorComponents(colorB, {
		minLength: QUAD,
		validateRange: false
	})];
};
/**
* number to hex string
* @param value - numeric value
* @returns hex string
*/
var numberToHexString = (value) => {
	if (!Number.isFinite(value)) throw new TypeError(`${value} is not a number.`);
	else {
		value = Math.round(value);
		if (value < 0 || value > MAX_RGB) throw new RangeError(`${value} is not between 0 and ${MAX_RGB}.`);
	}
	let hex = value.toString(HEX);
	if (hex.length === 1) hex = `0${hex}`;
	return hex;
};
/**
* angle to deg
* @param angle
* @returns deg: 0..360
*/
var angleToDeg = (angle) => {
	if (isString(angle)) angle = angle.trim();
	else throw new TypeError(`${angle} is not a string.`);
	const GRAD = DEG / 400;
	const RAD = DEG / (Math.PI * DUO);
	if (!REG_ANGLE_TO_DEG.test(angle)) throw new SyntaxError(`Invalid property value: ${angle}`);
	const [, value, unit] = angle.match(REG_ANGLE_TO_DEG);
	let deg;
	switch (unit) {
		case "grad":
			deg = parseFloat(value) * GRAD;
			break;
		case "rad":
			deg = parseFloat(value) * RAD;
			break;
		case "turn":
			deg = parseFloat(value) * DEG;
			break;
		default: deg = parseFloat(value);
	}
	deg %= DEG;
	if (deg < 0) deg += DEG;
	else if (Object.is(deg, -0)) deg = 0;
	return deg;
};
/**
* parse alpha
* @param [alpha] - alpha value
* @returns alpha: 0..1
*/
var parseAlpha = (alpha = "") => {
	if (isString(alpha)) {
		alpha = alpha.trim();
		if (!alpha) alpha = "1";
		else if (alpha === "none") alpha = "0";
		else {
			let a;
			if (alpha.endsWith("%")) a = parseFloat(alpha) / MAX_PCT;
			else a = parseFloat(alpha);
			if (!Number.isFinite(a)) throw new TypeError(`${a} is not a finite number.`);
			if (a < PPTH) alpha = "0";
			else if (a > 1) alpha = "1";
			else alpha = a.toFixed(TRIA);
		}
	} else alpha = "1";
	return parseFloat(alpha);
};
/**
* parse hex alpha
* @param value - alpha value in hex string
* @returns alpha: 0..1
*/
var parseHexAlpha = (value) => {
	if (isString(value)) {
		if (value === "") throw new SyntaxError("Invalid property value: (empty string)");
		value = value.trim();
	} else throw new TypeError(`${value} is not a string.`);
	let alpha = parseInt(value, HEX);
	if (alpha <= 0) return 0;
	if (alpha >= MAX_RGB) return 1;
	const alphaMap = /* @__PURE__ */ new Map();
	for (let i = 1; i < MAX_PCT; i++) alphaMap.set(Math.round(i * MAX_RGB / MAX_PCT), i);
	if (alphaMap.has(alpha)) alpha = alphaMap.get(alpha) / MAX_PCT;
	else alpha = Math.round(alpha / MAX_RGB / PPTH) * PPTH;
	return parseFloat(alpha.toFixed(TRIA));
};
/**
* transform rgb to linear rgb
* @param rgb - [r, g, b] r|g|b: 0..255
* @param [skip] - skip validate
* @returns TriColorChannels - [r, g, b] r|g|b: 0..1
*/
var transformRgbToLinearRgb = (rgb, skip = false) => {
	let rr, gg, bb;
	if (skip) [rr, gg, bb] = rgb;
	else [rr, gg, bb] = validateColorComponents(rgb, {
		maxLength: TRIA,
		maxRange: MAX_RGB
	});
	let r = rr / MAX_RGB;
	let g = gg / MAX_RGB;
	let b = bb / MAX_RGB;
	const COND_POW = .04045;
	if (r > COND_POW) r = Math.pow((r + LINEAR_OFFSET) / (1 + LINEAR_OFFSET), POW_LINEAR);
	else r /= LINEAR_COEF;
	if (g > COND_POW) g = Math.pow((g + LINEAR_OFFSET) / (1 + LINEAR_OFFSET), POW_LINEAR);
	else g /= LINEAR_COEF;
	if (b > COND_POW) b = Math.pow((b + LINEAR_OFFSET) / (1 + LINEAR_OFFSET), POW_LINEAR);
	else b /= LINEAR_COEF;
	return [
		r,
		g,
		b
	];
};
/**
* transform rgb to xyz
* @param rgb - [r, g, b] r|g|b: 0..255
* @param [skip] - skip validate
* @returns TriColorChannels - [x, y, z]
*/
var transformRgbToXyz = (rgb, skip = false) => {
	if (!skip) rgb = validateColorComponents(rgb, {
		maxLength: TRIA,
		maxRange: MAX_RGB
	});
	rgb = transformRgbToLinearRgb(rgb, true);
	return transformMatrix(MATRIX_L_RGB_TO_XYZ, rgb, true);
};
/**
* transform linear rgb to rgb
* @param rgb - [r, g, b] r|g|b: 0..1
* @param [round] - round result
* @returns TriColorChannels - [r, g, b] r|g|b: 0..255
*/
var transformLinearRgbToRgb = (rgb, round = false) => {
	let [r, g, b] = validateColorComponents(rgb, { maxLength: TRIA });
	const COND_POW = 809 / 258400;
	if (r > COND_POW) r = Math.pow(r, 1 / POW_LINEAR) * (1 + LINEAR_OFFSET) - LINEAR_OFFSET;
	else r *= LINEAR_COEF;
	r *= MAX_RGB;
	if (g > COND_POW) g = Math.pow(g, 1 / POW_LINEAR) * (1 + LINEAR_OFFSET) - LINEAR_OFFSET;
	else g *= LINEAR_COEF;
	g *= MAX_RGB;
	if (b > COND_POW) b = Math.pow(b, 1 / POW_LINEAR) * (1 + LINEAR_OFFSET) - LINEAR_OFFSET;
	else b *= LINEAR_COEF;
	b *= MAX_RGB;
	return [
		round ? Math.round(r) : r,
		round ? Math.round(g) : g,
		round ? Math.round(b) : b
	];
};
/**
* transform xyz to rgb
* @param xyz - [x, y, z]
* @param [skip] - skip validate
* @returns TriColorChannels - [r, g, b] r|g|b: 0..255
*/
var transformXyzToRgb = (xyz, skip = false) => {
	if (!skip) xyz = validateColorComponents(xyz, {
		maxLength: TRIA,
		validateRange: false
	});
	let [r, g, b] = transformMatrix(MATRIX_XYZ_TO_L_RGB, xyz, true);
	[r, g, b] = transformLinearRgbToRgb([
		Math.min(Math.max(r, 0), 1),
		Math.min(Math.max(g, 0), 1),
		Math.min(Math.max(b, 0), 1)
	], true);
	return [
		r,
		g,
		b
	];
};
/**
* transform xyz to hsl
* @param xyz - [x, y, z]
* @param [skip] - skip validate
* @returns TriColorChannels - [h, s, l]
*/
var transformXyzToHsl = (xyz, skip = false) => {
	const [rr, gg, bb] = transformXyzToRgb(xyz, skip);
	const r = rr / MAX_RGB;
	const g = gg / MAX_RGB;
	const b = bb / MAX_RGB;
	const max = Math.max(r, g, b);
	const min = Math.min(r, g, b);
	const d = max - min;
	const l = (max + min) * HALF * MAX_PCT;
	let h, s;
	if (Math.round(l) === 0 || Math.round(l) === MAX_PCT) {
		h = 0;
		s = 0;
	} else {
		s = d / (1 - Math.abs(max + min - 1)) * MAX_PCT;
		if (s === 0) h = 0;
		else {
			switch (max) {
				case r:
					h = (g - b) / d;
					break;
				case g:
					h = (b - r) / d + DUO;
					break;
				case b:
				default:
					h = (r - g) / d + QUAD;
					break;
			}
			h = h * SEXA % DEG;
			if (h < 0) h += DEG;
		}
	}
	return [
		h,
		s,
		l
	];
};
/**
* transform xyz to hwb
* @param xyz - [x, y, z]
* @param [skip] - skip validate
* @returns TriColorChannels - [h, w, b]
*/
var transformXyzToHwb = (xyz, skip = false) => {
	const [r, g, b] = transformXyzToRgb(xyz, skip);
	const wh = Math.min(r, g, b) / MAX_RGB;
	const bk = 1 - Math.max(r, g, b) / MAX_RGB;
	let h;
	if (wh + bk === 1) h = 0;
	else [h] = transformXyzToHsl(xyz);
	return [
		h,
		wh * MAX_PCT,
		bk * MAX_PCT
	];
};
/**
* transform xyz to oklab
* @param xyz - [x, y, z]
* @param [skip] - skip validate
* @returns TriColorChannels - [l, a, b]
*/
var transformXyzToOklab = (xyz, skip = false) => {
	if (!skip) xyz = validateColorComponents(xyz, {
		maxLength: TRIA,
		validateRange: false
	});
	let [l, a, b] = transformMatrix(MATRIX_LMS_TO_OKLAB, transformMatrix(MATRIX_XYZ_TO_LMS, xyz, true).map((c) => Math.cbrt(c)), true);
	l = Math.min(Math.max(l, 0), 1);
	const lPct = Math.round(parseFloat(l.toFixed(QUAD)) * MAX_PCT);
	if (lPct === 0 || lPct === MAX_PCT) {
		a = 0;
		b = 0;
	}
	return [
		l,
		a,
		b
	];
};
/**
* transform xyz to oklch
* @param xyz - [x, y, z]
* @param [skip] - skip validate
* @returns TriColorChannels - [l, c, h]
*/
var transformXyzToOklch = (xyz, skip = false) => {
	const [l, a, b] = transformXyzToOklab(xyz, skip);
	let c, h;
	const lPct = Math.round(parseFloat(l.toFixed(QUAD)) * MAX_PCT);
	if (lPct === 0 || lPct === MAX_PCT) {
		c = 0;
		h = 0;
	} else {
		c = Math.max(Math.sqrt(Math.pow(a, POW_SQR) + Math.pow(b, POW_SQR)), 0);
		if (parseFloat(c.toFixed(QUAD)) === 0) h = 0;
		else {
			h = Math.atan2(b, a) * DEG_HALF / Math.PI;
			if (h < 0) h += DEG;
		}
	}
	return [
		l,
		c,
		h
	];
};
/**
* transform xyz D50 to rgb
* @param xyz - [x, y, z]
* @param [skip] - skip validate
* @returns TriColorChannels - [r, g, b] r|g|b: 0..255
*/
var transformXyzD50ToRgb = (xyz, skip = false) => {
	if (!skip) xyz = validateColorComponents(xyz, {
		maxLength: TRIA,
		validateRange: false
	});
	return transformXyzToRgb(transformMatrix(MATRIX_D50_TO_D65, xyz, true), true);
};
/**
* transform xyz-d50 to lab
* @param xyz - [x, y, z]
* @param [skip] - skip validate
* @returns TriColorChannels - [l, a, b]
*/
var transformXyzD50ToLab = (xyz, skip = false) => {
	if (!skip) xyz = validateColorComponents(xyz, {
		maxLength: TRIA,
		validateRange: false
	});
	const [f0, f1, f2] = xyz.map((val, i) => val / D50[i]).map((val) => val > LAB_EPSILON ? Math.cbrt(val) : (val * LAB_KAPPA + HEX) / LAB_L);
	const l = Math.min(Math.max(LAB_L * f1 - HEX, 0), MAX_PCT);
	let a, b;
	if (l === 0 || l === MAX_PCT) {
		a = 0;
		b = 0;
	} else {
		a = (f0 - f1) * LAB_A;
		b = (f1 - f2) * LAB_B;
	}
	return [
		l,
		a,
		b
	];
};
/**
* transform xyz-d50 to lch
* @param xyz - [x, y, z]
* @param [skip] - skip validate
* @returns TriColorChannels - [l, c, h]
*/
var transformXyzD50ToLch = (xyz, skip = false) => {
	const [l, a, b] = transformXyzD50ToLab(xyz, skip);
	let c, h;
	if (l === 0 || l === MAX_PCT) {
		c = 0;
		h = 0;
	} else {
		c = Math.max(Math.sqrt(Math.pow(a, POW_SQR) + Math.pow(b, POW_SQR)), 0);
		h = Math.atan2(b, a) * DEG_HALF / Math.PI;
		if (h < 0) h += DEG;
	}
	return [
		l,
		c,
		h
	];
};
/**
* convert rgb to hex color
* @param rgb - [r, g, b, alpha] r|g|b: 0..255 alpha: 0..1
* @returns hex color
*/
var convertRgbToHex = (rgb) => {
	const [r, g, b, alpha] = validateColorComponents(rgb, {
		alpha: true,
		maxRange: MAX_RGB
	});
	const rr = numberToHexString(r);
	const gg = numberToHexString(g);
	const bb = numberToHexString(b);
	const aa = numberToHexString(alpha * MAX_RGB);
	let hex;
	if (aa === "ff") hex = `#${rr}${gg}${bb}`;
	else hex = `#${rr}${gg}${bb}${aa}`;
	return hex;
};
/**
* convert hex color to rgb
* @param value - hex color value
* @returns ColorChannels - [r, g, b, alpha] r|g|b: 0..255 alpha: 0..1
*/
var convertHexToRgb = (value) => {
	if (isString(value)) value = value.toLowerCase().trim();
	else throw new TypeError(`${value} is not a string.`);
	if (!(/^#[\da-f]{6}$/.test(value) || /^#[\da-f]{3}$/.test(value) || /^#[\da-f]{8}$/.test(value) || /^#[\da-f]{4}$/.test(value))) throw new SyntaxError(`Invalid property value: ${value}`);
	const arr = [];
	if (/^#[\da-f]{3}$/.test(value)) {
		const [, r, g, b] = value.match(/^#([\da-f])([\da-f])([\da-f])$/);
		arr.push(parseInt(`${r}${r}`, HEX), parseInt(`${g}${g}`, HEX), parseInt(`${b}${b}`, HEX), 1);
	} else if (/^#[\da-f]{4}$/.test(value)) {
		const [, r, g, b, alpha] = value.match(/^#([\da-f])([\da-f])([\da-f])([\da-f])$/);
		arr.push(parseInt(`${r}${r}`, HEX), parseInt(`${g}${g}`, HEX), parseInt(`${b}${b}`, HEX), parseHexAlpha(`${alpha}${alpha}`));
	} else if (/^#[\da-f]{8}$/.test(value)) {
		const [, r, g, b, alpha] = value.match(/^#([\da-f]{2})([\da-f]{2})([\da-f]{2})([\da-f]{2})$/);
		arr.push(parseInt(r, HEX), parseInt(g, HEX), parseInt(b, HEX), parseHexAlpha(alpha));
	} else {
		const [, r, g, b] = value.match(/^#([\da-f]{2})([\da-f]{2})([\da-f]{2})$/);
		arr.push(parseInt(r, HEX), parseInt(g, HEX), parseInt(b, HEX), 1);
	}
	return arr;
};
/**
* convert hex color to linear rgb
* @param value - hex color value
* @returns ColorChannels - [r, g, b, alpha] r|g|b|alpha: 0..1
*/
var convertHexToLinearRgb = (value) => {
	const [rr, gg, bb, alpha] = convertHexToRgb(value);
	const [r, g, b] = transformRgbToLinearRgb([
		rr,
		gg,
		bb
	], true);
	return [
		r,
		g,
		b,
		alpha
	];
};
/**
* convert hex color to xyz
* @param value - hex color value
* @returns ColorChannels - [x, y, z, alpha]
*/
var convertHexToXyz = (value) => {
	const [r, g, b, alpha] = convertHexToLinearRgb(value);
	const [x, y, z] = transformMatrix(MATRIX_L_RGB_TO_XYZ, [
		r,
		g,
		b
	], true);
	return [
		x,
		y,
		z,
		alpha
	];
};
/**
* parse rgb()
* @param value - rgb color value
* @param [opt] - options
* @returns parsed color - ['rgb', r, g, b, alpha], '(empty)', NullObject
*/
var parseRgb = (value, opt = {}) => {
	if (isString(value)) value = value.toLowerCase().trim();
	else throw new TypeError(`${value} is not a string.`);
	const { format = "", nullable = false } = opt;
	if (!REG_PARSE_RGB.test(value)) {
		const res = resolveInvalidColorValue(format, nullable);
		if (res instanceof NullObject) return res;
		if (isString(res)) return res;
		return res;
	}
	const [, val] = value.match(REG_PARSE_RGB);
	const [v1, v2, v3, v4 = ""] = val.match(/[^\s,/]+/g);
	let r, g, b;
	if (v1 === "none") r = 0;
	else {
		if (v1.endsWith("%")) r = parseFloat(v1) * MAX_RGB / MAX_PCT;
		else r = parseFloat(v1);
		r = Math.min(Math.max(roundToPrecision(r, OCT), 0), MAX_RGB);
	}
	if (v2 === "none") g = 0;
	else {
		if (v2.endsWith("%")) g = parseFloat(v2) * MAX_RGB / MAX_PCT;
		else g = parseFloat(v2);
		g = Math.min(Math.max(roundToPrecision(g, OCT), 0), MAX_RGB);
	}
	if (v3 === "none") b = 0;
	else {
		if (v3.endsWith("%")) b = parseFloat(v3) * MAX_RGB / MAX_PCT;
		else b = parseFloat(v3);
		b = Math.min(Math.max(roundToPrecision(b, OCT), 0), MAX_RGB);
	}
	const alpha = parseAlpha(v4);
	return [
		"rgb",
		r,
		g,
		b,
		format === "mixValue" && v4 === "none" ? NONE : alpha
	];
};
/**
* parse hsl()
* @param value - hsl color value
* @param [opt] - options
* @returns parsed color - ['rgb', r, g, b, alpha], '(empty)', NullObject
*/
var parseHsl = (value, opt = {}) => {
	if (isString(value)) value = value.trim();
	else throw new TypeError(`${value} is not a string.`);
	const { format = "", nullable = false } = opt;
	if (!REG_HSL.test(value)) {
		const res = resolveInvalidColorValue(format, nullable);
		if (res instanceof NullObject) return res;
		if (isString(res)) return res;
		return res;
	}
	const [, val] = value.match(REG_HSL);
	const [v1, v2, v3, v4 = ""] = val.match(/[^\s,/]+/g);
	let h, s, l;
	if (v1 === "none") h = 0;
	else h = angleToDeg(v1);
	if (v2 === "none") s = 0;
	else s = Math.min(Math.max(parseFloat(v2), 0), MAX_PCT);
	if (v3 === "none") l = 0;
	else l = Math.min(Math.max(parseFloat(v3), 0), MAX_PCT);
	const alpha = parseAlpha(v4);
	if (format === "hsl") return [
		format,
		v1 === "none" ? v1 : h,
		v2 === "none" ? v2 : s,
		v3 === "none" ? v3 : l,
		v4 === "none" ? v4 : alpha
	];
	h = h / DEG * DOZ;
	l /= MAX_PCT;
	const sa = s / MAX_PCT * Math.min(l, 1 - l);
	const rk = h % DOZ;
	const gk = (8 + h) % DOZ;
	const bk = (4 + h) % DOZ;
	const r = l - sa * Math.max(-1, Math.min(rk - TRIA, TRIA ** POW_SQR - rk, 1));
	const g = l - sa * Math.max(-1, Math.min(gk - TRIA, TRIA ** POW_SQR - gk, 1));
	const b = l - sa * Math.max(-1, Math.min(bk - TRIA, TRIA ** POW_SQR - bk, 1));
	return [
		"rgb",
		Math.min(Math.max(roundToPrecision(r * MAX_RGB, OCT), 0), MAX_RGB),
		Math.min(Math.max(roundToPrecision(g * MAX_RGB, OCT), 0), MAX_RGB),
		Math.min(Math.max(roundToPrecision(b * MAX_RGB, OCT), 0), MAX_RGB),
		alpha
	];
};
/**
* parse hwb()
* @param value - hwb color value
* @param [opt] - options
* @returns parsed color - ['rgb', r, g, b, alpha], '(empty)', NullObject
*/
var parseHwb = (value, opt = {}) => {
	if (isString(value)) value = value.trim();
	else throw new TypeError(`${value} is not a string.`);
	const { format = "", nullable = false } = opt;
	if (!REG_HWB.test(value)) {
		const res = resolveInvalidColorValue(format, nullable);
		if (res instanceof NullObject) return res;
		if (isString(res)) return res;
		return res;
	}
	const [, val] = value.match(REG_HWB);
	const [v1, v2, v3, v4 = ""] = val.match(/[^\s,/]+/g);
	let h, wh, bk;
	if (v1 === "none") h = 0;
	else h = angleToDeg(v1);
	if (v2 === "none") wh = 0;
	else wh = Math.min(Math.max(parseFloat(v2), 0), MAX_PCT) / MAX_PCT;
	if (v3 === "none") bk = 0;
	else bk = Math.min(Math.max(parseFloat(v3), 0), MAX_PCT) / MAX_PCT;
	const alpha = parseAlpha(v4);
	if (format === "hwb") return [
		format,
		v1 === "none" ? v1 : h,
		v2 === "none" ? v2 : wh * MAX_PCT,
		v3 === "none" ? v3 : bk * MAX_PCT,
		v4 === "none" ? v4 : alpha
	];
	if (wh + bk >= 1) {
		const v = roundToPrecision(wh / (wh + bk) * MAX_RGB, OCT);
		return [
			"rgb",
			v,
			v,
			v,
			alpha
		];
	}
	const factor = (1 - wh - bk) / MAX_RGB;
	let [, r, g, b] = parseHsl(`hsl(${h} 100 50)`);
	r = roundToPrecision((r * factor + wh) * MAX_RGB, OCT);
	g = roundToPrecision((g * factor + wh) * MAX_RGB, OCT);
	b = roundToPrecision((b * factor + wh) * MAX_RGB, OCT);
	return [
		"rgb",
		Math.min(Math.max(r, 0), MAX_RGB),
		Math.min(Math.max(g, 0), MAX_RGB),
		Math.min(Math.max(b, 0), MAX_RGB),
		alpha
	];
};
/**
* parse lab()
* @param value - lab color value
* @param [opt] - options
* @returns parsed color
*   - [xyz-d50, x, y, z, alpha], ['lab', l, a, b, alpha], '(empty)', NullObject
*/
var parseLab = (value, opt = {}) => {
	if (isString(value)) value = value.trim();
	else throw new TypeError(`${value} is not a string.`);
	const { format = "", nullable = false } = opt;
	if (!REG_LAB.test(value)) {
		const res = resolveInvalidColorValue(format, nullable);
		if (res instanceof NullObject) return res;
		if (isString(res)) return res;
		return res;
	}
	const COEF_PCT = 1.25;
	const COND_POW = 8;
	const [, val] = value.match(REG_LAB);
	const [v1, v2, v3, v4 = ""] = val.match(/[^\s,/]+/g);
	let l, a, b;
	if (v1 === "none") l = 0;
	else {
		if (v1.endsWith("%")) {
			l = parseFloat(v1);
			if (l > MAX_PCT) l = MAX_PCT;
		} else l = parseFloat(v1);
		if (l < 0) l = 0;
	}
	if (v2 === "none") a = 0;
	else a = v2.endsWith("%") ? parseFloat(v2) * COEF_PCT : parseFloat(v2);
	if (v3 === "none") b = 0;
	else b = v3.endsWith("%") ? parseFloat(v3) * COEF_PCT : parseFloat(v3);
	const alpha = parseAlpha(v4);
	if (REG_SPEC.test(format)) return [
		"lab",
		v1 === "none" ? v1 : roundToPrecision(l, HEX),
		v2 === "none" ? v2 : roundToPrecision(a, HEX),
		v3 === "none" ? v3 : roundToPrecision(b, HEX),
		v4 === "none" ? v4 : alpha
	];
	const fl = (l + HEX) / LAB_L;
	const fa = a / LAB_A + fl;
	const fb = fl - b / LAB_B;
	const powFl = Math.pow(fl, POW_CUBE);
	const powFa = Math.pow(fa, POW_CUBE);
	const powFb = Math.pow(fb, POW_CUBE);
	const [x, y, z] = [
		powFa > LAB_EPSILON ? powFa : (fa * LAB_L - HEX) / LAB_KAPPA,
		l > COND_POW ? powFl : l / LAB_KAPPA,
		powFb > LAB_EPSILON ? powFb : (fb * LAB_L - HEX) / LAB_KAPPA
	].map((val, i) => val * D50[i]);
	return [
		"xyz-d50",
		roundToPrecision(x, HEX),
		roundToPrecision(y, HEX),
		roundToPrecision(z, HEX),
		alpha
	];
};
/**
* parse lch()
* @param value - lch color value
* @param [opt] - options
* @returns parsed color
*   - ['xyz-d50', x, y, z, alpha], ['lch', l, c, h, alpha]
*   - '(empty)', NullObject
*/
var parseLch = (value, opt = {}) => {
	if (isString(value)) value = value.trim();
	else throw new TypeError(`${value} is not a string.`);
	const { format = "", nullable = false } = opt;
	if (!REG_LCH.test(value)) {
		const res = resolveInvalidColorValue(format, nullable);
		if (res instanceof NullObject) return res;
		if (isString(res)) return res;
		return res;
	}
	const COEF_PCT = 1.5;
	const [, val] = value.match(REG_LCH);
	const [v1, v2, v3, v4 = ""] = val.match(/[^\s,/]+/g);
	let l, c, h;
	if (v1 === "none") l = 0;
	else {
		l = parseFloat(v1);
		if (l < 0) l = 0;
	}
	if (v2 === "none") c = 0;
	else c = v2.endsWith("%") ? parseFloat(v2) * COEF_PCT : parseFloat(v2);
	if (v3 === "none") h = 0;
	else h = angleToDeg(v3);
	const alpha = parseAlpha(v4);
	if (REG_SPEC.test(format)) return [
		"lch",
		v1 === "none" ? v1 : roundToPrecision(l, HEX),
		v2 === "none" ? v2 : roundToPrecision(c, HEX),
		v3 === "none" ? v3 : roundToPrecision(h, HEX),
		v4 === "none" ? v4 : alpha
	];
	const a = c * Math.cos(h * Math.PI / DEG_HALF);
	const b = c * Math.sin(h * Math.PI / DEG_HALF);
	const [, x, y, z] = parseLab(`lab(${l} ${a} ${b})`);
	return [
		"xyz-d50",
		roundToPrecision(x, HEX),
		roundToPrecision(y, HEX),
		roundToPrecision(z, HEX),
		alpha
	];
};
/**
* parse oklab()
* @param value - oklab color value
* @param [opt] - options
* @returns parsed color
*   - ['xyz-d65', x, y, z, alpha], ['oklab', l, a, b, alpha]
*   - '(empty)', NullObject
*/
var parseOklab = (value, opt = {}) => {
	if (isString(value)) value = value.trim();
	else throw new TypeError(`${value} is not a string.`);
	const { format = "", nullable = false } = opt;
	if (!REG_OKLAB.test(value)) {
		const res = resolveInvalidColorValue(format, nullable);
		if (res instanceof NullObject) return res;
		if (isString(res)) return res;
		return res;
	}
	const COEF_PCT = .4;
	const [, val] = value.match(REG_OKLAB);
	const [v1, v2, v3, v4 = ""] = val.match(/[^\s,/]+/g);
	let l, a, b;
	if (v1 === "none") l = 0;
	else {
		l = v1.endsWith("%") ? parseFloat(v1) / MAX_PCT : parseFloat(v1);
		if (l < 0) l = 0;
	}
	if (v2 === "none") a = 0;
	else if (v2.endsWith("%")) a = parseFloat(v2) * COEF_PCT / MAX_PCT;
	else a = parseFloat(v2);
	if (v3 === "none") b = 0;
	else if (v3.endsWith("%")) b = parseFloat(v3) * COEF_PCT / MAX_PCT;
	else b = parseFloat(v3);
	const alpha = parseAlpha(v4);
	if (REG_SPEC.test(format)) return [
		"oklab",
		v1 === "none" ? v1 : roundToPrecision(l, HEX),
		v2 === "none" ? v2 : roundToPrecision(a, HEX),
		v3 === "none" ? v3 : roundToPrecision(b, HEX),
		v4 === "none" ? v4 : alpha
	];
	const [x, y, z] = transformMatrix(MATRIX_LMS_TO_XYZ, transformMatrix(MATRIX_OKLAB_TO_LMS, [
		l,
		a,
		b
	]).map((c) => Math.pow(c, POW_CUBE)), true);
	return [
		"xyz-d65",
		roundToPrecision(x, HEX),
		roundToPrecision(y, HEX),
		roundToPrecision(z, HEX),
		alpha
	];
};
/**
* parse oklch()
* @param value - oklch color value
* @param [opt] - options
* @returns parsed color
*   - ['xyz-d65', x, y, z, alpha], ['oklch', l, c, h, alpha]
*   - '(empty)', NullObject
*/
var parseOklch = (value, opt = {}) => {
	if (isString(value)) value = value.trim();
	else throw new TypeError(`${value} is not a string.`);
	const { format = "", nullable = false } = opt;
	if (!REG_OKLCH.test(value)) {
		const res = resolveInvalidColorValue(format, nullable);
		if (res instanceof NullObject) return res;
		if (isString(res)) return res;
		return res;
	}
	const COEF_PCT = .4;
	const [, val] = value.match(REG_OKLCH);
	const [v1, v2, v3, v4 = ""] = val.match(/[^\s,/]+/g);
	let l, c, h;
	if (v1 === "none") l = 0;
	else {
		l = v1.endsWith("%") ? parseFloat(v1) / MAX_PCT : parseFloat(v1);
		if (l < 0) l = 0;
	}
	if (v2 === "none") c = 0;
	else {
		if (v2.endsWith("%")) c = parseFloat(v2) * COEF_PCT / MAX_PCT;
		else c = parseFloat(v2);
		if (c < 0) c = 0;
	}
	if (v3 === "none") h = 0;
	else h = angleToDeg(v3);
	const alpha = parseAlpha(v4);
	if (REG_SPEC.test(format)) return [
		"oklch",
		v1 === "none" ? v1 : roundToPrecision(l, HEX),
		v2 === "none" ? v2 : roundToPrecision(c, HEX),
		v3 === "none" ? v3 : roundToPrecision(h, HEX),
		v4 === "none" ? v4 : alpha
	];
	const a = c * Math.cos(h * Math.PI / DEG_HALF);
	const b = c * Math.sin(h * Math.PI / DEG_HALF);
	const [x, y, z] = transformMatrix(MATRIX_LMS_TO_XYZ, transformMatrix(MATRIX_OKLAB_TO_LMS, [
		l,
		a,
		b
	]).map((cc) => Math.pow(cc, POW_CUBE)), true);
	return [
		"xyz-d65",
		roundToPrecision(x, HEX),
		roundToPrecision(y, HEX),
		roundToPrecision(z, HEX),
		alpha
	];
};
/**
* parse color()
* @param value - color function value
* @param [opt] - options
* @returns parsed color
*   - ['xyz-(d50|d65)', x, y, z, alpha], [cs, r, g, b, alpha]
*   - '(empty)', NullObject
*/
var parseColorFunc = (value, opt = {}) => {
	if (isString(value)) value = value.trim();
	else throw new TypeError(`${value} is not a string.`);
	const { colorSpace = "", d50 = false, format = "", nullable = false } = opt;
	if (!REG_FN_COLOR.test(value)) {
		const res = resolveInvalidColorValue(format, nullable);
		if (res instanceof NullObject) return res;
		if (isString(res)) return res;
		return res;
	}
	const [, val] = value.match(REG_FN_COLOR);
	let [cs, v1, v2, v3, v4 = ""] = val.match(/[^\s,/]+/g);
	let r, g, b;
	if (cs === "xyz") cs = "xyz-d65";
	if (v1 === "none") r = 0;
	else r = v1.endsWith("%") ? parseFloat(v1) / MAX_PCT : parseFloat(v1);
	if (v2 === "none") g = 0;
	else g = v2.endsWith("%") ? parseFloat(v2) / MAX_PCT : parseFloat(v2);
	if (v3 === "none") b = 0;
	else b = v3.endsWith("%") ? parseFloat(v3) / MAX_PCT : parseFloat(v3);
	const alpha = parseAlpha(v4);
	if (REG_SPEC.test(format) || format === "mixValue" && cs === colorSpace) return [
		cs,
		v1 === "none" ? v1 : roundToPrecision(r, DEC),
		v2 === "none" ? v2 : roundToPrecision(g, DEC),
		v3 === "none" ? v3 : roundToPrecision(b, DEC),
		v4 === "none" ? v4 : alpha
	];
	let x = 0;
	let y = 0;
	let z = 0;
	if (cs === "srgb-linear") {
		[x, y, z] = transformMatrix(MATRIX_L_RGB_TO_XYZ, [
			r,
			g,
			b
		]);
		if (d50) [x, y, z] = transformMatrix(MATRIX_D65_TO_D50, [
			x,
			y,
			z
		], true);
	} else if (cs === "display-p3") {
		const linearRgb = transformRgbToLinearRgb([
			r * MAX_RGB,
			g * MAX_RGB,
			b * MAX_RGB
		]);
		[x, y, z] = transformMatrix(MATRIX_P3_TO_XYZ, linearRgb);
		if (d50) [x, y, z] = transformMatrix(MATRIX_D65_TO_D50, [
			x,
			y,
			z
		], true);
	} else if (cs === "rec2020") {
		const ALPHA = 1.09929682680944;
		const BETA = .018053968510807;
		const REC_COEF = .45;
		const rgb = [
			r,
			g,
			b
		].map((c) => {
			let cl;
			if (c < BETA * REC_COEF * DEC) cl = c / (REC_COEF * DEC);
			else cl = Math.pow((c + ALPHA - 1) / ALPHA, 1 / REC_COEF);
			return cl;
		});
		[x, y, z] = transformMatrix(MATRIX_REC2020_TO_XYZ, rgb);
		if (d50) [x, y, z] = transformMatrix(MATRIX_D65_TO_D50, [
			x,
			y,
			z
		], true);
	} else if (cs === "a98-rgb") {
		const POW_A98 = 563 / 256;
		const rgb = [
			r,
			g,
			b
		].map((c) => {
			return Math.pow(c, POW_A98);
		});
		[x, y, z] = transformMatrix(MATRIX_A98_TO_XYZ, rgb);
		if (d50) [x, y, z] = transformMatrix(MATRIX_D65_TO_D50, [
			x,
			y,
			z
		], true);
	} else if (cs === "prophoto-rgb") {
		const POW_PROPHOTO = 1.8;
		const rgb = [
			r,
			g,
			b
		].map((c) => {
			let cl;
			if (c > 1 / (HEX * DUO)) cl = Math.pow(c, POW_PROPHOTO);
			else cl = c / HEX;
			return cl;
		});
		[x, y, z] = transformMatrix(MATRIX_PROPHOTO_TO_XYZ_D50, rgb);
		if (!d50) [x, y, z] = transformMatrix(MATRIX_D50_TO_D65, [
			x,
			y,
			z
		], true);
	} else if (/^xyz(?:-d(?:50|65))?$/.test(cs)) {
		[x, y, z] = [
			r,
			g,
			b
		];
		if (cs === "xyz-d50") {
			if (!d50) [x, y, z] = transformMatrix(MATRIX_D50_TO_D65, [
				x,
				y,
				z
			]);
		} else if (d50) [x, y, z] = transformMatrix(MATRIX_D65_TO_D50, [
			x,
			y,
			z
		], true);
	} else {
		[x, y, z] = transformRgbToXyz([
			r * MAX_RGB,
			g * MAX_RGB,
			b * MAX_RGB
		]);
		if (d50) [x, y, z] = transformMatrix(MATRIX_D65_TO_D50, [
			x,
			y,
			z
		], true);
	}
	return [
		d50 ? "xyz-d50" : "xyz-d65",
		roundToPrecision(x, HEX),
		roundToPrecision(y, HEX),
		roundToPrecision(z, HEX),
		format === "mixValue" && v4 === "none" ? v4 : alpha
	];
};
/**
* parse color value
* @param value - CSS color value
* @param [opt] - options
* @returns parsed color
*   - ['xyz-(d50|d65)', x, y, z, alpha], ['rgb', r, g, b, alpha]
*   - value, '(empty)', NullObject
*/
var parseColorValue = (value, opt = {}) => {
	if (isString(value)) value = value.toLowerCase().trim();
	else throw new TypeError(`${value} is not a string.`);
	const { d50 = false, format = "", nullable = false } = opt;
	if (!REG_COLOR.test(value)) {
		const res = resolveInvalidColorValue(format, nullable);
		if (res instanceof NullObject) return res;
		if (isString(res)) return res;
		return res;
	}
	let x = 0;
	let y = 0;
	let z = 0;
	let alpha = 0;
	if (REG_CURRENT.test(value)) {
		if (format === "computedValue") return [
			"rgb",
			0,
			0,
			0,
			0
		];
		if (format === "specifiedValue") return value;
	} else if (/^[a-z]+$/.test(value)) if (Object.hasOwn(NAMED_COLORS, value)) {
		if (format === "specifiedValue") return value;
		const [r, g, b] = NAMED_COLORS[value];
		alpha = 1;
		if (format === "computedValue") return [
			"rgb",
			r,
			g,
			b,
			alpha
		];
		[x, y, z] = transformRgbToXyz([
			r,
			g,
			b
		], true);
		if (d50) [x, y, z] = transformMatrix(MATRIX_D65_TO_D50, [
			x,
			y,
			z
		], true);
	} else switch (format) {
		case VAL_COMP:
			if (nullable && value !== "transparent") return new NullObject();
			return [
				"rgb",
				0,
				0,
				0,
				0
			];
		case VAL_SPEC:
			if (value === "transparent") return value;
			return "";
		case VAL_MIX:
			if (value === "transparent") return [
				"rgb",
				0,
				0,
				0,
				0
			];
			return new NullObject();
		default:
	}
	else if (value[0] === "#") {
		if (REG_SPEC.test(format)) return ["rgb", ...convertHexToRgb(value)];
		[x, y, z, alpha] = convertHexToXyz(value);
		if (d50) [x, y, z] = transformMatrix(MATRIX_D65_TO_D50, [
			x,
			y,
			z
		], true);
	} else if (value.startsWith("lab")) {
		if (REG_SPEC.test(format)) return parseLab(value, opt);
		[, x, y, z, alpha] = parseLab(value);
		if (!d50) [x, y, z] = transformMatrix(MATRIX_D50_TO_D65, [
			x,
			y,
			z
		], true);
	} else if (value.startsWith("lch")) {
		if (REG_SPEC.test(format)) return parseLch(value, opt);
		[, x, y, z, alpha] = parseLch(value);
		if (!d50) [x, y, z] = transformMatrix(MATRIX_D50_TO_D65, [
			x,
			y,
			z
		], true);
	} else if (value.startsWith("oklab")) {
		if (REG_SPEC.test(format)) return parseOklab(value, opt);
		[, x, y, z, alpha] = parseOklab(value);
		if (d50) [x, y, z] = transformMatrix(MATRIX_D65_TO_D50, [
			x,
			y,
			z
		], true);
	} else if (value.startsWith("oklch")) {
		if (REG_SPEC.test(format)) return parseOklch(value, opt);
		[, x, y, z, alpha] = parseOklch(value);
		if (d50) [x, y, z] = transformMatrix(MATRIX_D65_TO_D50, [
			x,
			y,
			z
		], true);
	} else {
		let r, g, b;
		if (value.startsWith("hsl")) [, r, g, b, alpha] = parseHsl(value);
		else if (value.startsWith("hwb")) [, r, g, b, alpha] = parseHwb(value);
		else [, r, g, b, alpha] = parseRgb(value, opt);
		if (REG_SPEC.test(format)) return [
			"rgb",
			Math.round(r),
			Math.round(g),
			Math.round(b),
			alpha
		];
		[x, y, z] = transformRgbToXyz([
			r,
			g,
			b
		]);
		if (d50) [x, y, z] = transformMatrix(MATRIX_D65_TO_D50, [
			x,
			y,
			z
		], true);
	}
	return [
		d50 ? "xyz-d50" : "xyz-d65",
		roundToPrecision(x, HEX),
		roundToPrecision(y, HEX),
		roundToPrecision(z, HEX),
		alpha
	];
};
/**
* resolve color value
* @param value - CSS color value
* @param [opt] - options
* @returns resolved color
*   - [cs, v1, v2, v3, alpha], value, '(empty)', NullObject
*/
var resolveColorValue = (value, opt = {}) => {
	if (isString(value)) value = value.toLowerCase().trim();
	else throw new TypeError(`${value} is not a string.`);
	const { colorSpace = "", format = "", nullable = false } = opt;
	const cacheKey = createCacheKey({
		namespace: NAMESPACE,
		name: "resolveColorValue",
		value
	}, opt);
	const cachedResult = getCache(cacheKey);
	if (cachedResult instanceof CacheItem) {
		if (cachedResult.isNull) return cachedResult;
		const cachedItem = cachedResult.item;
		if (isString(cachedItem)) return cachedItem;
		return cachedItem;
	}
	if (!REG_COLOR.test(value)) {
		const res = resolveInvalidColorValue(format, nullable);
		if (res instanceof NullObject) {
			setCache(cacheKey, null);
			return res;
		}
		setCache(cacheKey, res);
		if (isString(res)) return res;
		return res;
	}
	let cs = "";
	let r = 0;
	let g = 0;
	let b = 0;
	let alpha = 0;
	if (REG_CURRENT.test(value)) {
		if (format === "specifiedValue") {
			setCache(cacheKey, value);
			return value;
		}
	} else if (/^[a-z]+$/.test(value)) if (Object.hasOwn(NAMED_COLORS, value)) {
		if (format === "specifiedValue") {
			setCache(cacheKey, value);
			return value;
		}
		[r, g, b] = NAMED_COLORS[value];
		alpha = 1;
	} else switch (format) {
		case VAL_SPEC: {
			if (value === "transparent") {
				setCache(cacheKey, value);
				return value;
			}
			const res = "";
			setCache(cacheKey, res);
			return res;
		}
		case VAL_MIX:
			if (value === "transparent") {
				const res = [
					"rgb",
					0,
					0,
					0,
					0
				];
				setCache(cacheKey, res);
				return res;
			}
			setCache(cacheKey, null);
			return new NullObject();
		case VAL_COMP:
		default: {
			if (nullable && value !== "transparent") {
				setCache(cacheKey, null);
				return new NullObject();
			}
			const res = [
				"rgb",
				0,
				0,
				0,
				0
			];
			setCache(cacheKey, res);
			return res;
		}
	}
	else if (value[0] === "#") [r, g, b, alpha] = convertHexToRgb(value);
	else if (value.startsWith("hsl")) [, r, g, b, alpha] = parseHsl(value, opt);
	else if (value.startsWith("hwb")) [, r, g, b, alpha] = parseHwb(value, opt);
	else if (/^l(?:ab|ch)/.test(value)) {
		let x, y, z;
		if (value.startsWith("lab")) [cs, x, y, z, alpha] = parseLab(value, opt);
		else [cs, x, y, z, alpha] = parseLch(value, opt);
		if (REG_SPEC.test(format)) {
			const res = [
				cs,
				x,
				y,
				z,
				alpha
			];
			setCache(cacheKey, res);
			return res;
		}
		[r, g, b] = transformXyzD50ToRgb([
			x,
			y,
			z
		]);
	} else if (/^okl(?:ab|ch)/.test(value)) {
		let x, y, z;
		if (value.startsWith("oklab")) [cs, x, y, z, alpha] = parseOklab(value, opt);
		else [cs, x, y, z, alpha] = parseOklch(value, opt);
		if (REG_SPEC.test(format)) {
			const res = [
				cs,
				x,
				y,
				z,
				alpha
			];
			setCache(cacheKey, res);
			return res;
		}
		[r, g, b] = transformXyzToRgb([
			x,
			y,
			z
		]);
	} else [, r, g, b, alpha] = parseRgb(value, opt);
	if (format === "mixValue" && colorSpace === "srgb") {
		const res = [
			"srgb",
			r / MAX_RGB,
			g / MAX_RGB,
			b / MAX_RGB,
			alpha
		];
		setCache(cacheKey, res);
		return res;
	}
	const res = [
		"rgb",
		Math.round(r),
		Math.round(g),
		Math.round(b),
		alpha
	];
	setCache(cacheKey, res);
	return res;
};
/**
* resolve color()
* @param value - color function value
* @param [opt] - options
* @returns resolved color - [cs, v1, v2, v3, alpha], '(empty)', NullObject
*/
var resolveColorFunc = (value, opt = {}) => {
	if (isString(value)) value = value.toLowerCase().trim();
	else throw new TypeError(`${value} is not a string.`);
	const { colorSpace = "", format = "", nullable = false } = opt;
	const cacheKey = createCacheKey({
		namespace: NAMESPACE,
		name: "resolveColorFunc",
		value
	}, opt);
	const cachedResult = getCache(cacheKey);
	if (cachedResult instanceof CacheItem) {
		if (cachedResult.isNull) return cachedResult;
		const cachedItem = cachedResult.item;
		if (isString(cachedItem)) return cachedItem;
		return cachedItem;
	}
	if (!REG_FN_COLOR.test(value)) {
		const res = resolveInvalidColorValue(format, nullable);
		if (res instanceof NullObject) {
			setCache(cacheKey, null);
			return res;
		}
		setCache(cacheKey, res);
		if (isString(res)) return res;
		return res;
	}
	const [cs, v1, v2, v3, v4] = parseColorFunc(value, opt);
	if (REG_SPEC.test(format) || format === "mixValue" && cs === colorSpace) {
		const res = [
			cs,
			v1,
			v2,
			v3,
			v4
		];
		setCache(cacheKey, res);
		return res;
	}
	const x = parseFloat(`${v1}`);
	const y = parseFloat(`${v2}`);
	const z = parseFloat(`${v3}`);
	const alpha = parseAlpha(`${v4}`);
	const [r, g, b] = transformXyzToRgb([
		x,
		y,
		z
	], true);
	const res = [
		"rgb",
		r,
		g,
		b,
		alpha
	];
	setCache(cacheKey, res);
	return res;
};
/**
* convert color value to linear rgb
* @param value - CSS color value
* @param [opt] - options
* @returns ColorChannels | NullObject - [r, g, b, alpha] r|g|b|alpha: 0..1
*/
var convertColorToLinearRgb = (value, opt = {}) => {
	if (isString(value)) value = value.trim();
	else throw new TypeError(`${value} is not a string.`);
	const { colorSpace = "", format = "" } = opt;
	let cs = "";
	let r, g, b, alpha, x, y, z;
	if (format === "mixValue") {
		let xyz;
		if (value.startsWith("color(")) xyz = parseColorFunc(value, opt);
		else xyz = parseColorValue(value, opt);
		if (xyz instanceof NullObject) return xyz;
		[cs, x, y, z, alpha] = xyz;
		if (cs === colorSpace) return [
			x,
			y,
			z,
			alpha
		];
		[r, g, b] = transformMatrix(MATRIX_XYZ_TO_L_RGB, [
			x,
			y,
			z
		], true);
	} else if (value.startsWith("color(")) {
		const [, val] = value.match(REG_FN_COLOR);
		const [cs] = val.match(/[^\s,/]+/g);
		if (cs === "srgb-linear") [, r, g, b, alpha] = resolveColorFunc(value, { format: VAL_COMP });
		else {
			[, x, y, z, alpha] = parseColorFunc(value);
			[r, g, b] = transformMatrix(MATRIX_XYZ_TO_L_RGB, [
				x,
				y,
				z
			], true);
		}
	} else {
		[, x, y, z, alpha] = parseColorValue(value);
		[r, g, b] = transformMatrix(MATRIX_XYZ_TO_L_RGB, [
			x,
			y,
			z
		], true);
	}
	return [
		Math.min(Math.max(r, 0), 1),
		Math.min(Math.max(g, 0), 1),
		Math.min(Math.max(b, 0), 1),
		alpha
	];
};
/**
* convert color value to rgb
* @param value - CSS color value
* @param [opt] - options
* @returns ColorChannels | NullObject
*   - [r, g, b, alpha] r|g|b: 0..255 alpha: 0..1
*/
var convertColorToRgb = (value, opt = {}) => {
	if (isString(value)) value = value.trim();
	else throw new TypeError(`${value} is not a string.`);
	const { format = "" } = opt;
	let r, g, b, alpha;
	if (format === "mixValue") {
		let rgb;
		if (value.startsWith("color(")) rgb = resolveColorFunc(value, opt);
		else rgb = resolveColorValue(value, opt);
		if (rgb instanceof NullObject) return rgb;
		[, r, g, b, alpha] = rgb;
	} else if (value.startsWith("color(")) {
		const [, val] = value.match(REG_FN_COLOR);
		const [cs] = val.match(/[^\s,/]+/g);
		if (cs === "srgb") {
			[, r, g, b, alpha] = resolveColorFunc(value, { format: VAL_COMP });
			r *= MAX_RGB;
			g *= MAX_RGB;
			b *= MAX_RGB;
		} else [, r, g, b, alpha] = resolveColorFunc(value);
	} else if (/^(?:ok)?l(?:ab|ch)/.test(value)) {
		[r, g, b, alpha] = convertColorToLinearRgb(value);
		[r, g, b] = transformLinearRgbToRgb([
			r,
			g,
			b
		]);
	} else [, r, g, b, alpha] = resolveColorValue(value, { format: VAL_COMP });
	return [
		r,
		g,
		b,
		alpha
	];
};
/**
* convert color value to xyz
* @param value - CSS color value
* @param [opt] - options
* @returns ColorChannels | NullObject - [x, y, z, alpha]
*/
var convertColorToXyz = (value, opt = {}) => {
	if (isString(value)) value = value.trim();
	else throw new TypeError(`${value} is not a string.`);
	const { d50 = false, format = "" } = opt;
	let x, y, z, alpha;
	if (format === "mixValue") {
		let xyz;
		if (value.startsWith("color(")) xyz = parseColorFunc(value, opt);
		else xyz = parseColorValue(value, opt);
		if (xyz instanceof NullObject) return xyz;
		[, x, y, z, alpha] = xyz;
	} else if (value.startsWith("color(")) {
		const [, val] = value.match(REG_FN_COLOR);
		const [cs] = val.match(/[^\s,/]+/g);
		if (d50) if (cs === "xyz-d50") [, x, y, z, alpha] = resolveColorFunc(value, { format: VAL_COMP });
		else [, x, y, z, alpha] = parseColorFunc(value, opt);
		else if (/^xyz(?:-d65)?$/.test(cs)) [, x, y, z, alpha] = resolveColorFunc(value, { format: VAL_COMP });
		else [, x, y, z, alpha] = parseColorFunc(value);
	} else [, x, y, z, alpha] = parseColorValue(value, opt);
	return [
		x,
		y,
		z,
		alpha
	];
};
/**
* convert color value to hsl
* @param value - CSS color value
* @param [opt] - options
* @returns ColorChannels | NullObject - [h, s, l, alpha], hue may be powerless
*/
var convertColorToHsl = (value, opt = {}) => {
	if (isString(value)) value = value.trim();
	else throw new TypeError(`${value} is not a string.`);
	const { format = "" } = opt;
	let h, s, l, alpha;
	if (REG_HSL.test(value)) {
		[, h, s, l, alpha] = parseHsl(value, { format: "hsl" });
		if (format === "hsl") return [
			Math.round(h),
			Math.round(s),
			Math.round(l),
			alpha
		];
		return [
			h,
			s,
			l,
			alpha
		];
	}
	let x, y, z;
	if (format === "mixValue") {
		let xyz;
		if (value.startsWith("color(")) xyz = parseColorFunc(value, opt);
		else xyz = parseColorValue(value, opt);
		if (xyz instanceof NullObject) return xyz;
		[, x, y, z, alpha] = xyz;
	} else if (value.startsWith("color(")) [, x, y, z, alpha] = parseColorFunc(value);
	else [, x, y, z, alpha] = parseColorValue(value);
	[h, s, l] = transformXyzToHsl([
		x,
		y,
		z
	], true);
	if (format === "hsl") return [
		Math.round(h),
		Math.round(s),
		Math.round(l),
		alpha
	];
	return [
		format === "mixValue" && s === 0 ? NONE : h,
		s,
		l,
		alpha
	];
};
/**
* convert color value to hwb
* @param value - CSS color value
* @param [opt] - options
* @returns ColorChannels | NullObject - [h, w, b, alpha], hue may be powerless
*/
var convertColorToHwb = (value, opt = {}) => {
	if (isString(value)) value = value.trim();
	else throw new TypeError(`${value} is not a string.`);
	const { format = "" } = opt;
	let h, w, b, alpha;
	if (REG_HWB.test(value)) {
		[, h, w, b, alpha] = parseHwb(value, { format: "hwb" });
		if (format === "hwb") return [
			Math.round(h),
			Math.round(w),
			Math.round(b),
			alpha
		];
		return [
			h,
			w,
			b,
			alpha
		];
	}
	let x, y, z;
	if (format === "mixValue") {
		let xyz;
		if (value.startsWith("color(")) xyz = parseColorFunc(value, opt);
		else xyz = parseColorValue(value, opt);
		if (xyz instanceof NullObject) return xyz;
		[, x, y, z, alpha] = xyz;
	} else if (value.startsWith("color(")) [, x, y, z, alpha] = parseColorFunc(value);
	else [, x, y, z, alpha] = parseColorValue(value);
	[h, w, b] = transformXyzToHwb([
		x,
		y,
		z
	], true);
	if (format === "hwb") return [
		Math.round(h),
		Math.round(w),
		Math.round(b),
		alpha
	];
	return [
		format === "mixValue" && w + b >= 100 ? NONE : h,
		w,
		b,
		alpha
	];
};
/**
* convert color value to lab
* @param value - CSS color value
* @param [opt] - options
* @returns ColorChannels | NullObject - [l, a, b, alpha]
*/
var convertColorToLab = (value, opt = {}) => {
	if (isString(value)) value = value.trim();
	else throw new TypeError(`${value} is not a string.`);
	const { format = "" } = opt;
	let l, a, b, alpha;
	if (REG_LAB.test(value)) {
		[, l, a, b, alpha] = parseLab(value, { format: VAL_COMP });
		return [
			l,
			a,
			b,
			alpha
		];
	}
	let x, y, z;
	if (format === "mixValue") {
		let xyz;
		opt.d50 = true;
		if (value.startsWith("color(")) xyz = parseColorFunc(value, opt);
		else xyz = parseColorValue(value, opt);
		if (xyz instanceof NullObject) return xyz;
		[, x, y, z, alpha] = xyz;
	} else if (value.startsWith("color(")) [, x, y, z, alpha] = parseColorFunc(value, { d50: true });
	else [, x, y, z, alpha] = parseColorValue(value, { d50: true });
	[l, a, b] = transformXyzD50ToLab([
		x,
		y,
		z
	], true);
	return [
		l,
		a,
		b,
		alpha
	];
};
/**
* convert color value to lch
* @param value - CSS color value
* @param [opt] - options
* @returns ColorChannels | NullObject - [l, c, h, alpha], hue may be powerless
*/
var convertColorToLch = (value, opt = {}) => {
	if (isString(value)) value = value.trim();
	else throw new TypeError(`${value} is not a string.`);
	const { format = "" } = opt;
	let l, c, h, alpha;
	if (REG_LCH.test(value)) {
		[, l, c, h, alpha] = parseLch(value, { format: VAL_COMP });
		return [
			l,
			c,
			h,
			alpha
		];
	}
	let x, y, z;
	if (format === "mixValue") {
		let xyz;
		opt.d50 = true;
		if (value.startsWith("color(")) xyz = parseColorFunc(value, opt);
		else xyz = parseColorValue(value, opt);
		if (xyz instanceof NullObject) return xyz;
		[, x, y, z, alpha] = xyz;
	} else if (value.startsWith("color(")) [, x, y, z, alpha] = parseColorFunc(value, { d50: true });
	else [, x, y, z, alpha] = parseColorValue(value, { d50: true });
	[l, c, h] = transformXyzD50ToLch([
		x,
		y,
		z
	], true);
	return [
		l,
		c,
		format === "mixValue" && c === 0 ? NONE : h,
		alpha
	];
};
/**
* convert color value to oklab
* @param value - CSS color value
* @param [opt] - options
* @returns ColorChannels | NullObject - [l, a, b, alpha]
*/
var convertColorToOklab = (value, opt = {}) => {
	if (isString(value)) value = value.trim();
	else throw new TypeError(`${value} is not a string.`);
	const { format = "" } = opt;
	let l, a, b, alpha;
	if (REG_OKLAB.test(value)) {
		[, l, a, b, alpha] = parseOklab(value, { format: VAL_COMP });
		return [
			l,
			a,
			b,
			alpha
		];
	}
	let x, y, z;
	if (format === "mixValue") {
		let xyz;
		if (value.startsWith("color(")) xyz = parseColorFunc(value, opt);
		else xyz = parseColorValue(value, opt);
		if (xyz instanceof NullObject) return xyz;
		[, x, y, z, alpha] = xyz;
	} else if (value.startsWith("color(")) [, x, y, z, alpha] = parseColorFunc(value);
	else [, x, y, z, alpha] = parseColorValue(value);
	[l, a, b] = transformXyzToOklab([
		x,
		y,
		z
	], true);
	return [
		l,
		a,
		b,
		alpha
	];
};
/**
* convert color value to oklch
* @param value - CSS color value
* @param [opt] - options
* @returns ColorChannels | NullObject - [l, c, h, alpha], hue may be powerless
*/
var convertColorToOklch = (value, opt = {}) => {
	if (isString(value)) value = value.trim();
	else throw new TypeError(`${value} is not a string.`);
	const { format = "" } = opt;
	let l, c, h, alpha;
	if (REG_OKLCH.test(value)) {
		[, l, c, h, alpha] = parseOklch(value, { format: VAL_COMP });
		return [
			l,
			c,
			h,
			alpha
		];
	}
	let x, y, z;
	if (format === "mixValue") {
		let xyz;
		if (value.startsWith("color(")) xyz = parseColorFunc(value, opt);
		else xyz = parseColorValue(value, opt);
		if (xyz instanceof NullObject) return xyz;
		[, x, y, z, alpha] = xyz;
	} else if (value.startsWith("color(")) [, x, y, z, alpha] = parseColorFunc(value);
	else [, x, y, z, alpha] = parseColorValue(value);
	[l, c, h] = transformXyzToOklch([
		x,
		y,
		z
	], true);
	return [
		l,
		c,
		format === "mixValue" && c === 0 ? NONE : h,
		alpha
	];
};
/**
* resolve color-mix()
* @param value - color-mix color value
* @param [opt] - options
* @returns resolved color - [cs, v1, v2, v3, alpha], '(empty)'
*/
var resolveColorMix = (value, opt = {}) => {
	if (isString(value)) value = value.toLowerCase().trim();
	else throw new TypeError(`${value} is not a string.`);
	const { format = "", nullable = false } = opt;
	const cacheKey = createCacheKey({
		namespace: NAMESPACE,
		name: "resolveColorMix",
		value
	}, opt);
	const cachedResult = getCache(cacheKey);
	if (cachedResult instanceof CacheItem) {
		if (cachedResult.isNull) return cachedResult;
		const cachedItem = cachedResult.item;
		if (isString(cachedItem)) return cachedItem;
		return cachedItem;
	}
	const nestedItems = [];
	let colorSpace = "";
	let hueArc = "";
	let colorA = "";
	let pctA = "";
	let colorB = "";
	let pctB = "";
	let parsed = false;
	if (!REG_MIX.test(value)) if (value.startsWith("color-mix(") && REG_MIX_NEST.test(value)) {
		const items = value.match(REG_MIX_NEST);
		for (const item of items) if (item) {
			let val = resolveColorMix(item, { format: format === "specifiedValue" ? format : VAL_COMP });
			if (Array.isArray(val)) {
				const [cs, v1, v2, v3, v4] = val;
				if (v1 === 0 && v2 === 0 && v3 === 0 && v4 === 0) {
					value = "";
					break;
				}
				if (REG_MIX_CS_RGB_XYZ.test(cs)) if (v4 === 1) val = `color(${cs} ${v1} ${v2} ${v3})`;
				else val = `color(${cs} ${v1} ${v2} ${v3} / ${v4})`;
				else if (v4 === 1) val = `${cs}(${v1} ${v2} ${v3})`;
				else val = `${cs}(${v1} ${v2} ${v3} / ${v4})`;
			} else if (!REG_MIX.test(val)) {
				value = "";
				break;
			}
			nestedItems.push(val);
			value = value.replace(item, val);
		}
		if (!value) return cacheInvalidColorValue(cacheKey, format, nullable);
	} else if (value.startsWith("color-mix(") && value.endsWith(")") && value.includes("light-dark(")) {
		const [csPart = "", partA = "", partB = ""] = splitValue(value.replace(FN_MIX, "").replace(/\)$/, ""), { delimiter: "," });
		const [colorPartA = "", pctPartA = ""] = splitValue(partA);
		const [colorPartB = "", pctPartB = ""] = splitValue(partB);
		const specifiedColorA = resolveColor(colorPartA, { format: VAL_SPEC });
		const specifiedColorB = resolveColor(colorPartB, { format: VAL_SPEC });
		if (REG_MIX_IN_CS.test(csPart) && specifiedColorA && specifiedColorB) if (format === "specifiedValue") {
			const [, cs] = csPart.match(REG_MIX_IN_CS);
			if (REG_CS_HUE.test(cs)) [, colorSpace, hueArc] = cs.match(REG_CS_HUE);
			else colorSpace = cs;
			colorA = specifiedColorA;
			if (pctPartA) pctA = pctPartA;
			colorB = specifiedColorB;
			if (pctPartB) pctB = pctPartB;
			value = value.replace(colorPartA, specifiedColorA).replace(colorPartB, specifiedColorB);
			parsed = true;
		} else {
			const resolvedColorA = resolveColor(colorPartA, opt);
			const resolvedColorB = resolveColor(colorPartB, opt);
			if (isString(resolvedColorA) && isString(resolvedColorB)) value = value.replace(colorPartA, resolvedColorA).replace(colorPartB, resolvedColorB);
		}
		else return cacheInvalidColorValue(cacheKey, format, nullable);
	} else return cacheInvalidColorValue(cacheKey, format, nullable);
	if (nestedItems.length && format === "specifiedValue") {
		const [, cs] = value.match(REG_MIX_START);
		if (REG_CS_HUE.test(cs)) [, colorSpace, hueArc] = cs.match(REG_CS_HUE);
		else colorSpace = cs;
		if (nestedItems.length === 2) {
			let [itemA, itemB] = nestedItems;
			itemA = itemA.replace(/(?=[()])/g, "\\");
			itemB = itemB.replace(/(?=[()])/g, "\\");
			const regA = new RegExp(`(${itemA})(?:\\s+(${PCT}))?`);
			const regB = new RegExp(`(${itemB})(?:\\s+(${PCT}))?`);
			[, colorA, pctA] = value.match(regA);
			[, colorB, pctB] = value.match(regB);
		} else {
			let [item] = nestedItems;
			item = item.replace(/(?=[()])/g, "\\");
			const itemPart = `${item}(?:\\s+${PCT})?`;
			const itemPartCapt = `(${item})(?:\\s+(${PCT}))?`;
			const regItemPart = new RegExp(`^${itemPartCapt}$`);
			if (new RegExp(`${itemPartCapt}\\s*\\)$`).test(value)) {
				const reg = new RegExp(`(${SYN_MIX_PART})\\s*,\\s*(${itemPart})\\s*\\)$`);
				const [, colorPartA, colorPartB] = value.match(reg);
				[, colorA, pctA] = colorPartA.match(REG_MIX_COLOR_PART);
				[, colorB, pctB] = colorPartB.match(regItemPart);
			} else {
				const reg = new RegExp(`(${itemPart})\\s*,\\s*(${SYN_MIX_PART})\\s*\\)$`);
				const [, colorPartA, colorPartB] = value.match(reg);
				[, colorA, pctA] = colorPartA.match(regItemPart);
				[, colorB, pctB] = colorPartB.match(REG_MIX_COLOR_PART);
			}
		}
	} else if (!parsed) {
		const [, cs, colorPartA, colorPartB] = value.match(REG_MIX_CAPT);
		[, colorA, pctA] = colorPartA.match(REG_MIX_COLOR_PART);
		[, colorB, pctB] = colorPartB.match(REG_MIX_COLOR_PART);
		if (REG_CS_HUE.test(cs)) [, colorSpace, hueArc] = cs.match(REG_CS_HUE);
		else colorSpace = cs;
	}
	let pA, pB, m;
	if (pctA && pctB) {
		const p1 = parseFloat(pctA) / MAX_PCT;
		const p2 = parseFloat(pctB) / MAX_PCT;
		if (p1 < 0 || p1 > 1 || p2 < 0 || p2 > 1 || p1 === 0 && p2 === 0) return cacheInvalidColorValue(cacheKey, format, nullable);
		const factor = p1 + p2;
		pA = p1 / factor;
		pB = p2 / factor;
		m = factor < 1 ? factor : 1;
	} else {
		if (pctA) {
			pA = parseFloat(pctA) / MAX_PCT;
			if (pA < 0 || pA > 1) return cacheInvalidColorValue(cacheKey, format, nullable);
			pB = 1 - pA;
		} else if (pctB) {
			pB = parseFloat(pctB) / MAX_PCT;
			if (pB < 0 || pB > 1) return cacheInvalidColorValue(cacheKey, format, nullable);
			pA = 1 - pB;
		} else {
			pA = HALF;
			pB = HALF;
		}
		m = 1;
	}
	if (colorSpace === "xyz") colorSpace = "xyz-d65";
	if (format === "specifiedValue") {
		let valueA = "";
		let valueB = "";
		if (colorA.startsWith("color-mix(") || colorA.startsWith("light-dark(")) valueA = colorA;
		else if (colorA.startsWith("color(")) {
			const [cs, v1, v2, v3, v4] = parseColorFunc(colorA, opt);
			if (v4 === 1) valueA = `color(${cs} ${v1} ${v2} ${v3})`;
			else valueA = `color(${cs} ${v1} ${v2} ${v3} / ${v4})`;
		} else {
			const val = parseColorValue(colorA, opt);
			if (Array.isArray(val)) {
				const [cs, v1, v2, v3, v4] = val;
				if (v4 === 1) if (cs === "rgb") valueA = `${cs}(${v1}, ${v2}, ${v3})`;
				else valueA = `${cs}(${v1} ${v2} ${v3})`;
				else if (cs === "rgb") valueA = `${cs}a(${v1}, ${v2}, ${v3}, ${v4})`;
				else valueA = `${cs}(${v1} ${v2} ${v3} / ${v4})`;
			} else {
				if (!isString(val) || !val) {
					setCache(cacheKey, "");
					return "";
				}
				valueA = val;
			}
		}
		if (colorB.startsWith("color-mix(") || colorB.startsWith("light-dark(")) valueB = colorB;
		else if (colorB.startsWith("color(")) {
			const [cs, v1, v2, v3, v4] = parseColorFunc(colorB, opt);
			if (v4 === 1) valueB = `color(${cs} ${v1} ${v2} ${v3})`;
			else valueB = `color(${cs} ${v1} ${v2} ${v3} / ${v4})`;
		} else {
			const val = parseColorValue(colorB, opt);
			if (Array.isArray(val)) {
				const [cs, v1, v2, v3, v4] = val;
				if (v4 === 1) if (cs === "rgb") valueB = `${cs}(${v1}, ${v2}, ${v3})`;
				else valueB = `${cs}(${v1} ${v2} ${v3})`;
				else if (cs === "rgb") valueB = `${cs}a(${v1}, ${v2}, ${v3}, ${v4})`;
				else valueB = `${cs}(${v1} ${v2} ${v3} / ${v4})`;
			} else {
				if (!isString(val) || !val) {
					setCache(cacheKey, "");
					return "";
				}
				valueB = val;
			}
		}
		if (pctA && pctB) {
			valueA += ` ${parseFloat(pctA)}%`;
			valueB += ` ${parseFloat(pctB)}%`;
		} else if (pctA) {
			const pA = parseFloat(pctA);
			if (pA !== MAX_PCT * HALF) valueA += ` ${pA}%`;
		} else if (pctB) {
			const pA = MAX_PCT - parseFloat(pctB);
			if (pA !== MAX_PCT * HALF) valueA += ` ${pA}%`;
		}
		if (hueArc) {
			const res = `color-mix(in ${colorSpace} ${hueArc} hue, ${valueA}, ${valueB})`;
			setCache(cacheKey, res);
			return res;
		} else {
			const res = `color-mix(in ${colorSpace}, ${valueA}, ${valueB})`;
			setCache(cacheKey, res);
			return res;
		}
	}
	let r = 0;
	let g = 0;
	let b = 0;
	let alpha = 0;
	if (/^srgb(?:-linear)?$/.test(colorSpace)) {
		let rgbA, rgbB;
		if (colorSpace === "srgb") {
			if (REG_CURRENT.test(colorA)) rgbA = [
				NONE,
				NONE,
				NONE,
				NONE
			];
			else rgbA = convertColorToRgb(colorA, {
				colorSpace,
				format: VAL_MIX
			});
			if (REG_CURRENT.test(colorB)) rgbB = [
				NONE,
				NONE,
				NONE,
				NONE
			];
			else rgbB = convertColorToRgb(colorB, {
				colorSpace,
				format: VAL_MIX
			});
		} else {
			if (REG_CURRENT.test(colorA)) rgbA = [
				NONE,
				NONE,
				NONE,
				NONE
			];
			else rgbA = convertColorToLinearRgb(colorA, {
				colorSpace,
				format: VAL_MIX
			});
			if (REG_CURRENT.test(colorB)) rgbB = [
				NONE,
				NONE,
				NONE,
				NONE
			];
			else rgbB = convertColorToLinearRgb(colorB, {
				colorSpace,
				format: VAL_MIX
			});
		}
		if (rgbA instanceof NullObject || rgbB instanceof NullObject) return cacheInvalidColorValue(cacheKey, format, nullable);
		const [rrA, ggA, bbA, aaA] = rgbA;
		const [rrB, ggB, bbB, aaB] = rgbB;
		const rNone = rrA === "none" && rrB === "none";
		const gNone = ggA === "none" && ggB === "none";
		const bNone = bbA === "none" && bbB === "none";
		const alphaNone = aaA === "none" && aaB === "none";
		const [[rA, gA, bA, alphaA], [rB, gB, bB, alphaB]] = normalizeColorComponents([
			rrA,
			ggA,
			bbA,
			aaA
		], [
			rrB,
			ggB,
			bbB,
			aaB
		], true);
		const factorA = alphaA * pA;
		const factorB = alphaB * pB;
		alpha = factorA + factorB;
		if (alpha === 0) {
			r = rA * pA + rB * pB;
			g = gA * pA + gB * pB;
			b = bA * pA + bB * pB;
		} else {
			r = (rA * factorA + rB * factorB) / alpha;
			g = (gA * factorA + gB * factorB) / alpha;
			b = (bA * factorA + bB * factorB) / alpha;
			alpha = parseFloat(alpha.toFixed(3));
		}
		if (format === "computedValue") {
			const res = [
				colorSpace,
				rNone ? NONE : roundToPrecision(r, HEX),
				gNone ? NONE : roundToPrecision(g, HEX),
				bNone ? NONE : roundToPrecision(b, HEX),
				alphaNone ? NONE : alpha * m
			];
			setCache(cacheKey, res);
			return res;
		}
		r *= MAX_RGB;
		g *= MAX_RGB;
		b *= MAX_RGB;
	} else if (REG_CS_XYZ.test(colorSpace)) {
		let xyzA, xyzB;
		if (REG_CURRENT.test(colorA)) xyzA = [
			NONE,
			NONE,
			NONE,
			NONE
		];
		else xyzA = convertColorToXyz(colorA, {
			colorSpace,
			d50: colorSpace === "xyz-d50",
			format: VAL_MIX
		});
		if (REG_CURRENT.test(colorB)) xyzB = [
			NONE,
			NONE,
			NONE,
			NONE
		];
		else xyzB = convertColorToXyz(colorB, {
			colorSpace,
			d50: colorSpace === "xyz-d50",
			format: VAL_MIX
		});
		if (xyzA instanceof NullObject || xyzB instanceof NullObject) return cacheInvalidColorValue(cacheKey, format, nullable);
		const [xxA, yyA, zzA, aaA] = xyzA;
		const [xxB, yyB, zzB, aaB] = xyzB;
		const xNone = xxA === "none" && xxB === "none";
		const yNone = yyA === "none" && yyB === "none";
		const zNone = zzA === "none" && zzB === "none";
		const alphaNone = aaA === "none" && aaB === "none";
		const [[xA, yA, zA, alphaA], [xB, yB, zB, alphaB]] = normalizeColorComponents([
			xxA,
			yyA,
			zzA,
			aaA
		], [
			xxB,
			yyB,
			zzB,
			aaB
		], true);
		const factorA = alphaA * pA;
		const factorB = alphaB * pB;
		alpha = factorA + factorB;
		let x, y, z;
		if (alpha === 0) {
			x = xA * pA + xB * pB;
			y = yA * pA + yB * pB;
			z = zA * pA + zB * pB;
		} else {
			x = (xA * factorA + xB * factorB) / alpha;
			y = (yA * factorA + yB * factorB) / alpha;
			z = (zA * factorA + zB * factorB) / alpha;
			alpha = parseFloat(alpha.toFixed(3));
		}
		if (format === "computedValue") {
			const res = [
				colorSpace,
				xNone ? NONE : roundToPrecision(x, HEX),
				yNone ? NONE : roundToPrecision(y, HEX),
				zNone ? NONE : roundToPrecision(z, HEX),
				alphaNone ? NONE : alpha * m
			];
			setCache(cacheKey, res);
			return res;
		}
		if (colorSpace === "xyz-d50") [r, g, b] = transformXyzD50ToRgb([
			x,
			y,
			z
		], true);
		else [r, g, b] = transformXyzToRgb([
			x,
			y,
			z
		], true);
	} else if (/^h(?:sl|wb)$/.test(colorSpace)) {
		let hslA, hslB;
		if (colorSpace === "hsl") {
			if (REG_CURRENT.test(colorA)) hslA = [
				NONE,
				NONE,
				NONE,
				NONE
			];
			else hslA = convertColorToHsl(colorA, {
				colorSpace,
				format: VAL_MIX
			});
			if (REG_CURRENT.test(colorB)) hslB = [
				NONE,
				NONE,
				NONE,
				NONE
			];
			else hslB = convertColorToHsl(colorB, {
				colorSpace,
				format: VAL_MIX
			});
		} else {
			if (REG_CURRENT.test(colorA)) hslA = [
				NONE,
				NONE,
				NONE,
				NONE
			];
			else hslA = convertColorToHwb(colorA, {
				colorSpace,
				format: VAL_MIX
			});
			if (REG_CURRENT.test(colorB)) hslB = [
				NONE,
				NONE,
				NONE,
				NONE
			];
			else hslB = convertColorToHwb(colorB, {
				colorSpace,
				format: VAL_MIX
			});
		}
		if (hslA instanceof NullObject || hslB instanceof NullObject) return cacheInvalidColorValue(cacheKey, format, nullable);
		const [hhA, ssA, llA, aaA] = hslA;
		const [hhB, ssB, llB, aaB] = hslB;
		const alphaNone = aaA === "none" && aaB === "none";
		let [[hA, sA, lA, alphaA], [hB, sB, lB, alphaB]] = normalizeColorComponents([
			hhA,
			ssA,
			llA,
			aaA
		], [
			hhB,
			ssB,
			llB,
			aaB
		], true);
		if (hueArc) [hA, hB] = interpolateHue(hA, hB, hueArc);
		const factorA = alphaA * pA;
		const factorB = alphaB * pB;
		alpha = factorA + factorB;
		const h = (hA * pA + hB * pB) % DEG;
		let s, l;
		if (alpha === 0) {
			s = sA * pA + sB * pB;
			l = lA * pA + lB * pB;
		} else {
			s = (sA * factorA + sB * factorB) / alpha;
			l = (lA * factorA + lB * factorB) / alpha;
			alpha = parseFloat(alpha.toFixed(3));
		}
		[r, g, b] = convertColorToRgb(`${colorSpace}(${h} ${s} ${l})`);
		if (format === "computedValue") {
			const res = [
				"srgb",
				roundToPrecision(r / MAX_RGB, HEX),
				roundToPrecision(g / MAX_RGB, HEX),
				roundToPrecision(b / MAX_RGB, HEX),
				alphaNone ? NONE : alpha * m
			];
			setCache(cacheKey, res);
			return res;
		}
	} else if (/^(?:ok)?lch$/.test(colorSpace)) {
		let lchA, lchB;
		if (colorSpace === "lch") {
			if (REG_CURRENT.test(colorA)) lchA = [
				NONE,
				NONE,
				NONE,
				NONE
			];
			else lchA = convertColorToLch(colorA, {
				colorSpace,
				format: VAL_MIX
			});
			if (REG_CURRENT.test(colorB)) lchB = [
				NONE,
				NONE,
				NONE,
				NONE
			];
			else lchB = convertColorToLch(colorB, {
				colorSpace,
				format: VAL_MIX
			});
		} else {
			if (REG_CURRENT.test(colorA)) lchA = [
				NONE,
				NONE,
				NONE,
				NONE
			];
			else lchA = convertColorToOklch(colorA, {
				colorSpace,
				format: VAL_MIX
			});
			if (REG_CURRENT.test(colorB)) lchB = [
				NONE,
				NONE,
				NONE,
				NONE
			];
			else lchB = convertColorToOklch(colorB, {
				colorSpace,
				format: VAL_MIX
			});
		}
		if (lchA instanceof NullObject || lchB instanceof NullObject) return cacheInvalidColorValue(cacheKey, format, nullable);
		const [llA, ccA, hhA, aaA] = lchA;
		const [llB, ccB, hhB, aaB] = lchB;
		const lNone = llA === "none" && llB === "none";
		const cNone = ccA === "none" && ccB === "none";
		const hNone = hhA === "none" && hhB === "none";
		const alphaNone = aaA === "none" && aaB === "none";
		let [[lA, cA, hA, alphaA], [lB, cB, hB, alphaB]] = normalizeColorComponents([
			llA,
			ccA,
			hhA,
			aaA
		], [
			llB,
			ccB,
			hhB,
			aaB
		], true);
		if (hueArc) [hA, hB] = interpolateHue(hA, hB, hueArc);
		const factorA = alphaA * pA;
		const factorB = alphaB * pB;
		alpha = factorA + factorB;
		const h = (hA * pA + hB * pB) % DEG;
		let l, c;
		if (alpha === 0) {
			l = lA * pA + lB * pB;
			c = cA * pA + cB * pB;
		} else {
			l = (lA * factorA + lB * factorB) / alpha;
			c = (cA * factorA + cB * factorB) / alpha;
			alpha = parseFloat(alpha.toFixed(3));
		}
		if (format === "computedValue") {
			const res = [
				colorSpace,
				lNone ? NONE : roundToPrecision(l, HEX),
				cNone ? NONE : roundToPrecision(c, HEX),
				hNone ? NONE : roundToPrecision(h, HEX),
				alphaNone ? NONE : alpha * m
			];
			setCache(cacheKey, res);
			return res;
		}
		[, r, g, b] = resolveColorValue(`${colorSpace}(${l} ${c} ${h})`);
	} else {
		let labA, labB;
		if (colorSpace === "lab") {
			if (REG_CURRENT.test(colorA)) labA = [
				NONE,
				NONE,
				NONE,
				NONE
			];
			else labA = convertColorToLab(colorA, {
				colorSpace,
				format: VAL_MIX
			});
			if (REG_CURRENT.test(colorB)) labB = [
				NONE,
				NONE,
				NONE,
				NONE
			];
			else labB = convertColorToLab(colorB, {
				colorSpace,
				format: VAL_MIX
			});
		} else {
			if (REG_CURRENT.test(colorA)) labA = [
				NONE,
				NONE,
				NONE,
				NONE
			];
			else labA = convertColorToOklab(colorA, {
				colorSpace,
				format: VAL_MIX
			});
			if (REG_CURRENT.test(colorB)) labB = [
				NONE,
				NONE,
				NONE,
				NONE
			];
			else labB = convertColorToOklab(colorB, {
				colorSpace,
				format: VAL_MIX
			});
		}
		if (labA instanceof NullObject || labB instanceof NullObject) return cacheInvalidColorValue(cacheKey, format, nullable);
		const [llA, aaA, bbA, alA] = labA;
		const [llB, aaB, bbB, alB] = labB;
		const lNone = llA === "none" && llB === "none";
		const aNone = aaA === "none" && aaB === "none";
		const bNone = bbA === "none" && bbB === "none";
		const alphaNone = alA === "none" && alB === "none";
		const [[lA, aA, bA, alphaA], [lB, aB, bB, alphaB]] = normalizeColorComponents([
			llA,
			aaA,
			bbA,
			alA
		], [
			llB,
			aaB,
			bbB,
			alB
		], true);
		const factorA = alphaA * pA;
		const factorB = alphaB * pB;
		alpha = factorA + factorB;
		let l, aO, bO;
		if (alpha === 0) {
			l = lA * pA + lB * pB;
			aO = aA * pA + aB * pB;
			bO = bA * pA + bB * pB;
		} else {
			l = (lA * factorA + lB * factorB) / alpha;
			aO = (aA * factorA + aB * factorB) / alpha;
			bO = (bA * factorA + bB * factorB) / alpha;
			alpha = parseFloat(alpha.toFixed(3));
		}
		if (format === "computedValue") {
			const res = [
				colorSpace,
				lNone ? NONE : roundToPrecision(l, HEX),
				aNone ? NONE : roundToPrecision(aO, HEX),
				bNone ? NONE : roundToPrecision(bO, HEX),
				alphaNone ? NONE : alpha * m
			];
			setCache(cacheKey, res);
			return res;
		}
		[, r, g, b] = resolveColorValue(`${colorSpace}(${l} ${aO} ${bO})`);
	}
	const res = [
		"rgb",
		Math.round(r),
		Math.round(g),
		Math.round(b),
		parseFloat((alpha * m).toFixed(3))
	];
	setCache(cacheKey, res);
	return res;
};
//#endregion
export { NAMED_COLORS, convertColorToHsl, convertColorToHwb, convertColorToLab, convertColorToLch, convertColorToOklab, convertColorToOklch, convertColorToRgb, convertRgbToHex, numberToHexString, parseColorFunc, parseColorValue, resolveColorFunc, resolveColorMix, resolveColorValue };

//# sourceMappingURL=color.js.map