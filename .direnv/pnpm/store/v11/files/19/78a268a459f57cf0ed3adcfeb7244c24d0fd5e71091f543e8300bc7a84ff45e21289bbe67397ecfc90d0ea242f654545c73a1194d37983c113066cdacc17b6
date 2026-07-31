const require_unescape = require("../../string/unescape.js");
const require_toString = require("../util/toString.js");
//#region src/compat/string/unescape.ts
/**
* Converts the HTML entities `&amp;`, `&lt;`, `&gt;`, `&quot;`, and `&#39;` in `str` to their corresponding characters.
* It is the inverse of `escape`.
*
* @param str The string to unescape.
* @returns Returns the unescaped string.
*
* @example
* unescape('This is a &lt;div&gt; element.'); // returns 'This is a <div> element.'
* unescape('This is a &quot;quote&quot;'); // returns 'This is a "quote"'
* unescape('This is a &#39;quote&#39;'); // returns 'This is a 'quote''
* unescape('This is a &amp; symbol'); // returns 'This is a & symbol'
*/
function unescape(str) {
	return require_unescape.unescape(require_toString.toString(str));
}
//#endregion
exports.unescape = unescape;
