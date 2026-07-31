const require_range = require("../../math/range.js");
const require_isArrayLike = require("../predicate/isArrayLike.js");
const require_iteratee = require("../util/iteratee.js");
const require_isSymbol = require("../predicate/isSymbol.js");
const require_identity = require("../function/identity.js");
const require_keysIn = require("./keysIn.js");
const require_getSymbolsIn = require("../_internal/getSymbolsIn.js");
//#region src/compat/object/pickBy.ts
/**
* Creates a new object composed of the properties that satisfy the predicate function.
*
* This function takes an object and a predicate function, and returns a new object that
* includes only the properties for which the predicate function returns true.
*
* @template T - The type of object.
* @param obj - The object to pick properties from.
* @param [shouldPick] - A predicate function that determines
* whether a property should be picked. It takes the property's key and value as arguments and returns `true`
* if the property should be picked, and `false` otherwise.
* @returns A new object with the properties that satisfy the predicate function.
*
* @example
* const obj = { a: 1, b: 'pick', c: 3 };
* const shouldPick = (value) => typeof value === 'string';
* const result = pickBy(obj, shouldPick);
* // result will be { b: 'pick' }
*/
function pickBy(obj, shouldPick) {
	if (obj == null) return {};
	const predicate = require_iteratee.iteratee(shouldPick ?? require_identity.identity);
	const result = {};
	const keys = require_isArrayLike.isArrayLike(obj) ? require_range.range(0, obj.length) : [...require_keysIn.keysIn(obj), ...require_getSymbolsIn.getSymbolsIn(obj)];
	for (let i = 0; i < keys.length; i++) {
		const key = require_isSymbol.isSymbol(keys[i]) ? keys[i] : keys[i].toString();
		const value = obj[key];
		if (predicate(value, key, obj)) result[key] = value;
	}
	return result;
}
//#endregion
exports.pickBy = pickBy;
