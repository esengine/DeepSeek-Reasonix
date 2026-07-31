import { isArray } from "../predicate/isArray.mjs";
import { isFunction } from "../../predicate/isFunction.mjs";
import { toString } from "./toString.mjs";
import { isObject } from "../predicate/isObject.mjs";
//#region src/compat/util/bindAll.ts
/**
* Binds methods of an object to the object itself, overwriting the existing method.
* Method names may be specified as individual arguments or as arrays of method names.
*
* @template T - The type of the object.
* @param object - The object to bind methods to.
* @param [methodNames] - The method names to bind, specified individually or in arrays.
* @returns Returns the object.
*
* @example
* const view = {
*   'label': 'docs',
*   'click': function() {
*     console.log('clicked ' + this.label);
*   }
* };
*
* bindAll(view, ['click']);
* jQuery(element).on('click', view.click);
* // => Logs 'clicked docs' when clicked.
*
* @example
* // Using individual method names
* bindAll(view, 'click');
* // => Same as above
*/
function bindAll(object, ...methodNames) {
	if (object == null) return object;
	if (!isObject(object)) return object;
	if (isArray(object) && methodNames.length === 0) return object;
	const methods = [];
	for (let i = 0; i < methodNames.length; i++) {
		const name = methodNames[i];
		if (isArray(name)) methods.push(...name);
		else if (name && typeof name === "object" && "length" in name) methods.push(...Array.from(name));
		else methods.push(name);
	}
	if (methods.length === 0) return object;
	for (let i = 0; i < methods.length; i++) {
		const key = methods[i];
		const stringKey = toString(key);
		const func = object[stringKey];
		if (isFunction(func)) object[stringKey] = func.bind(object);
	}
	return object;
}
//#endregion
export { bindAll };
