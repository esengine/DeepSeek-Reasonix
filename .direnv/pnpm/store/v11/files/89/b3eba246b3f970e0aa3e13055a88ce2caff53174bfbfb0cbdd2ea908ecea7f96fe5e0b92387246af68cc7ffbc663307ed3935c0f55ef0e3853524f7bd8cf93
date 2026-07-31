const require_toString = require("../util/toString.js");
//#region src/compat/_internal/normalizeForCase.ts
function normalizeForCase(str) {
	if (typeof str !== "string") str = require_toString.toString(str);
	return str.replace(/['\u2019]/g, "");
}
//#endregion
exports.normalizeForCase = normalizeForCase;
