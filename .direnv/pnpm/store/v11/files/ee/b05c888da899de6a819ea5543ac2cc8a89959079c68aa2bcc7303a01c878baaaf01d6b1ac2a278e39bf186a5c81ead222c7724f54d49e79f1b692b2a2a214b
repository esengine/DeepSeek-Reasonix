//#region src/function/memoize.ts
/**
* Creates a memoized version of the provided function. The memoized function caches
* results based on the argument it receives, so if the same argument is passed again,
* it returns the cached result instead of recomputing it.
*
* This function works with functions that take zero or just one argument. If your function
* originally takes multiple arguments, you should refactor it to take a single object or array
* that combines those arguments.
*
* If the argument is not primitive (e.g., arrays or objects), provide a
* `getCacheKey` function to generate a unique cache key for proper caching.
*
* @template F - The type of the function to be memoized.
* @param fn - The function to be memoized. It should accept a single argument and return a value.
* @param [options={}] - Optional configuration for the memoization.
* @param [options.cache] - The cache object used to store results. Defaults to a new `Map`.
* @param [options.getCacheKey] - An optional function to generate a unique cache key for each argument.
*
* @returns The memoized function with an additional `cache` property that exposes the internal cache.
*
* @example
* // Example using the default cache
* const add = (x: number) => x + 10;
* const memoizedAdd = memoize(add);
*
* console.log(memoizedAdd(5)); // 15
* console.log(memoizedAdd(5)); // 15 (cached result)
* console.log(memoizedAdd.cache.size); // 1
*
* @example
* // Example using a custom resolver
* const sum = (arr: number[]) => arr.reduce((x, y) => x + y, 0);
* const memoizedSum = memoize(sum, { getCacheKey: (arr: number[]) => arr.join(',') });
* console.log(memoizedSum([1, 2])); // 3
* console.log(memoizedSum([1, 2])); // 3 (cached result)
* console.log(memoizedSum.cache.size); // 1
*
* @example
* // Example using a custom cache implementation
* class CustomCache<K, T> implements MemoizeCache<K, T> {
*   private cache = new Map<K, T>();
*
*   set(key: K, value: T): void {
*     this.cache.set(key, value);
*   }
*
*   get(key: K): T | undefined {
*     return this.cache.get(key);
*   }
*
*   has(key: K): boolean {
*     return this.cache.has(key);
*   }
*
*   delete(key: K): boolean {
*     return this.cache.delete(key);
*   }
*
*   clear(): void {
*     this.cache.clear();
*   }
*
*   get size(): number {
*     return this.cache.size;
*   }
* }
* const customCache = new CustomCache<string, number>();
* const memoizedSumWithCustomCache = memoize(sum, { cache: customCache });
* console.log(memoizedSumWithCustomCache([1, 2])); // 3
* console.log(memoizedSumWithCustomCache([1, 2])); // 3 (cached result)
* console.log(memoizedSumWithCustomCache.cache.size); // 1
*/
function memoize(fn, options = {}) {
	const { cache = /* @__PURE__ */ new Map(), getCacheKey } = options;
	const memoizedFn = function(arg) {
		const key = getCacheKey ? getCacheKey(arg) : arg;
		if (cache.has(key)) return cache.get(key);
		const result = fn.call(this, arg);
		cache.set(key, result);
		return result;
	};
	memoizedFn.cache = cache;
	return memoizedFn;
}
//#endregion
exports.memoize = memoize;
