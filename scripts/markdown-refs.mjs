// Finds the #N references in Markdown prose. A number counts only where a
// reference is written: at the start of a line, or after whitespace, "(", "（"
// or a list separator ("、", "，", ","). Fenced and indented code, code spans,
// HTML comments, link targets ("](#N") and escaped "\#N" are never references.

const refPattern = /(?<=^|[\s（、，,]|(?<!\])\()#(\d+)(?![\w-])/g;
const codeSpanPattern = /(`+)[\s\S]*?\1/g;
const listItemPattern = /^ {0,3}(?:[-*+]|\d+[.)])(?:\s|$)/;
const masked = "\u0001";

function mask(text, start, end) {
  return text.slice(0, start) + masked.repeat(end - start) + text.slice(end);
}

// Returns, per line, the text with every non-prose span replaced by a
// same-length run of a non-space placeholder, so match offsets still index the
// original line and a placeholder never reads as the whitespace a ref needs.
function proseLines(markdown) {
  const lines = markdown.split("\n");
  let fence = null;
  let comment = false;
  let previousBlank = true;
  let previousCode = false;
  let inList = false;
  return lines.map((line) => {
    const blank = line.trim() === "";
    const opening = /^ {0,3}(`{3,}|~{3,})/.exec(line);
    if (fence) {
      const closing = /^ {0,3}(`{3,}|~{3,})\s*$/.exec(line);
      if (closing && closing[1][0] === fence[0] && closing[1].length >= fence.length) fence = null;
      previousBlank = false;
      return masked.repeat(line.length);
    }
    if (opening && !comment) {
      fence = opening[1];
      previousBlank = false;
      return masked.repeat(line.length);
    }
    const indented = /^( {4}|\t)/.test(line);
    if (!comment && indented && !inList && (previousBlank || previousCode)) {
      previousCode = true;
      previousBlank = false;
      return masked.repeat(line.length);
    }
    previousCode = false;
    if (listItemPattern.test(line)) inList = true;
    else if (!blank && !indented) inList = false;
    previousBlank = blank;

    let text = line;
    for (const match of text.matchAll(codeSpanPattern)) {
      if (!comment) text = mask(text, match.index, match.index + match[0].length);
    }
    let from = 0;
    while (from <= text.length) {
      if (comment) {
        const end = text.indexOf("-->", from);
        if (end === -1) return mask(text, from, text.length);
        text = mask(text, from, end + 3);
        from = end + 3;
        comment = false;
      }
      const start = text.indexOf("<!--", from);
      if (start === -1) break;
      comment = true;
      from = start;
    }
    return text;
  });
}

export function markdownRefs(markdown) {
  return proseLines(markdown).flatMap((prose) => [...prose.matchAll(refPattern)].map((match) => Number(match[1])));
}

// Appends suffixFor(N) after each reference; everything else is left byte for byte.
export function annotateMarkdown(markdown, suffixFor) {
  const source = markdown.split("\n");
  return proseLines(markdown)
    .map((prose, index) => {
      let line = source[index];
      for (const match of [...prose.matchAll(refPattern)].reverse()) {
        const end = match.index + match[0].length;
        line = line.slice(0, end) + suffixFor(Number(match[1])) + line.slice(end);
      }
      return line;
    })
    .join("\n");
}
