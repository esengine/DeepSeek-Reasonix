const require_isArrayLike = require("../predicate/isArrayLike.js");
const require_get = require("./get.js");
const require_isString = require("../predicate/isString.js");
//#region src/compat/object/at.ts
/**
* Returns an array of values corresponding to `paths` of `object`.
*
* @template T - The type of the object.
* @param object - The object to iterate over.
* @param [paths] - The property paths to pick.
* @returns Returns the picked values.
*
* @example
* ```js
* const object = { 'a': [{ 'b': { 'c': 3 } }, 4] };
*
* at(object, ['a[0].b.c', 'a[1]']);
* // => [3, 4]
* ```
*/
function at(object, ...paths) {
	if (paths.length === 0) return [];
	const allPaths = [];
	for (let i = 0; i < paths.length; i++) {
		const path = paths[i];
		if (!require_isArrayLike.isArrayLike(path) || require_isString.isString(path)) {
			allPaths.push(path);
			continue;
		}
		for (let j = 0; j < path.length; j++) allPaths.push(path[j]);
	}
	const result = [];
	for (let i = 0; i < allPaths.length; i++) result.push(require_get.get(object, allPaths[i]));
	return result;
}
//#endregion
exports.at = at;
