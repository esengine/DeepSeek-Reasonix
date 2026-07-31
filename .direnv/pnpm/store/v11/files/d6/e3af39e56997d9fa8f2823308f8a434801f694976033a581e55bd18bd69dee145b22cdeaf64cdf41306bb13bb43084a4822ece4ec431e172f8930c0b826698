//#region src/predicate/isNotNil.ts
/**
* Checks if the given value is not null nor undefined.
*
* The main use of this function is to be used with TypeScript as a type predicate.
*
* @template T - The type of value.
* @param x - The value to test if it is not null nor undefined.
* @returns True if the value is not null nor undefined, false otherwise.
*
* @example
* // Here the type of `arr` is (number | undefined)[]
* const arr = [1, undefined, 3];
* // Here the type of `result` is number[]
* const result = arr.filter(isNotNil);
* // result will be [1, 3]
*/
function isNotNil(x) {
	return x != null;
}
//#endregion
export { isNotNil };
