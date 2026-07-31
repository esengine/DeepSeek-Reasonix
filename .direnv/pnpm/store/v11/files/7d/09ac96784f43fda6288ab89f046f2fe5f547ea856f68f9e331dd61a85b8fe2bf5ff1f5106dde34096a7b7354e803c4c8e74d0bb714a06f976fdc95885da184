const require_isObject = require("../predicate/isObject.js");
const require_assignValue = require("../_internal/assignValue.js");
const require_keys = require("./keys.js");
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
	const proto = require_isObject.isObject(prototype) ? Object.create(prototype) : {};
	if (properties != null) {
		const propsKeys = require_keys.keys(properties);
		for (let i = 0; i < propsKeys.length; i++) {
			const key = propsKeys[i];
			const propsValue = properties[key];
			require_assignValue.assignValue(proto, key, propsValue);
		}
	}
	return proto;
}
//#endregion
exports.create = create;
