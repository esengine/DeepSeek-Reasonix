const require_zipObject = require("../../array/zipObject.js");
//#region src/fp/array/zipObject.ts
/**
* Creates a function that builds an object from the piped keys and configured values.
*
* Keys are read from the piped array and paired with values by index, matching the main
* {@link zipObject} behavior.
*
* @template P - The property-key type from the piped keys.
* @template V - The type of configured values.
* @param values - Values assigned to the piped keys by index.
* @returns A function that maps keys to an object.
*
* @example
* import { pipe, zipObject } from 'es-toolkit/fp';
*
* pipe(['a', 'b'] as const, zipObject([1, 2]));
* // => { a: 1, b: 2 }
*/
function zipObject(values) {
	return function(keys) {
		return require_zipObject.zipObject(keys, values);
	};
}
//#endregion
exports.zipObject = zipObject;
