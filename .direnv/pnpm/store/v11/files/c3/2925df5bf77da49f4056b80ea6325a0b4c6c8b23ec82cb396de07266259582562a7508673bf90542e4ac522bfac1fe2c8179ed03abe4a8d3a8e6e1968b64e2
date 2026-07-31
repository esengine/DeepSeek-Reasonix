//#region src/fp/_internal/lazy.ts
/**
* Pairs an eager data-last function with a lazy transform (see
* {@link createLazyFunction}).
*
* The returned function behaves exactly like `eager` when called directly. When
* placed inside a {@link pipe}, the attached transform lets `pipe` fuse it with
* adjacent lazy functions into a single, short-circuiting pass.
*
* @template Eager - The eager data-last function type.
* @param eager - The eager `(array) => result` implementation.
* @param lazy - The lazy transform built by {@link createLazyFunction}.
* @param options - Set `shortCircuit: true` for functions that can end a run early.
* @returns `eager`, augmented in place with the lazy metadata.
*/
function combineEagerAndLazyFunctions(eager, lazy, { shortCircuit } = {}) {
	return Object.assign(eager, {
		lazy,
		shortCircuit
	});
}
/**
* Builds a {@link LazyTransform} from a per-element `step`.
*
* `step` receives each input `value`, its zero-based `index` within this stage,
* and an `emit` callback that forwards a result to the next stage. Call `emit`
* zero times (drop), once (`map`), or many times (`flatMap`). Return
* `false` to end the run after this element (used by short-circuiting functions
* like `take`); returning nothing continues.
*
* @template T - The type of values entering this stage.
* @template U - The type of values it emits.
* @param step - The per-element push function.
* @returns A lazy transform that `pipe` can fuse and drive.
*
* @example
* // map
* createLazyFunction<T, U>((value, index, emit) => {
*   emit(callback(value, index));
* });
*/
function createLazyFunction(step) {
	return (emit) => {
		let index = 0;
		let active = true;
		const _emit = (value) => {
			if (!active) return;
			if (emit(value) === false) active = false;
		};
		return (value) => {
			if (step(value, index++, _emit) === false) active = false;
			return active;
		};
	};
}
//#endregion
export { combineEagerAndLazyFunctions, createLazyFunction };
