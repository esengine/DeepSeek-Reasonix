const require_toKey = require("../_internal/toKey.js");
const require_toString = require("./toString.js");
//#region src/compat/util/toPath.ts
/**
* Converts a deep key string into an array of path segments.
*
* This function takes a string representing a deep key (e.g., 'a.b.c' or 'a[b][c]') and breaks it down into an array of strings, each representing a segment of the path.
*
* @param deepKey - The deep key string to convert.
* @returns An array of strings, each representing a segment of the path.
*
* Examples:
*
* toPath('a.b.c') // Returns ['a', 'b', 'c']
* toPath('a[b][c]') // Returns ['a', 'b', 'c']
* toPath('.a.b.c') // Returns ['', 'a', 'b', 'c']
* toPath('a["b.c"].d') // Returns ['a', 'b.c', 'd']
* toPath('') // Returns []
* toPath('.a[b].c.d[e]["f.g"].h') // Returns ['', 'a', 'b', 'c', 'd', 'e', 'f.g', 'h']
*/
function toPath(deepKey) {
	if (Array.isArray(deepKey)) return deepKey.map(require_toKey.toKey);
	if (typeof deepKey === "symbol") return [deepKey];
	deepKey = require_toString.toString(deepKey);
	const result = [];
	const length = deepKey.length;
	if (length === 0) return result;
	let index = 0;
	let key = "";
	let quoteChar = "";
	let bracket = false;
	if (deepKey.charCodeAt(0) === 46) result.push("");
	while (index < length) {
		const char = deepKey[index];
		if (quoteChar) if (char === "\\" && index + 1 < length) {
			index++;
			key += deepKey[index];
		} else if (char === quoteChar) quoteChar = "";
		else key += char;
		else if (bracket) if (char === "\"" || char === "'") quoteChar = char;
		else if (char === "]") {
			bracket = false;
			result.push(key);
			key = "";
		} else key += char;
		else if (char === "[") {
			bracket = true;
			if (key) {
				result.push(key);
				key = "";
			}
		} else if (char === ".") {
			if (key) {
				result.push(key);
				key = "";
			}
			const next = deepKey[index + 1];
			if (next === void 0 || next === ".") result.push("");
		} else key += char;
		index++;
	}
	if (key) result.push(key);
	return result;
}
//#endregion
exports.toPath = toPath;
