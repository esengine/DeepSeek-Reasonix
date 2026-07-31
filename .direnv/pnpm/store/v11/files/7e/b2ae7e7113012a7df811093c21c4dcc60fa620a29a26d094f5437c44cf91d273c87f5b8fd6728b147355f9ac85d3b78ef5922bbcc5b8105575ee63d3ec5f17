import { isObject } from "../predicate/isObject.mjs";
import { assignValue } from "../_internal/assignValue.mjs";
import { keys } from "./keys.mjs";
//#region src/compat/object/create.ts
/**
* Creates an object that inherits from the prototype object.
*
* If `properties` are provided, they will be added to the new object.
* Only string-keyed enumerable properties directly owned by the `properties` object are copied.
* Inherited properties or those with `Symbol` keys are not copied.
*
* @template T - The prototype object type.
* @template U - The properties object type.
* @param prototype - The object to inherit from.
* @param properties - The properties to assign to the created object.
* @returns The new object.
*/
function create(prototype, properties) {
	const proto = isObject(prototype) ? Object.create(prototype) : {};
	if (properties != null) {
		const propsKeys = keys(properties);
		for (let i = 0; i < propsKeys.length; i++) {
			const key = propsKeys[i];
			const propsValue = properties[key];
			assignValue(proto, key, propsValue);
		}
	}
	return proto;
}
//#endregion
export { create };
