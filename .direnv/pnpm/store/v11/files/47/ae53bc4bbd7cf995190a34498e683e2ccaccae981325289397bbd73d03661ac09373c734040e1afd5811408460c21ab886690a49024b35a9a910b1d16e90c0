import { keysIn } from "../object/keysIn.mjs";
//#region src/compat/util/toPlainObject.ts
/**
* Converts value to a plain object flattening inherited enumerable string keyed properties of value to own properties of the plain object.
*
* @param value The value to convert.
* @returns Returns the converted plain object.
*
* @example
* function Foo() {
*   this.b = 2;
* }
* Foo.prototype.c = 3;
* toPlainObject(new Foo()); // { b: 2, c: 3 }
*/
function toPlainObject(value) {
	const plainObject = {};
	const valueKeys = keysIn(value);
	for (let i = 0; i < valueKeys.length; i++) {
		const key = valueKeys[i];
		const objValue = value[key];
		if (key === "__proto__") Object.defineProperty(plainObject, key, {
			configurable: true,
			enumerable: true,
			value: objValue,
			writable: true
		});
		else plainObject[key] = objValue;
	}
	return plainObject;
}
//#endregion
export { toPlainObject };
