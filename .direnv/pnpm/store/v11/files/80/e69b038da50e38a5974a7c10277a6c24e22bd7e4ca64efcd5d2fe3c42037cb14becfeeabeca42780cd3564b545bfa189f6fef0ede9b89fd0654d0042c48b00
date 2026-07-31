import { dropWhile as dropWhile$1 } from "../../array/dropWhile.mjs";
import { combineEagerAndLazyFunctions } from "../_internal/lazy.mjs";
//#region src/fp/array/dropWhile.ts
/**
* Creates a function that removes leading values while a predicate returns true.
*
* Once the predicate returns false, that value and all following values are
* emitted. The returned function is lazy-capable inside {@link pipe}.
*
* @template T - The type of elements in the array.
* @param predicate - Called with each leading value and index while values are being dropped.
* @returns A function that maps the piped array to the remaining suffix.
*
* @example
* import { dropWhile, pipe } from 'es-toolkit/fp';
*
* pipe([1, 2, 3, 1], dropWhile(value => value < 3));
* // => [3, 1]
*/
function dropWhile(predicate) {
	function dropWhileEager(array) {
		return dropWhile$1(array, (item, index) => predicate(item, index));
	}
	const dropWhileLazy = (emit) => {
		let dropping = true;
		let index = 0;
		return (value) => {
			if (dropping && predicate(value, index++)) return true;
			dropping = false;
			return emit(value);
		};
	};
	return combineEagerAndLazyFunctions(dropWhileEager, dropWhileLazy);
}
//#endregion
export { dropWhile };
