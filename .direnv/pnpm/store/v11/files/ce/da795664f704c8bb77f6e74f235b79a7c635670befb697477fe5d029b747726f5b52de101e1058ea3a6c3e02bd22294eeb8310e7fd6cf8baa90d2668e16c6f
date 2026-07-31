//#region src/server/colors/_internal/parseHex.ts
const BLACK = [
	0,
	0,
	0
];
const HEX_COLOR_REGEX = /^[0-9a-fA-F]{3}$|^[0-9a-fA-F]{6}$/;
/**
* Parses a hex color string (e.g. "#ff0000", "#f00", "ff0000") into
* an [r, g, b] tuple. Used by `hex()` and `bgHex()` to convert
* user-provided hex values into RGB components for ANSI escape codes.
*
* Only #RGB and #RRGGBB formats are supported. #RGBA and #RRGGBBAA
* are not accepted because ANSI terminals do not support alpha channels.
* Returns [0, 0, 0] (black) for invalid input.
*
* @param hex - A hex color string, with or without the leading `#`.
* @returns An `[r, g, b]` tuple with each component in the 0–255 range.
*/
function parseHex(hex) {
	const raw = hex.startsWith("#") ? hex.slice(1) : hex;
	if (!HEX_COLOR_REGEX.test(raw)) return BLACK;
	const full = raw.length === 3 ? raw[0] + raw[0] + raw[1] + raw[1] + raw[2] + raw[2] : raw;
	const colorValue = parseInt(full, 16);
	return [
		colorValue >> 16 & 255,
		colorValue >> 8 & 255,
		colorValue & 255
	];
}
//#endregion
exports.parseHex = parseHex;
