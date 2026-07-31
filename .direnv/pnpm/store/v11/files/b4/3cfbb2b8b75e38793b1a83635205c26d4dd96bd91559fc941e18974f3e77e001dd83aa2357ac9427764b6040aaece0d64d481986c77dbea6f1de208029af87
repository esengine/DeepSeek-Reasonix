//#region src/predicate/isPlainObject.ts
/**
* Checks if a given value is a plain object.
*
* @param value - The value to check.
* @returns True if the value is a plain object, otherwise false.
*
* @example
* ```typescript
* // ✅👇 True
*
* isPlainObject({ });                       // ✅
* isPlainObject({ key: 'value' });          // ✅
* isPlainObject({ key: new Date() });       // ✅
* isPlainObject(new Object());              // ✅
* isPlainObject(Object.create(null));       // ✅
* isPlainObject({ nested: { key: true} });  // ✅
* isPlainObject(new Proxy({}, {}));         // ✅
* isPlainObject({ [Symbol('tag')]: 'A' });  // ✅
*
* // ✅👇 (cross-realms, node context, workers, ...)
* const runInNewContext = await import('node:vm').then(
*     (mod) => mod.runInNewContext
* );
* isPlainObject(runInNewContext('({})'));   // ✅
*
* // ❌👇 False
*
* class Test { };
* isPlainObject(new Test())           // ❌
* isPlainObject(10);                  // ❌
* isPlainObject(null);                // ❌
* isPlainObject('hello');             // ❌
* isPlainObject([]);                  // ❌
* isPlainObject(new Date());          // ❌
* isPlainObject(new Uint8Array([1])); // ❌
* isPlainObject(Buffer.from('ABC'));  // ❌
* isPlainObject(Promise.resolve({})); // ❌
* isPlainObject(Object.create({}));   // ❌
* isPlainObject(new (class Cls {}));  // ❌
* isPlainObject(globalThis);          // ❌,
* ```
*/
function isPlainObject(value) {
	if (!value || typeof value !== "object") return false;
	const proto = Object.getPrototypeOf(value);
	if (!(proto === null || proto === Object.prototype || Object.getPrototypeOf(proto) === null)) return false;
	return Object.prototype.toString.call(value) === "[object Object]";
}
//#endregion
export { isPlainObject };
