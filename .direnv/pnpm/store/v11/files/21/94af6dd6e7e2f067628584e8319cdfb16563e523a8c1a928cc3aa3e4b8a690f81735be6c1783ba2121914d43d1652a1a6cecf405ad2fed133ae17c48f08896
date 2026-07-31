const require_windowed = require("../../array/windowed.js");
const require_lazy = require("../_internal/lazy.js");
//#region src/fp/array/windowed.ts
/**
* Creates a function that returns sliding windows from an array.
*
* By default only full windows are returned. Pass partialWindows: true to include shorter
* trailing windows. The full-window form is lazy-capable inside {@link pipe}.
*
* @template T - The type of elements in the array.
* @param size - The size of each window. Must be a positive integer.
* @param step - The step between window starts. Defaults to 1.
* @param options - Window options.
* @returns A function that maps a readonly array to sliding windows.
* @throws {Error} When size or step is not a positive integer.
*
* @example
* import { pipe, windowed } from 'es-toolkit/fp';
*
* pipe([1, 2, 3, 4], windowed(2));
* // => [[1, 2], [2, 3], [3, 4]]
*/
function windowed(size, step, options) {
	const resolvedStep = step ?? 1;
	const partialWindows = options?.partialWindows ?? false;
	function windowedEager(array) {
		return require_windowed.windowed(array, size, step, options);
	}
	if (partialWindows || !Number.isInteger(size) || size <= 0 || !Number.isInteger(resolvedStep) || resolvedStep <= 0) return windowedEager;
	const windowedLazy = (emit) => {
		const buffer = [];
		let index = 0;
		return (value) => {
			buffer.push(value);
			if (buffer.length > size) buffer.shift();
			if (buffer.length === size) {
				if ((index - size + 1) % resolvedStep === 0) {
					const shouldContinue = emit(buffer.slice());
					index++;
					return shouldContinue;
				}
			}
			index++;
			return true;
		};
	};
	return require_lazy.combineEagerAndLazyFunctions(windowedEager, windowedLazy);
}
//#endregion
exports.windowed = windowed;
