import { tail as tail$1 } from "../../array/tail.mjs";
//#region src/fp/array/tail.ts
/**
* Creates a function that returns every element except the first one.
*
* Empty and single-element arrays return an empty array, matching the main {@link tail}
* behavior. Use the returned function with {@link pipe}.
*
* @template T - The type of elements in the array.
* @returns A function that maps a readonly array to its tail elements.
*
* @example
* import { pipe, tail } from 'es-toolkit/fp';
*
* pipe([1, 2, 3], tail());
* // => [2, 3]
*/
function tail() {
	return function(array) {
		return tail$1(array);
	};
}
//#endregion
export { tail };
