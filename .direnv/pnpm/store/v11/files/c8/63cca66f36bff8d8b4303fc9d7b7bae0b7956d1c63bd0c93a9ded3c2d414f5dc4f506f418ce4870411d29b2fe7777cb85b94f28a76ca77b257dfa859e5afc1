import { shuffle as shuffle$1 } from "../../array/shuffle.mjs";
import { isArray } from "../predicate/isArray.mjs";
import { isArrayLike } from "../predicate/isArrayLike.mjs";
import { isObjectLike } from "../predicate/isObjectLike.mjs";
import { values } from "../object/values.mjs";
import { isNil } from "../predicate/isNil.mjs";
//#region src/compat/array/shuffle.ts
/**
* Randomizes the order of elements in an `collection` using the Fisher-Yates algorithm.
*
* This function takes an `collection` and returns a new `collection` with its elements shuffled in a random order.
*
* @template T - The type of elements in the `collection`.
* @param collection - The `collection` to shuffle.
* @returns A new `collection` with its elements shuffled in random order.
*/
function shuffle(collection) {
	if (isNil(collection)) return [];
	if (isArray(collection)) return shuffle$1(collection);
	if (isArrayLike(collection)) return shuffle$1(Array.from(collection));
	if (isObjectLike(collection)) return shuffle$1(values(collection));
	return [];
}
//#endregion
export { shuffle };
