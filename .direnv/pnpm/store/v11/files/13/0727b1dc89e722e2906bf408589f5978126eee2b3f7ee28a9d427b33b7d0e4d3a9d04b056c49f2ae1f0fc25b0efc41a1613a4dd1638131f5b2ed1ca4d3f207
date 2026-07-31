const require_identity = require("../../function/identity.js");
const require_iteratee = require("../util/iteratee.js");
const require_toInteger = require("../util/toInteger.js");
//#region src/compat/array/findLast.ts
/**
* Finds the last item in an object that has a specific property, where the property name is provided as a PropertyKey.
*
* @template T
* @param source - The source array or object to search through.
* @param doesMatch - The criteria to match. It can be a function, a partial object, a key-value pair, or a property name.
* @param [fromIndex] - The index to start the search from, defaults to source.length-1 for arrays or Object.keys(source).length-1 for objects.
* @returns The last property value that has the specified property, or `undefined` if no match is found.
*
* @example
* // Using a property name
* const obj = { a: { id: 1, name: 'Alice' }, b: { id: 2 }, c: { id: 3, name: 'Bob' } };
* const result = findLast(obj, 'name');
* console.log(result); // { id: 3, name: 'Bob' }
*/
function findLast(source, _doesMatch = require_identity.identity, fromIndex) {
	if (!source) return;
	const length = Array.isArray(source) ? source.length : Object.keys(source).length;
	fromIndex = require_toInteger.toInteger(fromIndex ?? length - 1);
	if (fromIndex < 0) fromIndex = Math.max(length + fromIndex, 0);
	else fromIndex = Math.min(fromIndex, length - 1);
	const doesMatch = require_iteratee.iteratee(_doesMatch);
	if (!Array.isArray(source)) {
		const keys = Object.keys(source);
		for (let i = fromIndex; i >= 0; i--) {
			const key = keys[i];
			const value = source[key];
			if (doesMatch(value, key, source)) return value;
		}
		return;
	}
	return source.slice(0, fromIndex + 1).findLast(doesMatch);
}
//#endregion
exports.findLast = findLast;
