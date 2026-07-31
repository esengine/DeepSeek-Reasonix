const require_trim = require("../../string/trim.js");
//#region src/compat/string/trim.ts
function trim(str, chars, guard) {
	if (str == null) return "";
	if (guard != null || chars == null) return str.toString().trim();
	switch (typeof chars) {
		case "object": if (Array.isArray(chars)) return require_trim.trim(str, chars.flatMap((x) => x.toString().split("")));
		else return require_trim.trim(str, chars.toString().split(""));
		default: return require_trim.trim(str, chars.toString().split(""));
	}
}
//#endregion
exports.trim = trim;
