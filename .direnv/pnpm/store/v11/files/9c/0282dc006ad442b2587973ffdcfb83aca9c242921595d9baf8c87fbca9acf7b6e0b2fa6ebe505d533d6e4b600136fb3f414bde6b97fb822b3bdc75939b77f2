const require_escape = require("../../string/escape.js");
const require_toString = require("../util/toString.js");
//#region src/compat/string/escape.ts
/**
* Converts the characters "&", "<", ">", '"', and "'" in `str` to their corresponding HTML entities.
* For example, "<" becomes "&lt;".
*
* @param str  The string to escape.
* @returns Returns the escaped string.
*
* @example
* escape('This is a <div> element.'); // returns 'This is a &lt;div&gt; element.'
* escape('This is a "quote"'); // returns 'This is a &quot;quote&quot;'
* escape("This is a 'quote'"); // returns 'This is a &#39;quote&#39;'
* escape('This is a & symbol'); // returns 'This is a &amp; symbol'
*/
function escape(string) {
	return require_escape.escape(require_toString.toString(string));
}
//#endregion
exports.escape = escape;
