import { isEqualsSameValueZero } from "../../_internal/isEqualsSameValueZero.mjs";
import "../util/eq.mjs";
import { isNil } from "../../predicate/isNil.mjs";
import { isIterateeCall } from "../_internal/isIterateeCall.mjs";
//#region src/compat/object/defaults.ts
/**
* Assigns default values to an `object`, ensuring that certain properties do not remain `undefined`.
* It sets default values for properties that are either `undefined` or inherited from `Object.prototype`.
*
* You can pass in multiple objects to define these default values,
* and they will be applied in order from left to right.
* Once a property has been assigned a value, any subsequent values for that property will be ignored.
*
* Note: This function modifies the first argument, `object`. If you want to keep `object` unchanged, consider using `toDefaulted` instead.
*
* @template T - The type of the object being processed.
* @template S - The type of the objects that provides default values.
* @param object - The target object that will receive default values.
* @param source - The objects that specifies the default values to apply.
* @returns The `object` that has been updated with default values from `sources`, ensuring that all properties are defined and none are left as `undefined`.
*
* @example
* defaults({ a: 1 }, { a: 2, b: 2 }, { c: 3 }); // { a: 1, b: 2, c: 3 }
* defaults({ a: 1, b: 2 }, { b: 3 }, { c: 3 }); // { a: 1, b: 2, c: 3 }
* defaults({ a: null }, { a: 1 }); // { a: null }
* defaults({ a: undefined }, { a: 1 }); // { a: 1 }
*/
function defaults(object, ...sources) {
	object = Object(object);
	const objectProto = Object.prototype;
	let length = sources.length;
	const guard = length > 2 ? sources[2] : void 0;
	if (guard && isIterateeCall(sources[0], sources[1], guard)) length = 1;
	for (let i = 0; i < length; i++) {
		if (isNil(sources[i])) continue;
		const source = sources[i];
		const keys = Object.keys(source);
		for (let j = 0; j < keys.length; j++) {
			const key = keys[j];
			const value = object[key];
			if (value === void 0 || !Object.hasOwn(object, key) && isEqualsSameValueZero(value, objectProto[key])) object[key] = source[key];
		}
	}
	return object;
}
//#endregion
export { defaults };
