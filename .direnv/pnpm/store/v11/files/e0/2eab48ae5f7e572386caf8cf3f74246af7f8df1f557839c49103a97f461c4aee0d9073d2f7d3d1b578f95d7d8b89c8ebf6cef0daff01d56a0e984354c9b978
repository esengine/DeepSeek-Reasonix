const require_identity = require("../../function/identity.js");
const require_property = require("../object/property.js");
const require_matches = require("../predicate/matches.js");
const require_matchesProperty = require("../predicate/matchesProperty.js");
//#region src/compat/array/some.ts
/**
* Checks if there is an element in an array that matches the given predicate.
*
* Iteration is stopped once there is an element that matches `predicate`.
*
* @template T
* @param source The source to iterate over.
* @param [predicate=identity] The function invoked per iteration.
* If a property name or an object is provided it will be used to create a predicate function.
* @returns Returns `true` if any element passes the predicate check, else `false`.
*
* @example
* some([1, 2, 3, 4], n => n % 2 === 0);
* // => true
*
* some([{ a: 1 }, { a: 2 }, { a: 3 }], { a: 2 });
* // => true
*
* some([{ a: 1 }, { a: 2 }, { a: 3 }], ['a', 2]);
* // => true
*
* some([{ a: 1 }, { a: 2 }, { a: 3 }], 'a');
* // => true
*
* some({ a: 1, b: 2, c: 3 }, n => n % 2 === 0);
* // => true
*
* some({ a: { id: 1, name: 'Alice' }, b: { id: 2, name: 'Bob' } }, { name: 'Bob' });
* // => true
*
* some({ a: { id: 1, name: 'Alice' }, b: { id: 2, name: 'Bob' } }, ['name', 'Alice']);
* // => true
*
* some({ a: { id: 1, name: 'Alice' }, b: { id: 2, name: 'Bob' } }, 'name');
* // => true
*/
function some(source, predicate, guard) {
	if (!source) return false;
	if (guard != null) predicate = void 0;
	if (predicate == null) predicate = require_identity.identity;
	const values = Array.isArray(source) ? source : Object.values(source);
	switch (typeof predicate) {
		case "function":
			if (!Array.isArray(source)) {
				const keys = Object.keys(source);
				for (let i = 0; i < keys.length; i++) {
					const key = keys[i];
					const value = source[key];
					if (predicate(value, key, source)) return true;
				}
				return false;
			}
			for (let i = 0; i < source.length; i++) if (predicate(source[i], i, source)) return true;
			return false;
		case "object": if (Array.isArray(predicate) && predicate.length === 2) {
			const key = predicate[0];
			const value = predicate[1];
			const matchFunc = require_matchesProperty.matchesProperty(key, value);
			if (Array.isArray(source)) {
				for (let i = 0; i < source.length; i++) if (matchFunc(source[i])) return true;
				return false;
			}
			return values.some(matchFunc);
		} else {
			const matchFunc = require_matches.matches(predicate);
			if (Array.isArray(source)) {
				for (let i = 0; i < source.length; i++) if (matchFunc(source[i])) return true;
				return false;
			}
			return values.some(matchFunc);
		}
		case "number":
		case "symbol":
		case "string": {
			const propFunc = require_property.property(predicate);
			if (Array.isArray(source)) {
				for (let i = 0; i < source.length; i++) if (propFunc(source[i])) return true;
				return false;
			}
			return values.some(propFunc);
		}
	}
}
//#endregion
exports.some = some;
