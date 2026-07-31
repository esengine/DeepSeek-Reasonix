import { drop as drop$1 } from "../../array/drop.mjs";
import { toArray } from "../_internal/toArray.mjs";
import { isArrayLike } from "../predicate/isArrayLike.mjs";
import { toInteger } from "../util/toInteger.mjs";
//#region src/compat/array/drop.ts
function drop(array, itemsCount = 1, guard) {
	if (!isArrayLike(array)) return [];
	itemsCount = guard ? 1 : toInteger(itemsCount);
	return drop$1(toArray(array), itemsCount);
}
//#endregion
export { drop };
