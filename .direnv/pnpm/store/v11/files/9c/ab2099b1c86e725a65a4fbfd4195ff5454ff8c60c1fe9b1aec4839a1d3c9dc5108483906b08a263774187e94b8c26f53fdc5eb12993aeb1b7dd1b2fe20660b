import { identity } from "../../function/identity.mjs";
import { keys } from "./keys.mjs";
//#region src/compat/object/forOwnRight.ts
/**
* Iterates over an object's properties in reverse order and calls the `iteratee` function for each property.
*
* It only iterates over the object's own properties, not including inherited properties or properties with `Symbol` keys.
*
* The `iteratee` function can terminate the iteration early by returning `false`.
*
* @template T - The type of the object.
* @param object The object to iterate over.
* @param [iteratee=identity] The function invoked per iteration. If not provided, the identity function will be used.
* @return {T | null | undefined} Returns object.
*
* @example
* function Foo() {
*   this.a = 1;
*   this.b = 2;
* }
*
* Foo.prototype.c = 3;
*
* forOwnRight(new Foo(), function(value, key) {
*   console.log(key);
* });
* // => Logs 'b' then 'a' (iteration order is not guaranteed).
*/
function forOwnRight(object, iteratee = identity) {
	if (object == null) return object;
	const iterable = Object(object);
	const keys$1 = keys(object);
	for (let i = keys$1.length - 1; i >= 0; --i) {
		const key = keys$1[i];
		if (iteratee(iterable[key], key, iterable) === false) break;
	}
	return object;
}
//#endregion
export { forOwnRight };
