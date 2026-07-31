//#region src/server/colors/_internal/wrapAnsi.ts
/**
* Wraps text with ANSI open/close codes for foreground colors and modifiers.
*
* If `text` already contains `close` (typically from a nested style that
* shares the same close code, e.g. `red(green('x'))` where both end with
* `\x1b[39m`), re-opens the outer style after each occurrence so the rest
* of the text keeps its color. For background colors use `wrapAnsiBg`
* instead — it adds the newline handling that backgrounds need.
*
* @param open - The ANSI open code (e.g. `\x1b[31m`).
* @param close - The ANSI close code (e.g. `\x1b[39m`).
* @param text - The text to wrap.
* @returns The wrapped text.
*/
function wrapAnsi(open, close, text) {
	if (text.includes(close)) text = text.replaceAll(close, close + open);
	return open + text + close;
}
//#endregion
exports.wrapAnsi = wrapAnsi;
