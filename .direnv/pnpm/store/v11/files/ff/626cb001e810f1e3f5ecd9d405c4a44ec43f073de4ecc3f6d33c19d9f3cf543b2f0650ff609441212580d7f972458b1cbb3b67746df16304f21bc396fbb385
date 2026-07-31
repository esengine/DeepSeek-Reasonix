//#region src/compat/predicate/isNative.ts
const functionToString = Function.prototype.toString;
/** Used to detect if a method is native. */
const IS_NATIVE_FUNCTION_REGEXP = RegExp(`^${functionToString.call(Object.prototype.hasOwnProperty).replace(/[\\^$.*+?()[\]{}|]/g, "\\$&").replace(/hasOwnProperty|(function).*?(?=\\\()| for .+?(?=\\\])/g, "$1.*?")}$`);
/**
* Checks if a given value is a native function.
*
* This function tests whether the provided value is a native function implemented by the JavaScript engine.
* It returns `true` if the value is a native function, and `false` otherwise.
*
* @param value - The value to test for native function.
* @returns `true` if the value is a native function, `false` otherwise.
*
* @example
* const value1 = Array.prototype.push;
* const value2 = () => {};
* const result1 = isNative(value1); // true
* const result2 = isNative(value2); // false
*/
function isNative(value) {
	if (typeof value !== "function") return false;
	if (globalThis?.["__core-js_shared__"] != null) throw new Error("Unsupported core-js use. Try https://npms.io/search?q=ponyfill.");
	return IS_NATIVE_FUNCTION_REGEXP.test(functionToString.call(value));
}
//#endregion
exports.isNative = isNative;
