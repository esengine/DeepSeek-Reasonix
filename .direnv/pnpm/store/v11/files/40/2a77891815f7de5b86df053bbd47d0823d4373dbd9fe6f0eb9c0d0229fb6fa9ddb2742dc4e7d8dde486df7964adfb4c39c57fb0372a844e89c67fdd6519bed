const require_updateWith = require("./updateWith.js");
//#region src/compat/object/update.ts
/**
* Updates the value at the specified path of the given object using an updater function.
* If any part of the path does not exist, it will be created.
*
* @param obj - The object to modify.
* @param path - The path of the property to update.
* @param updater - The function to produce the updated value.
* @returns The modified object.
*/
function update(obj, path, updater) {
	return require_updateWith.updateWith(obj, path, updater, () => void 0);
}
//#endregion
exports.update = update;
