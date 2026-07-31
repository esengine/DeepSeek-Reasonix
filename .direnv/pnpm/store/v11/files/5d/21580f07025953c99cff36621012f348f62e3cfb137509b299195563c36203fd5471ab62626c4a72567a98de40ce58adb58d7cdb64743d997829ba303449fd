//#region src/predicate/isBlob.ts
/**
* Checks if the given value is a Blob.
*
* This function tests whether the provided value is an instance of `Blob`.
* It returns `true` if the value is an instance of `Blob`, and `false` otherwise.
*
* @param x - The value to test if it is a Blob.
* @returns True if the value is a Blob, false otherwise.
*
* @example
* const value1 = new Blob();
* const value2 = {};
*
* console.log(isBlob(value1)); // true
* console.log(isBlob(value2)); // false
*/
function isBlob(x) {
	if (typeof Blob === "undefined") return false;
	return x instanceof Blob;
}
//#endregion
export { isBlob };
