import { cloneDeep } from "./cloneDeep.mjs";
import { defaults } from "./defaults.mjs";
//#region src/compat/object/toDefaulted.ts
/**
* Creates a new object based on the provided `object`, applying default values from the `sources` to ensure that no properties are left `undefined`.
* It assigns default values to properties that are either `undefined` or come from `Object.prototype`.
*
* You can provide multiple source objects to set these default values,
* and they will be applied in the order they are given, from left to right.
* Once a property has been set, any later values for that property will be ignored.
*
* Note: This function creates a new object. If you want to modify the `object`, use the `defaults` function instead.
*
* @template T - The type of the object being processed.
* @template S - The type of the objects that provides default values.
* @param object - The target object that will receive default values.
* @param sources - The objects that specifies the default values to apply.
* @returns A new object that combines the target and default values, ensuring no properties are left undefined.
*
* @example
* toDefaulted({ a: 1 }, { a: 2, b: 2 }, { c: 3 }); // { a: 1, b: 2, c: 3 }
* toDefaulted({ a: 1, b: 2 }, { b: 3 }, { c: 3 }); // { a: 1, b: 2, c: 3 }
* toDefaulted({ a: null }, { a: 1 }); // { a: null }
* toDefaulted({ a: undefined }, { a: 1 }); // { a: 1 }
*/
function toDefaulted(object, ...sources) {
	return defaults(cloneDeep(object), ...sources);
}
//#endregion
export { toDefaulted };
