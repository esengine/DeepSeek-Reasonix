const require_identity = require("../../function/identity.js");
const require_iteratee = require("../util/iteratee.js");
//#region src/compat/array/find.ts
/**
* Finds the first item in an object that has a specific property, where the property name is provided as a PropertyKey.
*
* @template T
* @param source - The source array or object to search through.
* @param doesMatch - The criteria to match. It can be a function, a partial object, a key-value pair, or a property name.
* @param [fromIndex=0] - The index to start the search from, defaults to 0.
* @returns The first property value that has the specified property, or `undefined` if no match is found.
*
* @example
* // Using a property name
* const obj = { a: { id: 1, name: 'Alice' }, b: { id: 2, name: 'Bob' } };
* const result = find(obj, 'name');
* console.log(result); // { id: 1, name: 'Alice' }
*/
function find(source, _doesMatch = require_identity.identity, fromIndex = 0) {
	if (!source) return;
	if (fromIndex < 0) fromIndex = Math.max(source.length + fromIndex, 0);
	const doesMatch = require_iteratee.iteratee(_doesMatch);
	if (!Array.isArray(source)) {
		const keys = Object.keys(source);
		for (let i = fromIndex; i < keys.length; i++) {
			const key = keys[i];
			const value = source[key];
			if (doesMatch(value, key, source)) return value;
		}
		return;
	}
	return source.slice(fromIndex).find(doesMatch);
}
//#endregion
exports.find = find;
