//#region src/predicate/isTypedArray.ts
/**
* Checks if a value is a TypedArray.
* @param x The value to check.
* @returns Returns true if `x` is a TypedArray, false otherwise.
*
* @example
* const arr = new Uint8Array([1, 2, 3]);
* isTypedArray(arr); // true
*
* const regularArray = [1, 2, 3];
* isTypedArray(regularArray); // false
*
* const buffer = new ArrayBuffer(16);
* isTypedArray(buffer); // false
*/
function isTypedArray(x) {
	return ArrayBuffer.isView(x) && !(x instanceof DataView);
}
//#endregion
export { isTypedArray };
