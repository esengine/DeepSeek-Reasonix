const require_cloneDeepWith = require("./cloneDeepWith.js");
//#region src/object/cloneDeep.ts
/**
* Creates a deep clone of the given object.
*
* @template T - The type of the object.
* @param obj - The object to clone.
* @returns A deep clone of the given object.
*
* @example
* // Clone a primitive value
* const num = 29;
* const clonedNum = cloneDeep(num);
* console.log(clonedNum); // 29
* console.log(clonedNum === num); // true
*
* @example
* // Clone an array
* const arr = [1, 2, 3];
* const clonedArr = cloneDeep(arr);
* console.log(clonedArr); // [1, 2, 3]
* console.log(clonedArr === arr); // false
*
* @example
* // Clone an array with nested objects
* const arr = [1, { a: 1 }, [1, 2, 3]];
* const clonedArr = cloneDeep(arr);
* arr[1].a = 2;
* console.log(arr); // [1, { a: 2 }, [1, 2, 3]]
* console.log(clonedArr); // [1, { a: 1 }, [1, 2, 3]]
* console.log(clonedArr === arr); // false
*
* @example
* // Clone an object
* const obj = { a: 1, b: 'es-toolkit', c: [1, 2, 3] };
* const clonedObj = cloneDeep(obj);
* console.log(clonedObj); // { a: 1, b: 'es-toolkit', c: [1, 2, 3] }
* console.log(clonedObj === obj); // false
*
* @example
* // Clone an object with nested objects
* const obj = { a: 1, b: { c: 1 } };
* const clonedObj = cloneDeep(obj);
* obj.b.c = 2;
* console.log(obj); // { a: 1, b: { c: 2 } }
* console.log(clonedObj); // { a: 1, b: { c: 1 } }
* console.log(clonedObj === obj); // false
*/
function cloneDeep(obj) {
	return require_cloneDeepWith.cloneDeepWithImpl(obj, void 0, obj, /* @__PURE__ */ new Map(), void 0);
}
//#endregion
exports.cloneDeep = cloneDeep;
