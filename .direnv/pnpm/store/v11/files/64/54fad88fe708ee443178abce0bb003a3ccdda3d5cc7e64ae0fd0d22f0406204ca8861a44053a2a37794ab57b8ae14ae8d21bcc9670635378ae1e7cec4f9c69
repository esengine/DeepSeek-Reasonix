//#region src/server/colors/_internal/wrapAnsiBg.ts
/**
* Wraps text with ANSI open/close codes for background colors.
*
* On top of the basic wrap, handles two cases:
*
* 1. **Nested style sharing the same close.** If `text` already contains
*    `close` (e.g. `bgRed(bgGreen('x'))` where both end with `\x1b[49m`),
*    re-opens the outer style after each occurrence so the rest keeps its
*    color. This step runs first.
* 2. **Multi-line text.** Terminals reset the background color at line
*    boundaries, so the bg disappears after `\n` if not re-opened. Insert
*    `close + open` around every newline. CRLF is treated as a single unit
*    so the close sits outside both characters.
*
* Step 2 must run after step 1 — otherwise the close codes that step 2
* inserts would also trigger step 1's re-open and leak duplicate codes.
*
* @param open - The ANSI background open code (e.g. `\x1b[41m`).
* @param close - The ANSI background close code (e.g. `\x1b[49m`).
* @param text - The text to wrap.
* @returns The wrapped text.
*/
function wrapAnsiBg(open, close, text) {
	if (text.includes(close)) text = text.replaceAll(close, close + open);
	if (text.includes("\n")) text = text.replaceAll(/\r?\n/g, (match) => close + match + open);
	return open + text + close;
}
//#endregion
exports.wrapAnsiBg = wrapAnsiBg;
