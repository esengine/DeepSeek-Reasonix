//#region src/fp/math/add.ts
/**
* Creates a function that adds `addend` to its input. Use it with {@link pipe},
* or as the callback of a function such as {@link map}.
*
* @param addend - The number to add to the input.
* @returns A function that maps a `number` to `value + addend`.
*
* @example
* import { pipe, map, add } from 'es-toolkit/fp';
*
* pipe(3, add(2)); // => 5
* pipe([1, 2, 3], map(add(10))); // => [11, 12, 13]
*/
function add(addend) {
	return function(value) {
		return value + addend;
	};
}
//#endregion
export { add };
