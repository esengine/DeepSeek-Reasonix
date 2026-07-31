const require_cloneDeep = require("../../object/cloneDeep.js");
const require_isMatch = require("./isMatch.js");
//#region src/compat/predicate/matches.ts
/**
* Creates a function that performs a deep comparison between a given target and the source object.
*
* @template T
* @template V
* @param source - The source object to create the matcher from.
* @returns Returns a function that takes a target object and returns `true` if the target matches the source, otherwise `false`.
*
* @example
* // Basic usage
* const matcher = matches({ a: 1, b: 2 });
* matcher({ a: 1, b: 2, c: 3 }); // true
* matcher({ a: 1, c: 3 }); // false
*
* @example
* // Matching arrays
* const arrayMatcher = matches([1, 2, 3]);
* arrayMatcher([1, 2, 3, 4]); // true
* arrayMatcher([4, 5, 6]); // false
*
* @example
* // Matching objects with nested structures
* const nestedMatcher = matches({ a: { b: 2 } });
* nestedMatcher({ a: { b: 2, c: 3 } }); // true
* nestedMatcher({ a: { c: 3 } }); // false
*/
function matches(source) {
	source = require_cloneDeep.cloneDeep(source);
	return (target) => {
		return require_isMatch.isMatch(target, source);
	};
}
//#endregion
exports.matches = matches;
