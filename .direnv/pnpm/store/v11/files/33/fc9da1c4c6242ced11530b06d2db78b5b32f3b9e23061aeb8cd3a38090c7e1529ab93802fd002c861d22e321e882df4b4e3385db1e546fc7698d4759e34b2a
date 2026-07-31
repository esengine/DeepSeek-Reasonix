//#region src/function/negate.ts
/**
* Creates a function that negates the result of the predicate function.
*
* @template F - The type of the function to negate.
* @param func - The function to negate.
* @returns The new negated function, which negates the boolean result of `func`.
*
* @example
* const array = [1, 2, 3, 4, 5, 6];
* const isEven = (n: number) => n % 2 === 0;
* const result = array.filter(negate(isEven));
* // result will be [1, 3, 5]
*/
function negate(func) {
	return ((...args) => !func(...args));
}
//#endregion
export { negate };
