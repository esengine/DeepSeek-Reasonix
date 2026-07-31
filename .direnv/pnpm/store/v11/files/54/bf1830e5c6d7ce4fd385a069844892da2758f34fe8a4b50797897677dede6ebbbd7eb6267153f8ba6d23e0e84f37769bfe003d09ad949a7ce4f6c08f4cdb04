//#region src/compat/util/constant.ts
/**
* Creates a new function that always returns `value`.
*
* @template T - The type of the value to return.
* @param value - The value to return from the new function.
* @returns Returns the new constant function.
*
* @example
* const object = { a: 1 };
* const returnsObject = constant(object);
*
* returnsObject(); // => { a: 1 }
* returnsObject() === object; // => true
*/
function constant(value) {
	return () => value;
}
//#endregion
exports.constant = constant;
