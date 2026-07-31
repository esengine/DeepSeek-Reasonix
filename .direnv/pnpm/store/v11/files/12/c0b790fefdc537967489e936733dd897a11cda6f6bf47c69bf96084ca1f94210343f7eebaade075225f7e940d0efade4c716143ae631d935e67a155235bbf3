import { toKey } from "../_internal/toKey.mjs";
import { toPath } from "./toPath.mjs";
import { get } from "../object/get.mjs";
import { last } from "../array/last.mjs";
//#region src/compat/util/invoke.ts
/**
* Invokes the method at `path` of `object` with the given arguments.
*
* @param object - The object to query.
* @param path - The path of the method to invoke.
* @param args - The arguments to invoke the method with.
* @returns Returns the result of the invoked method.
*
* @example
* const object = {
*   a: {
*     b: function (x, y) {
*       return x + y;
*     }
*   }
* };
*
* invoke(object, 'a.b', [1, 2]); // => 3
* invoke(object, ['a', 'b'], [1, 2]); // => 3
*/
function invoke(object, path, ...args) {
	args = args.flat(1);
	if (object == null) return;
	switch (typeof path) {
		case "string":
			if (typeof object === "object" && Object.hasOwn(object, path)) return invokeImpl(object, [path], args);
			return invokeImpl(object, toPath(path), args);
		case "number":
		case "symbol": return invokeImpl(object, [path], args);
		default: if (Array.isArray(path)) return invokeImpl(object, path, args);
		else return invokeImpl(object, [path], args);
	}
}
function invokeImpl(object, path, args) {
	const parent = get(object, path.slice(0, -1), object);
	if (parent == null) return;
	let lastKey = last(path);
	const lastValue = lastKey?.valueOf();
	if (typeof lastValue === "number") lastKey = toKey(lastValue);
	else lastKey = String(lastKey);
	return get(parent, lastKey)?.apply(parent, args);
}
//#endregion
export { invoke };
