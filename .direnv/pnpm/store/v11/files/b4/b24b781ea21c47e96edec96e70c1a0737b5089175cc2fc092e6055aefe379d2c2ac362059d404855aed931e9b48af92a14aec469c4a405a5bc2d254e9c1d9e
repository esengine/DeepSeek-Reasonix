//#region src/predicate/isBrowser.ts
/**
* Checks if the current environment is a browser.
*
* This function checks for the existence of the `window.document` property,
* which only exists in browser environments.
*
* @returns `true` if the current environment is a browser, otherwise `false`.
*
* @example
* if (isBrowser()) {
*   console.log("This is running in a browser");
*   document.getElementById('app').innerHTML = 'Hello World';
* }
*/
function isBrowser() {
	return typeof window !== "undefined" && window?.document != null;
}
//#endregion
exports.isBrowser = isBrowser;
