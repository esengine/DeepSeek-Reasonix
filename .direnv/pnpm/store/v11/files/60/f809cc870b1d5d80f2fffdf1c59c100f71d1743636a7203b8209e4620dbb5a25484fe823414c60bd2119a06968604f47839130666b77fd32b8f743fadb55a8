[![Calculate CSS Specificity](./screenshots/calculate-specificity.png)](https://codepen.io/bramus/pen/WNXyoYm)

# Specificity

`@bramus/specificity` is a package to calculate the specificity of CSS Selectors. It also includes some convenience functions to compare, sort, and filter an array of specificity values.

Supports [Selectors Level 4](https://www.w3.org/TR/selectors-4/), including those special cases `:is()`, `:where()`, `:not()`, etc.

Demo: [https://codepen.io/bramus/pen/WNXyoYm](https://codepen.io/bramus/pen/WNXyoYm)

## Installation

```bash
npm i @bramus/specificity
```

## Usage / Example

At its core, `@bramus/specificity` exposes a `Specificity` class. Its static `calculate` method can be used to calculate the specificity of a given CSS [Selector List](https://www.w3.org/TR/selectors-4/#grouping) string.

```js
import Specificity from '@bramus/specificity';

const specificities = Specificity.calculate('header:where(#top) nav li:nth-child(2n), #doormat');
```

Because `calculate` accepts a [Selector List](https://www.w3.org/TR/selectors-4/#grouping) — which can contain more than 1 [Selector](https://www.w3.org/TR/selectors-4/#selector) — it will always return an array, with each entry being a `Specificity` instance — one per found selector.

```js
const specificities = Specificity.calculate('header:where(#top) nav li:nth-child(2n), #doormat');
specificities.map((s) => s.toString()); // ~> ["(0,1,3)","(1,0,0)"]
```

💡 If you know you’re passing only a single Selector into `calculate()`, you can use JavaScript’s built-in destructuring to keep your variable names clean.

```js
const [s] = Specificity.calculate('header:where(#top) nav li:nth-child(2n)');
s.toString(); // ~> "(0,1,3)"
```

💡 Under the hood, `@bramus/specificity` uses [CSSTree](https://github.com/csstree/csstree) to do the parsing of strings to Selectors. As a result, the `calculate` method also accepts a [CSSTree AST](https://github.com/csstree/csstree/blob/master/docs/ast.md) of the types `Selector` and `SelectorList`.

If you have a pre-parsed CSSTree AST of the type `Selector` you can pass it into `Specificity.calculateForAST()`. It [performs slightly better](#benchmark) than `Specificity.calculate()` as it needs to check fewer things. It differs from `Specificity.calculate()` in that it does not return an array of `Specificity` instances but only a single value.

## The Return Format

A calculated specificity is represented as an instance of the `Specificity` class. The `Specificity` class includes methods to get the specificity value in a certain format, along with some convenience methods to compare it against other instances.

```js
// 🚀 Thunderbirds are go!
import Specificity from '@bramus/specificity';

// ✨ Calculate specificity for each Selector in the given Selector List
const specificities = Specificity.calculate('header:where(#top) nav li:nth-child(2n), #doormat');

// 🚚 The values in the array are instances of the Specificity class
const s = specificities[0]; // Instance of Specificity

// 👀 Read the specificity value using one of its accessors
s.value; // { a: 0, b: 1, c: 3 }
s.a; // 0
s.b; // 1
s.c; // 3

// 🛠 Convert the calculated value to various formats using one of the toXXX() instance methods
s.toString(); // "(0,1,3)"
s.toArray(); // [0, 1, 3]
s.toObject(); // { a: 0, b: 1, c: 3 }

// 💡 Extract the matched selector string
s.selectorString(); // "header:where(#top) nav li:nth-child(2n)"

// 🔀 Use one of its instance comparison methods to compare it to another Specificity instance
s.isEqualTo(specificities[1]); // false
s.isGreaterThan(specificities[1]); // false
s.isLessThan(specificities[1]); // true

// 💻 Don’t worry about JSON.stringify()
JSON.stringify(s);
// {
//    "selector": 'header:where(#top) nav li:nth-child(2n)',
//    "asObject": { "a": 0, "b": 1, "c": 3 },
//    "asArray": [0, 1, 3],
//    "asString": "(0,1,3)",
// }
```

## Utility Functions (Static Methods)

This package also exposes some utility functions to work with specificities. These utility functions are all exposed as static methods on the `Specificity` class.

-   Comparing:

    -   `Specificity.compare(s1, s2)`: Compares s1 to s2. Returns a value that can be:
        -   `> 0` = Sort s2 before s1 _(i.e. s1 is more specific than s2)_
        -   `0` = Keep original order of s1 and s2 _(i.e. s1 and s2 are equally specific)_
        -   `< 0` = Sort s1 before s2 _(i.e. s1 is less specific than s2)_
    -   `Specificity.equals(s1, s2)`: Returns `true` if s1 and s2 have the same specificity. If not, `false` is returned.
    -   `Specificity.greaterThan(s1, s2)`: Returns `true` if s1 has a higher specificity than s2. If not, `false` is returned.
    -   `Specificity.lessThan(s1, s2)`: Returns `true` if s1 has a lower specificity than s2. If not, `false` is returned.

-   Sorting:

    -   `Specificity.sortAsc(s1, s2, …, sN)`: Sorts the given specificities in ascending order _(low specificity to high specificity)_
    -   `Specificity.sortDesc(s1, s2, …, sN)`: Sorts the given specificities in descending order _(high specificity to low specificity)_

-   Filtering:
    -   `Specificity.min(s1, s2, …, sN)`: Filters out the value with the lowest specificity
    -   `Specificity.max(s1, s2, …, sN)`: Filters out the value with the highest specificity

A specificity passed into any of these utility functions can be any of:

-   An instance of the included `Specificity` class
-   A simple Object such as `{'a': 1, 'b': 0, 'c': 2}`

## Utility Functions (Standalone)

All static methods the `Specificity` class exposes are also exported as standalone functions using [Subpath Exports](https://nodejs.org/api/packages.html#subpath-exports).

If you're only interested in including some of these functions into your project you can import them from their Subpath. As a result, your bundle size will be reduced greatly _(except for including the standalone `calculate`, as it returns an array of `Specificity` instances that relies on the whole lot)_

```js
import { calculate, calculateForAST } from '@bramus/specificity/core';
import { compare, equals, greaterThan, lessThan } from '@bramus/specificity/compare';
import { min, max } from '@bramus/specificity/filter';
import { sortAsc, sortDesc } from '@bramus/specificity/sort';
```

## Type Definitions

Although `@bramus/specificity` is written in Vanilla JavaScript, it does include [Type Definitions](https://www.typescriptlang.org/docs/handbook/2/type-declarations.html) which are exposed via its `package.json`.

## Binary/CLI

`@bramus/specificity` exposes a binary named `specificity` to calculate the specificity of a given selector list on the CLI. For each selector that it finds, it'll print out the calculated specificity as a string on a new line.

```bash
$ specificity "header:where(#top) nav li:nth-child(2n), #doormat"
(0,1,3)
(1,0,0)
```

## Benchmark

A benchmark is included, which you can invoke using `npm run benchmark`.

Sample results (tested on a MacBook Air M3):

```
Specificity.calculate(string) x 420,682 ops/sec ±0.34% (98 runs sampled)
Specificity.calculate(ast) - using SelectorList x 8,994,080 ops/sec ±0.25% (98 runs sampled)
Specificity.calculate(ast) - using Selector x 11,054,856 ops/sec ±0.39% (91 runs sampled)
Specificity.calculateForAST(ast) x 12,652,322 ops/sec ±0.35% (96 runs sampled)
```

## License

`@bramus/specificity` is released under the MIT public license. See the enclosed `LICENSE` for details.

## Acknowledgements

The idea to create this package was sparked by [the wonderful Specificity Calculator created by Kilian Valkhof / Polypane](https://polypane.app/css-specificity-calculator/), a highly educational tool that not only calculates the specificity, but also explains which parts are responsible for it.

The heavy lifting of doing the actual parsing of Selectors is done by [CSSTree](https://github.com/csstree/csstree).
