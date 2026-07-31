//#region src/compat/function/memoize.ts
/**
* Creates a function that memoizes the result of func. If resolver is provided it determines the cache key for
* storing the result based on the arguments provided to the memoized function. By default, the first argument
* provided to the memoized function is coerced to a string and used as the cache key. The func is invoked with
* the this binding of the memoized function.
*
* @template T - The type of the original function being memoized
* @param func The function to have its output memoized.
* @param [resolver] The function to resolve the cache key.
* @return {MemoizedFunction<T>} Returns the new memoizing function.
*/
function memoize(func, resolver) {
	if (typeof func !== "function" || resolver != null && typeof resolver !== "function") throw new TypeError("Expected a function");
	const memoized = function(...args) {
		const key = resolver ? resolver.apply(this, args) : args[0];
		const cache = memoized.cache;
		if (cache.has(key)) return cache.get(key);
		const result = func.apply(this, args);
		memoized.cache = cache.set(key, result) || cache;
		return result;
	};
	memoized.cache = new (memoize.Cache || Map)();
	return memoized;
}
memoize.Cache = Map;
//#endregion
export { memoize };
