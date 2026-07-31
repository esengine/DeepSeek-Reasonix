//#region src/object/findKey.ts
/**
* Finds the key of the first element in the object that satisfies the provided testing function.
*
* @param obj - The object to search.
* @param predicate - The function to execute on each value in the object. It takes three arguments:
*   - value: The current value being processed in the object.
*   - key: The key of the current value being processed in the object.
*   - obj: The object that findKey was called upon.
* @returns The key of the first element in the object that passes the test, or undefined if no element passes.
*
* @example
* const users = {
*   'barney':  { 'age': 36, 'active': true },
*   'fred':    { 'age': 40, 'active': false },
*   'pebbles': { 'age': 1,  'active': true }
* };
* findKey(users, function(o) { return o.age < 40; }); => 'barney'
*/
function findKey(obj, predicate) {
	return Object.keys(obj).find((key) => predicate(obj[key], key, obj));
}
//#endregion
export { findKey };
