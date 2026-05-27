/** Walk `text` line by line without allocating an intermediate array. */
export function forEachLine(
  text: string,
  visit: (line: string) => void | boolean,
): void {
  let start = 0;
  while (start <= text.length) {
    const end = text.indexOf('\n', start);
    const line = end === -1 ? text.substring(start) : text.substring(start, end);
    if (visit(line) === true) return;
    if (end === -1) break;
    start = end + 1;
  }
}
