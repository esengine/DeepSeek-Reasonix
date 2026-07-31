# CSS Tokenizer <img src="https://cssdb.org/images/css.svg" alt="for CSS" width="90" height="90" align="right">

[<img alt="npm version" src="https://img.shields.io/npm/v/@csstools/css-tokenizer.svg" height="20">][npm-url]
[<img alt="Build Status" src="https://github.com/csstools/postcss-plugins/actions/workflows/test.yml/badge.svg?branch=main" height="20">][cli-url]
[<img alt="Discord" src="https://shields.io/badge/Discord-5865F2?logo=discord&logoColor=white">][discord]

Implemented from : https://drafts.csswg.org/css-syntax/

## API

[Read the API docs](./docs/css-tokenizer.md)

## Usage

Add [CSS Tokenizer] to your project:

```bash
npm install @csstools/css-tokenizer --save-dev
```

```js
import { tokenize } from '@csstools/css-tokenizer';

const myCSS =  `@media only screen and (min-width: 768rem) {
	.foo {
		content: 'Some content!' !important;
	}
}
`;

const tokens = tokenize({
	css: myCSS,
});

console.log(tokens);
```

Or use the streaming interface:

```js
import { tokenizer, TokenType } from '@csstools/css-tokenizer';

const myCSS = `@media only screen and (min-width: 768rem) {
	.foo {
		content: 'Some content!' !important;
	}
}
`;

const t = tokenizer({
	css: myCSS,
});

while (true) {
	const token = t.nextToken();
	if (token[0] === TokenType.EOF) {
		break;
	}

	console.log(token);
}
```

### Options

```ts
{
	onParseError?: (error: ParseError) => void
}
```

#### `onParseError`

The tokenizer is forgiving and won't stop when a parse error is encountered.

To receive parsing error information you can set a callback.

```js
import { tokenizer, TokenType } from '@csstools/css-tokenizer';

const t = tokenizer({
	css: '\\',
}, { onParseError: (err) => console.warn(err) });

while (true) {
	const token = t.nextToken();
	if (token[0] === TokenType.EOF) {
		break;
	}
}
```

Parser errors will try to inform you where in the tokenizer logic the error happened.
This tells you what kind of error occurred.

## Order of priorities

1. specification compliance
2. correctness
3. reliability
4. tokenizing and serializing must round trip losslessly
5. exposing useful aspects about the source code
6. runtime performance
7. package size


[cli-url]: https://github.com/csstools/postcss-plugins/actions/workflows/test.yml?query=workflow/test
[discord]: https://discord.gg/bUadyRwkJS
[npm-url]: https://www.npmjs.com/package/@csstools/css-tokenizer

[CSS Tokenizer]: https://github.com/csstools/postcss-plugins/tree/main/packages/css-tokenizer
