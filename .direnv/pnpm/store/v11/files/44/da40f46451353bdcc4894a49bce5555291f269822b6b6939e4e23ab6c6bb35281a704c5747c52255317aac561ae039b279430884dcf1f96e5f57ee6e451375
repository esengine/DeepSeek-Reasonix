const require_getSymbols = require("./getSymbols.js");
//#region src/compat/_internal/getSymbolsIn.ts
function getSymbolsIn(object) {
	const result = [];
	while (object) {
		result.push(...require_getSymbols.getSymbols(object));
		object = Object.getPrototypeOf(object);
	}
	return result;
}
//#endregion
exports.getSymbolsIn = getSymbolsIn;
