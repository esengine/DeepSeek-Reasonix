const require_deburr = require("../../string/deburr.js");
const require_toString = require("../util/toString.js");
//#region src/compat/string/deburr.ts
/**
* Converts a string by replacing special characters and diacritical marks with their ASCII equivalents.
* For example, "Crème brûlée" becomes "Creme brulee".
*
* @param str - The input string to be deburred.
* @returns The deburred string with special characters replaced by their ASCII equivalents.
*
* @example
* // Basic usage:
* deburr('Æthelred') // returns 'Aethelred'
*
* @example
* // Handling diacritical marks:
* deburr('München') // returns 'Munchen'
*
* @example
* // Special characters:
* deburr('Crème brûlée') // returns 'Creme brulee'
*/
function deburr(str) {
	return require_deburr.deburr(require_toString.toString(str));
}
//#endregion
exports.deburr = deburr;
