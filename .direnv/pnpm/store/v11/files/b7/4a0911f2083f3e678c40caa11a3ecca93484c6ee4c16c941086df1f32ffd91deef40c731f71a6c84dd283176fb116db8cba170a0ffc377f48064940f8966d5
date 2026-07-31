//#region src/fp/pipe.ts
/**
* Performs left-to-right function composition, threading `value` through each
* function in sequence. Every function receives the output of the previous
* one, so the data flows top-to-bottom in the same order it is read. This
* turns deeply nested calls into clear, sequential steps without temporary
* variables.
*
* This is the entry point of `es-toolkit/fp`. Each `es-toolkit/fp` function is
* called with its configuration (e.g. `map(fn)`, `take(2)`) and returns a
* function that takes the data, which `pipe` then supplies.
*
* When consecutive **lazy-capable** functions (`map`, `filter`, `take`, ...)
* appear together, `pipe` fuses them and processes the input element-by-element
* instead of building an intermediate array after every step. This allows
* early termination: a trailing `take(n)` stops the walk as soon as `n`
* results exist, so the remaining input is never visited.
*
* @param value - The initial value fed into the pipe.
* @param functions - Data-last operators (or any unary functions) applied in order.
* @returns The result of applying every function, left to right.
*
* @example
* import { pipe, map, filter, take } from 'es-toolkit/fp';
*
* pipe([1, 2, 3], map(x => x * 3)); // => [3, 6, 9]
*
* @example
* // Lazy evaluation with early termination: only the first two even squares
* // are ever computed, the rest of the array is never touched.
* pipe(
*   [1, 2, 3, 4, 5, 6, 7, 8],
*   map(x => x * x),
*   filter(x => x % 2 === 0),
*   take(2)
* ); // => [4, 16]
*
* @example
* // Any unary function works inside a pipe, not just es-toolkit operators.
* pipe(
*   '  Hello  ',
*   s => s.trim(),
*   s => s.toLowerCase()
* ); // => 'hello'
*/
function pipe(value, ...functions) {
	let output = value;
	const groups = chunkFunctions(functions);
	for (let groupIndex = 0; groupIndex < groups.length; groupIndex++) {
		const group = groups[groupIndex];
		if (group.lazy && isIterable(output) && (group.shortCircuit || !Array.isArray(output))) output = lazyPipe(output, group);
		else for (let index = 0; index < group.length; index++) output = group[index](output);
	}
	return output;
}
function chunkFunctions(functions) {
	if (functions.length === 0) return [];
	let currentGroup = [functions[0]];
	const result = [currentGroup];
	let previousIsLazy = currentGroup[0].lazy != null;
	if (previousIsLazy) currentGroup.lazy = true;
	if (currentGroup[0].shortCircuit) currentGroup.shortCircuit = true;
	for (let index = 1; index < functions.length; index++) {
		const func = functions[index];
		const isLazy = func.lazy != null;
		if (isLazy !== previousIsLazy) {
			currentGroup = [func];
			result.push(currentGroup);
		} else currentGroup.push(func);
		if (isLazy) currentGroup.lazy = true;
		if (func.shortCircuit) currentGroup.shortCircuit = true;
		previousIsLazy = isLazy;
	}
	return result;
}
/**
* Fuses a run of lazy functions into a single pass over `data`. The functions'
* push transforms are composed (last function first) into one sink, then `data`
* is driven through it element-by-element. A short-circuiting function (e.g.
* `take`) returns `false`, which stops the drive loop at once, so the functions
* before it never run on the rest of the input — no intermediate arrays.
*
* @param data - The iterable fed into the run (an array, `Set`, generator, ...).
* @param lazyFunctions - The consecutive lazy functions to fuse, in order.
* @returns The collected results.
*/
function lazyPipe(data, lazyFunctions) {
	const result = [];
	let sink = (value) => {
		result.push(value);
		return true;
	};
	for (let index = lazyFunctions.length - 1; index >= 0; index--) sink = lazyFunctions[index].lazy(sink);
	if (Array.isArray(data)) {
		for (let index = 0; index < data.length; index++) if (sink(data[index]) === false) break;
	} else {
		const iterator = data[Symbol.iterator]();
		let step = iterator.next();
		while (!step.done) {
			if (sink(step.value) === false) break;
			step = iterator.next();
		}
	}
	return result;
}
/**
* Whether `value` can be fed through the lazy path. A non-array iterable (a
* `Set`, a generator, ...) can only go through `lazyPipe`, since the eager path
* relies on array methods. Primitives and plain objects (no `Symbol.iterator`)
* fall through to the eager branch.
*/
function isIterable(value) {
	return typeof value === "object" && value !== null && Symbol.iterator in value;
}
//#endregion
export { pipe };
