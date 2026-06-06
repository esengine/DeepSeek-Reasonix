import { stringWidth } from './stringWidth.js';
import { createTokenizer } from './termio/tokenize.js';

const DEFAULT_TAB_INTERVAL = 8;

export function expandTabs(text: string, interval = DEFAULT_TAB_INTERVAL): string {
  if (!text.includes('\t')) return text;

  const tokenizer = createTokenizer();
  const tokens = tokenizer.feed(text);
  tokens.push(...tokenizer.flush());

  let out = '';
  let column = 0;

  for (const token of tokens) {
    if (token.type === 'sequence') {
      out += token.value;
      continue;
    }

    const segments = token.value.split(/(\t|\n)/);
    for (const segment of segments) {
      if (segment === '\t') {
        const padding = interval - (column % interval);
        out += ' '.repeat(padding);
        column += padding;
      } else if (segment === '\n') {
        out += segment;
        column = 0;
      } else {
        out += segment;
        column += stringWidth(segment);
      }
    }
  }

  return out;
}
