//#region src/promise/allKeyed.ts
/**
* Resolves an object of promises concurrently, returning an object with the same keys and resolved values.
*
* Similar to `Promise.all`, but accepts an object of promises instead of an array,
* preserving the keys in the result. This makes it easy to destructure the resolved values
* by name instead of relying on positional indices.
*
* Based on the [TC39 `Promise.allKeyed` proposal](https://github.com/tc39/proposal-await-dictionary).
*
* @template T - A record type where each value is a promise or a value.
* @param tasks - An object whose values are promises (or plain values) to resolve concurrently.
* @returns>} A promise that resolves to an object with the same keys and resolved values.
*
* @example
* const { user, posts } = await allKeyed({
*   user: fetchUser(),
*   posts: fetchPosts(),
* });
*
* @example
* // Plain values are also supported
* const result = await allKeyed({
*   a: Promise.resolve(1),
*   b: 2,
* });
* // { a: 1, b: 2 }
*/
async function allKeyed(tasks) {
	const keys = Object.keys(tasks);
	const values = await Promise.all(keys.map((key) => tasks[key]));
	const result = {};
	for (let i = 0; i < keys.length; i++) result[keys[i]] = values[i];
	return result;
}
//#endregion
export { allKeyed };
