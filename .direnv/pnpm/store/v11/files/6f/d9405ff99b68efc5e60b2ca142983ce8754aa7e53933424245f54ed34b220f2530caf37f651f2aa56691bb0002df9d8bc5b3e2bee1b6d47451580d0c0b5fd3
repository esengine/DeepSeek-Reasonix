const require_isSymbol = require("../predicate/isSymbol.js");
//#region src/compat/util/toNumber.ts
/**
* Converts `value` to a number.
*
* Unlike `Number()`, this function returns `NaN` for symbols.
*
* @param value - The value to convert.
* @returns Returns the number.
*
* @example
* toNumber(3.2); // => 3.2
* toNumber(Number.MIN_VALUE); // => 5e-324
* toNumber(Infinity); // => Infinity
* toNumber('3.2'); // => 3.2
* toNumber(Symbol.iterator); // => NaN
* toNumber(NaN); // => NaN
*/
function toNumber(value) {
	if (require_isSymbol.isSymbol(value)) return NaN;
	return Number(value);
}
//#endregion
exports.toNumber = toNumber;
