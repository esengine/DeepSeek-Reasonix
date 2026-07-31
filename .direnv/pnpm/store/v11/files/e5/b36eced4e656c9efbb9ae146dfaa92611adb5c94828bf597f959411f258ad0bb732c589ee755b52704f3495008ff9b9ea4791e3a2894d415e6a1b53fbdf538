import { isEqualsSameValueZero } from "../../_internal/isEqualsSameValueZero.mjs";
import "../util/eq.mjs";
import { keysIn } from "./keysIn.mjs";
//#region src/compat/object/assignIn.ts
/**
* Assigns properties from multiple source objects to a target object.
*
* This function merges the properties of the source objects into the target object,
* including properties from the prototype chain. If a property in the source objects
* is equal to the corresponding property in the target object, it will not be overwritten.
*
* @param object - The target object to which properties will be assigned.
* @param sources - The source objects whose properties will be assigned to the target object.
* @returns The updated target object with properties from the source objects assigned.
*
* @example
* const target = { a: 1 };
* const result = assignIn(target, { b: 2 }, { c: 3 });
* console.log(result); // Output: { a: 1, b: 2, c: 3 }
*/
function assignIn(object, ...sources) {
	for (let i = 0; i < sources.length; i++) assignInImpl(object, sources[i]);
	return object;
}
function assignInImpl(object, source) {
	const keys = keysIn(source);
	for (let i = 0; i < keys.length; i++) {
		const key = keys[i];
		if (!(key in object) || !isEqualsSameValueZero(object[key], source[key])) object[key] = source[key];
	}
}
//#endregion
export { assignIn };
